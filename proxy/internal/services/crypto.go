package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
)

// AES-256-GCM helper, symmetric with the Node.js implementation in
// dashboard/app/api/keys/route.ts.
//
// Output format of encrypt():
//
//	<12 bytes IV><16 bytes auth tag><N bytes ciphertext>
//	all in hex, concatenated. IV is random per call.
//
// The same shared key (hex-encoded 32 bytes) must be set in:
//   - dashboard .env as ENCRYPTION_KEY
//   - proxy      .env as ENCRYPTION_KEY
//
// If unset, both sides fall back to a dev-only key derived from a
// stable per-deployment seed; this is logged loud at first use.

var (
	gcmKeyOnce sync.Once
	gcmKey     []byte
	gcmAEAD    cipher.AEAD
	gcmWarned  bool
)

func loadGCMKey() {
	gcmKeyOnce.Do(func() {
		raw := os.Getenv("ENCRYPTION_KEY")
		if raw == "" {
			// Dev fallback. Loud warning so it's obvious in logs.
			sum := sha256.Sum256([]byte("Synapse Proxy-dev-key-do-not-use-in-prod"))
			gcmKey = sum[:]
			if !gcmWarned {
				fmt.Println("[crypto] WARNING: ENCRYPTION_KEY not set, using dev fallback. Set it in production .env (32 bytes hex).")
				gcmWarned = true
			}
		} else {
			b, err := hex.DecodeString(raw)
			if err != nil {
				panic(fmt.Sprintf("[crypto] ENCRYPTION_KEY is not valid hex: %v", err))
			}
			if len(b) == 32 {
				gcmKey = b
			} else {
				// Accept any length: derive 32 bytes via SHA-256 to match
				// the dashboard's permissive key derivation.
				sum := sha256.Sum256(b)
				gcmKey = sum[:]
			}
		}
		block, err := aes.NewCipher(gcmKey)
		if err != nil {
			panic(fmt.Sprintf("[crypto] aes.NewCipher failed: %v", err))
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			panic(fmt.Sprintf("[crypto] cipher.NewGCM failed: %v", err))
		}
		gcmAEAD = aead
	})
}

// DecryptRealKey decrypts an AES-256-GCM ciphertext produced by the
// dashboard's encrypt() helper. Returns the plaintext API key.
//
// Accepts BOTH:
//   - new format: <iv-hex-24><tag-hex-32><ct-hex-N>  (AES-256-GCM)
//   - legacy plaintext fallback: returns input as-is if it doesn't
//     parse as GCM (so old keys seeded in Redis before encryption was
//     enabled still work during the rollout)
func DecryptRealKey(payload string) (string, error) {
	if payload == "" {
		return "", errors.New("empty payload")
	}

	// GCM payload: 24 hex (12 byte IV) + 32 hex (16 byte tag) + at least
	// 1 byte of ciphertext = at least 58 hex chars, all in [0-9a-f].
	if len(payload) >= 58 && isHex(payload) {
		loadGCMKey()
		iv, err := hex.DecodeString(payload[:24])
		if err != nil {
			return payload, nil // legacy plaintext
		}
		tag, err := hex.DecodeString(payload[24:56])
		if err != nil {
			return payload, nil
		}
		ct, err := hex.DecodeString(payload[56:])
		if err != nil {
			return payload, nil
		}
		pt, err := gcmAEAD.Open(nil, iv, append(ct, tag...), nil)
		if err == nil {
			return string(pt), nil
		}
		// GCM open failed (wrong key, tampered) â€” fall back to plaintext
		// for legacy keys seeded before encryption was enabled.
		return payload, nil
	}
	// Not a valid hex payload â†’ plaintext (legacy)
	return payload, nil
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
