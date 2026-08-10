package deepseek

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// keccakF1600_23Ref is an independent, textbook Keccak-f[1600] written with
// explicit (x, y) coordinates (FIPS 202 step order), restricted to rounds
// 1..23 like DeepSeek's variant. It deliberately shares no tables with the
// solver so table/indexing bugs in keccakF1600_23 cannot cancel out.
func keccakF1600_23Ref(a *[25]uint64) {
	var c, d [5]uint64
	var b [25]uint64

	for round := 1; round <= 23; round++ {
		for x := 0; x < 5; x++ {
			c[x] = a[x] ^ a[x+5] ^ a[x+10] ^ a[x+15] ^ a[x+20]
		}
		for x := 0; x < 5; x++ {
			d[x] = c[(x+4)%5] ^ rotl(c[(x+1)%5], 1)
		}
		for x := 0; x < 5; x++ {
			for y := 0; y < 5; y++ {
				a[x+5*y] ^= d[x]
			}
		}
		for x := 0; x < 5; x++ {
			for y := 0; y < 5; y++ {
				src := x + 5*y
				dst := y + 5*((2*x+3*y)%5)
				b[dst] = rotl(a[src], rhoOffsets[src])
			}
		}
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				a[x+5*y] = b[x+5*y] ^ (^b[(x+1)%5+5*y] & b[(x+2)%5+5*y])
			}
		}
		a[0] ^= keccakRoundConstants[round]
	}
}

// TestKeccakF1600_23MatchesReference cross-checks the table-based permutation
// in pow.go against the textbook reference on a realistic absorbed state.
func TestKeccakF1600_23MatchesReference(t *testing.T) {
	var st [25]uint64
	idx := 0
	prefix := "f27be616d0d3737613a2_1786304784836_"
	for i := 0; i < len(prefix); i++ {
		st[idx/8] ^= uint64(prefix[i]) << uint((idx%8)*8)
		idx++
	}
	ns := "12345"
	for i := 0; i < len(ns); i++ {
		st[idx/8] ^= uint64(ns[i]) << uint((idx%8)*8)
		idx++
	}
	st[idx/8] ^= uint64(0x06) << uint((idx%8)*8)
	st[135/8] ^= uint64(0x80) << uint((135%8)*8)

	got, want := st, st
	keccakF1600_23(&got)
	keccakF1600_23Ref(&want)
	if got != want {
		t.Fatalf("keccakF1600_23 menyimpang dari referensi:\ngot  = %x\nwant = %x", got, want)
	}
}

// hashV1 computes DeepSeekHashV1(prefix + nonce) for test verification.
// Uses the independent reference permutation so the roundtrip test below
// is not self-consistent with the solver.
func hashV1(prefix string, nonce int64) [32]byte {
	var st [25]uint64
	idx := 0
	for i := 0; i < len(prefix); i++ {
		st[idx/8] ^= uint64(prefix[i]) << uint((idx%8)*8)
		idx++
	}
	ns := strconv.AppendInt(nil, nonce, 10)
	for j := 0; j < len(ns); j++ {
		st[idx/8] ^= uint64(ns[j]) << uint((idx%8)*8)
		idx++
	}
	st[idx/8] ^= uint64(0x06) << uint((idx%8)*8)
	st[135/8] ^= uint64(0x80) << uint((135%8)*8)
	keccakF1600_23Ref(&st)
	var out [32]byte
	for i := 0; i < 32; i++ {
		out[i] = byte(st[i/8] >> uint((i%8)*8))
	}
	return out
}

// TestSolveChallengeRoundtrip verifies the solver against a synthetic
// challenge: we pick a known nonce, compute the expected hash with the same
// 23-round Keccak, and make sure SolveChallenge finds it.
func TestSolveChallengeRoundtrip(t *testing.T) {
	salt := "f27be616d0d3737613a2"
	expireAt := int64(1786304784836)
	nonce := int64(12345)

	h := hashV1(fmt.Sprintf("%s_%d_", salt, expireAt), nonce)
	expected := hex.EncodeToString(h[:])

	c := Challenge{
		Algorithm:  "DeepSeekHashV1",
		Challenge:  expected,
		Salt:       salt,
		Difficulty: 144000,
		ExpireAt:   expireAt,
		Signature:  "sig-test",
	}

	header, err := SolveChallenge(c, "/api/v0/chat/completion")
	if err != nil {
		t.Fatalf("SolveChallenge: %v", err)
	}

	raw, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var ans powAnswer
	if err := json.Unmarshal(raw, &ans); err != nil {
		t.Fatalf("unmarshal answer: %v", err)
	}
	if ans.Answer != nonce {
		t.Fatalf("answer = %d, want %d", ans.Answer, nonce)
	}
	if ans.Algorithm != "DeepSeekHashV1" || ans.Signature != "sig-test" {
		t.Fatalf("answer fields tidak lengkap: %+v", ans)
	}
	if ans.TargetPath != "/api/v0/chat/completion" {
		t.Fatalf("target_path = %q", ans.TargetPath)
	}
}

// TestSolveChallengeWrongTarget ensures the solver does not find a nonce
// when the target hash can never match.
func TestSolveChallengeWrongTarget(t *testing.T) {
	c := Challenge{
		Algorithm:  "DeepSeekHashV1",
		Challenge:  strings.Repeat("00", 32),
		Salt:       "x",
		Difficulty: 1000,
		ExpireAt:   1,
		Signature:  "s",
	}
	if _, err := SolveChallenge(c, "/api/v0/chat/completion"); err == nil {
		t.Fatal("expected error untuk target yang mustahil")
	}
}
