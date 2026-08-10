package deepseek

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
)

// Challenge is the proof-of-work challenge returned by /chat/create_pow_challenge.
// Field names match the JSON the DeepSeek web app sends.
type Challenge struct {
	Algorithm  string `json:"algorithm"`
	Challenge  string `json:"challenge"` // expected hash, hex-encoded 32 bytes
	Salt       string `json:"salt"`
	Difficulty int    `json:"difficulty"`
	ExpireAt   int64  `json:"expire_at"`
	Signature  string `json:"signature"`
}

// powAnswer is the JSON payload that gets base64-encoded into the
// x-ds-pow-response header.
type powAnswer struct {
	Algorithm  string `json:"algorithm"`
	Challenge  string `json:"challenge"`
	Salt       string `json:"salt"`
	Answer     int64  `json:"answer"`
	Signature  string `json:"signature"`
	TargetPath string `json:"target_path"`
}

// SolveChallenge finds a nonce in [0, difficulty] such that
// DeepSeekHashV1(salt_expireAt_nonce) == challenge, then returns the
// x-ds-pow-response header value.
//
// DeepSeekHashV1 is a custom variant: SHA3-256 padding (0x06 + 0x80) over a
// Keccak-f[1600] permutation with only 23 rounds (standard is 24). There is no
// library implementation of this, so the permutation lives in keccak.go.
func SolveChallenge(c Challenge, targetPath string) (string, error) {
	prefix := fmt.Sprintf("%s_%d_", c.Salt, c.ExpireAt)
	target, err := hex.DecodeString(c.Challenge)
	if err != nil {
		return "", fmt.Errorf("challenge hash bukan hex: %w", err)
	}

	answer, err := findNonce(prefix, target, c.Difficulty)
	if err != nil {
		return "", err
	}

	payload := powAnswer{
		Algorithm:  c.Algorithm,
		Challenge:  c.Challenge,
		Salt:       c.Salt,
		Answer:     answer,
		Signature:  c.Signature,
		TargetPath: targetPath,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("gagal marshal jawaban PoW: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// keccakRoundConstants for Keccak-f[1600]. DeepSeek's variant runs rounds
// 1..23 (skipping round 0), so index 0 is never used.
var keccakRoundConstants = [24]uint64{
	0x0000000000000001, 0x0000000000008082, 0x800000000000808A,
	0x8000000080008000, 0x000000000000808B, 0x0000000080000001,
	0x8000000080008081, 0x8000000000008009, 0x000000000000008A,
	0x0000000000000088, 0x0000000080008009, 0x000000008000000A,
	0x000000008000808B, 0x800000000000008B, 0x8000000000008089,
	0x8000000000008003, 0x8000000000008002, 0x8000000000000080,
	0x000000000000800A, 0x800000008000000A, 0x8000000080008081,
	0x8000000000008080, 0x0000000080000001, 0x8000000080008008,
}

// rhoOffsets[s] = rotation offset of source lane s (standard Keccak rho
// table, indexed by x+5y).
//
// In the rho+pi step the rotation amount is that of the SOURCE lane,
// A'[dest] = rot(A[src], rho[src]) — same as standard Keccak (FIPS 202).
// What makes DeepSeek's variant differ from standard Keccak is only the
// round count: 23 instead of 24.
var rhoOffsets = [25]uint{
	0, 1, 62, 28, 27,
	36, 44, 6, 55, 20,
	3, 10, 43, 25, 39,
	41, 45, 15, 21, 8,
	18, 2, 61, 56, 14,
}

// piLane[j] = source lane index for destination lane j (standard Keccak pi).
var piLane = [25]int{
	0, 6, 12, 18, 24,
	3, 9, 10, 16, 22,
	1, 7, 13, 19, 20,
	4, 5, 11, 17, 23,
	2, 8, 14, 15, 21,
}

func rotl(x uint64, n uint) uint64 {
	return (x << n) | (x >> (64 - n))
}

// keccakF1600_23 runs 23 rounds (1..23) of the DeepSeek Keccak-f[1600]
// variant. Same structure as standard Keccak except the rho+pi rotation
// amount is taken from the source lane (see rhoOffsets note above).
func keccakF1600_23(a *[25]uint64) {
	var c [5]uint64
	var d [5]uint64
	var b [25]uint64

	for round := 1; round <= 23; round++ {
		// Theta
		for i := 0; i < 5; i++ {
			c[i] = a[i] ^ a[i+5] ^ a[i+10] ^ a[i+15] ^ a[i+20]
		}
		for i := 0; i < 5; i++ {
			d[i] = c[(i+4)%5] ^ rotl(c[(i+1)%5], 1)
		}
		for i := 0; i < 25; i++ {
			a[i] ^= d[i%5]
		}

		// Rho + Pi (rotation amount of the source lane)
		for j := 0; j < 25; j++ {
			b[j] = rotl(a[piLane[j]], rhoOffsets[piLane[j]])
		}

		// Chi
		for i := 0; i < 25; i += 5 {
			for j := 0; j < 5; j++ {
				a[i+j] = b[i+j] ^ (^b[i+(j+1)%5] & b[i+(j+2)%5])
			}
		}

		// Iota
		a[0] ^= keccakRoundConstants[round]
	}
}

// findNonce iterates nonce 0..difficulty and returns the first one whose
// DeepSeekHashV1 digest equals target. A random starting offset distributes
// load across requests.
//
// Message layout (single 136-byte block, guaranteed since prefix is short):
//
//	[0..base_len)        prefix bytes
//	[base_len..+nlen)    nonce decimal digits
//	[base_len+nlen]      0x06 (SHA3 domain byte)
//	zeros
//	[135]                0x80 (end-of-padding, always at the last byte)
func findNonce(prefix string, target []byte, difficulty int) (int64, error) {
	if difficulty <= 0 {
		difficulty = 144000
	}

	// Absorb the static part once: prefix bytes + the fixed 0x80 tail byte.
	var static [25]uint64
	idx := 0
	for i := 0; i < len(prefix); i++ {
		static[idx/8] ^= uint64(prefix[i]) << uint((idx%8)*8)
		idx++
	}
	static[135/8] ^= uint64(0x80) << uint((135%8)*8)

	start := randomStart(difficulty)
	nbuf := make([]byte, 0, 20)

	for i := 0; i <= difficulty; i++ {
		nonce := int64((start + i) % (difficulty + 1))
		ns := strconv.AppendInt(nbuf[:0], nonce, 10)

		a := static
		pos := idx
		for j := 0; j < len(ns); j++ {
			a[pos/8] ^= uint64(ns[j]) << uint((pos%8)*8)
			pos++
		}
		a[pos/8] ^= uint64(0x06) << uint((pos%8)*8)

		keccakF1600_23(&a)

		// Compare digest (state lanes 0..3, little-endian) with target bytes.
		if matchState(a, target) {
			return nonce, nil
		}
	}

	return 0, fmt.Errorf("PoW tidak terselesaikan dalam batas difficulty %d", difficulty)
}

func matchState(a [25]uint64, target []byte) bool {
	for i := 0; i < 32; i++ {
		got := byte(a[i/8] >> uint((i%8)*8))
		if got != target[i] {
			return false
		}
	}
	return true
}

func randomStart(max int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max+1)))
	if err != nil {
		return 0
	}
	return int(n.Int64())
}
