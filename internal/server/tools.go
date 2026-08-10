package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// execute menjalankan satu panggilan tool dan mengembalikan hasilnya sebagai
// teks yang dikirim balik ke model.
func (a *agent) execute(call toolCall) string {
	start := time.Now()
	result := a.execTool(call)
	return fmt.Sprintf("tool: %s (durasi %s)\n%s", call.Name, time.Since(start).Round(time.Millisecond), clip(result, maxToolResultLen))
}

func (a *agent) execTool(call toolCall) string {
	var out string
	var err error
	switch call.Name {
	case "bash":
		out, err = a.toolBash(call.Args)
	case "read":
		out, err = toolRead(call.Args, a.cwd)
	case "write":
		out, err = toolWrite(call.Args, a.cwd)
	case "edit":
		out, err = toolEdit(call.Args, a.cwd)
	case "glob":
		out, err = toolGlob(call.Args, a.cwd)
	case "grep":
		out, err = toolGrep(call.Args, a.cwd)
	case "webfetch":
		out, err = toolWebFetch(call.Args)
	case "todowrite":
		return "todo dicatat (no-op)"
	case "task":
		return "tool 'task' (subagent) tidak didukung backend ini; selesaikan sendiri dengan tool lain (bash/read/edit/glob/grep)."
	default:
		return fmt.Sprintf("tool '%s' tidak didukung", call.Name)
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}

// argString mengambil argumen string dengan fallback default.
func argString(args map[string]any, key string, def string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return def
}

func argBool(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

func argInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case json.Number:
		n, _ := strconv.Atoi(string(v))
		return n
	case string:
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}

// resolvePath menggabungkan path relatif dengan working directory agent.
func (a *agent) resolvePath(p string) string {
	if p == "" {
		return a.cwd
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(a.cwd, p)
}

func (a *agent) toolBash(args map[string]any) (string, error) {
	command := argString(args, "command", "")
	if command == "" {
		return "", fmt.Errorf("argumen 'command' kosong")
	}
	timeout := argInt(args, "timeout", 120)
	if timeout > 600 {
		timeout = 600
	}
	workdir := a.resolvePath(argString(args, "workdir", a.cwd))

	ctx, cancel := context.WithTimeout(a.ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	var b strings.Builder
	if stdout.Len() > 0 {
		b.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteString("\n[stderr]\n")
		}
		b.WriteString(stderr.String())
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return b.String(), fmt.Errorf("command timeout setelah %ds (output parsial di atas)", timeout)
		}
		return b.String(), fmt.Errorf("exit: %w", err)
	}
	if b.Len() == 0 {
		return "(no output)", nil
	}
	return b.String(), nil
}

func toolRead(args map[string]any, cwd string) (string, error) {
	path := args["filePath"]
	if path == nil {
		return "", fmt.Errorf("argumen 'filePath' tidak ada")
	}
	p := filepath.Join(cwd, path.(string))
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	content := string(b)
	offset := argInt(args, "offset", 0)
	limit := argInt(args, "limit", 0)
	if offset > 0 || limit > 0 {
		lines := strings.Split(content, "\n")
		if offset > len(lines) {
			offset = len(lines)
		}
		end := len(lines)
		if limit > 0 && offset+limit < end {
			end = offset + limit
		}
		content = strings.Join(lines[offset:end], "\n")
	}
	return fmt.Sprintf("(%d bytes)", len(content)) + "\n" + clip(content, maxToolResultLen), nil
}

func toolWrite(args map[string]any, cwd string) (string, error) {
	path, _ := args["filePath"].(string)
	if path == "" {
		return "", fmt.Errorf("argumen 'filePath' tidak ada")
	}
	content, _ := args["content"].(string)
	p := filepath.Join(cwd, path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("file ditulis: %s (%d bytes)", p, len(content)), nil
}

func toolEdit(args map[string]any, cwd string) (string, error) {
	path, _ := args["filePath"].(string)
	if path == "" {
		return "", fmt.Errorf("argumen 'filePath' tidak ada")
	}
	oldString := argString(args, "oldString", "")
	newString := argString(args, "newString", "")
	replaceAll := argBool(args, "replaceAll", false)

	p := filepath.Join(cwd, path)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	content := string(b)

	if oldString == "" {
		// Kosongkan file (mirip replace dengan newString).
		if err := os.WriteFile(p, []byte(newString), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("file diperbarui: %s", p), nil
	}

	if replaceAll {
		if !strings.Contains(content, oldString) {
			return "", fmt.Errorf("oldString tidak ditemukan di %s", p)
		}
		content = strings.ReplaceAll(content, oldString, newString)
	} else {
		if strings.Count(content, oldString) == 0 {
			return "", fmt.Errorf("oldString tidak ditemukan di %s (baca dulu file untuk konteks yang tepat)", p)
		}
		if strings.Count(content, oldString) > 1 {
			return "", fmt.Errorf("oldString ditemukan %d kali; pakai replaceAll=true atau konteks yang lebih unik", strings.Count(content, oldString))
		}
		content = strings.Replace(content, oldString, newString, 1)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("file diedit: %s", p), nil
}

func toolGlob(args map[string]any, cwd string) (string, error) {
	pattern := argString(args, "pattern", "")
	if pattern == "" {
		return "", fmt.Errorf("argumen 'pattern' kosong")
	}
	root := cwd
	if p, ok := args["path"].(string); ok && p != "" {
		root = filepath.Join(cwd, p)
	}

	var matches []string
	if strings.Contains(pattern, "**") {
		segs := strings.Split(filepath.ToSlash(pattern), "/")
		matches = walkMatch(root, segs)
	} else {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(root, pattern)
		}
		m, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		matches = m
	}
	if len(matches) == 0 {
		return "(tidak ada file yang cocok)", nil
	}
	if len(matches) > 200 {
		matches = matches[:200]
	}
	return strings.Join(matches, "\n"), nil
}

// walkMatch mencocokkan pattern berisi "**" secara rekursif.
func walkMatch(root string, segs []string) []string {
	if len(segs) == 0 {
		return nil
	}
	var out []string
	if segs[0] == "**" {
		out = append(out, walkMatch(root, segs[1:])...)
		entries, err := os.ReadDir(root)
		if err != nil {
			return out
		}
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, walkMatch(filepath.Join(root, e.Name()), segs)...)
			}
		}
		return out
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		ok, err := filepath.Match(segs[0], e.Name())
		if err != nil || !ok {
			continue
		}
		p := filepath.Join(root, e.Name())
		if len(segs) == 1 {
			if !e.IsDir() {
				out = append(out, p)
			}
			continue
		}
		if e.IsDir() {
			out = append(out, walkMatch(p, segs[1:])...)
		}
	}
	return out
}

func toolGrep(args map[string]any, cwd string) (string, error) {
	pattern := argString(args, "pattern", "")
	if pattern == "" {
		return "", fmt.Errorf("argumen 'pattern' kosong")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("pattern regex tidak valid: %w", err)
	}
	root := cwd
	if p, ok := args["path"].(string); ok && p != "" {
		root = filepath.Join(cwd, p)
	}
	var inc *regexp.Regexp
	if s := argString(args, "include", ""); s != "" {
		if inc, err = regexp.Compile(s); err != nil {
			return "", fmt.Errorf("include tidak valid: %w", err)
		}
	}

	var out []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if inc != nil && !inc.MatchString(p) {
			return nil
		}
		if info.Size() > 1<<20 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if len(out) >= 50 {
				return filepath.SkipAll
			}
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d: %s", p, i+1, clip(line, 300)))
			}
		}
		return nil
	})
	if len(out) == 0 {
		return "(tidak ada kecocokan)", nil
	}
	return strings.Join(out, "\n"), nil
}

func toolWebFetch(args map[string]any) (string, error) {
	url := argString(args, "url", "")
	if url == "" {
		return "", fmt.Errorf("argumen 'url' kosong")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d dari %s", resp.StatusCode, url)
	}
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(io.LimitReader(resp.Body, maxToolResultLen+1))
	if err != nil {
		return "", err
	}
	return clip(buf.String(), maxToolResultLen), nil
}

// clip memendekkan teks ke n karakter.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n...[dipotong]"
}
