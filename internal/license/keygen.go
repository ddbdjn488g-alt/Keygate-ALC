package license

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateKey creates a high-entropy license key: PREFIX-XXXXXXXX-XXXXXXXX-XXXXXXXX-XXXXXXXX
// 32 random chars from a 32-char alphabet = 160 bits of entropy.
func GenerateKey(prefix string) string {
	if prefix == "" {
		prefix = "KG"
	}
	segments := make([]string, 4)
	for i := range segments {
		segments[i] = randomSegment(8)
	}
	return prefix + "-" + strings.Join(segments, "-")
}

// HashKey returns a SHA-256 hash of the license key for secure storage.
func HashKey(key string) string {
	h := sha256.Sum256([]byte(normalizeKey(key)))
	return hex.EncodeToString(h[:])
}

// normalizeKey strips whitespace and uppercases for consistent hashing.
func normalizeKey(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}

func randomSegment(n int) string {
	// Rejection sampling avoids modulo bias: only byte values below the
	// largest multiple of the alphabet length are used. With the current
	// 32 characters that is every byte, but the bound follows the
	// alphabet so a change to it cannot bring the bias back.
	const maxUnbiased = 256 - 256%len(alphabet)
	out := make([]byte, n)
	buf := make([]byte, 1)
	for i := 0; i < n; {
		if _, err := rand.Read(buf); err != nil {
			panic(fmt.Sprintf("crypto/rand: %v", err))
		}
		if int(buf[0]) >= maxUnbiased {
			continue // reject biased values
		}
		out[i] = alphabet[buf[0]%byte(len(alphabet))]
		i++
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// ALC key format: ALC-XXXX-XXXX-CCCC
//
// The ALC desktop application validates a key locally (it does not call
// Keygate): exactly 18 characters, the "ALC-" prefix, dashes at indexes 3, 8
// and 13, upper-case ASCII letters or digits elsewhere, and a checksum: the
// third group CCCC must equal alcChecksum(group1 + group2).
//
// SECURITY LIMITS (read before relying on this format):
//   - The two random groups carry about 40 bits (8 characters of a 32-letter
//     alphabet), not the 160 bits of the default Keygate key.
//   - The checksum is a public, non-secret function embedded in the ALC
//     binary: anyone who reads it can forge a key ALC accepts locally.
//   - ALC does not verify the key with Keygate, so revocation, suspension and
//     the activation counter kept by Keygate are not seen by the desktop app.
//
// This format makes Keygate-issued keys ACCEPTED by the current ALC
// validator. It is NOT a complete licensing scheme: online activation and
// signed tokens remain to be integrated and validated separately.
// ---------------------------------------------------------------------------

const (
	alcPrefix           = "ALC"
	alcChecksumAlphabet = "0123456789ABCDEFGHJKLMNPQRSTUVWXYZ" // 34 characters, no I / O (as in ALC)
	alcKeyLength        = 18
)

// GenerateALCKey returns a new key in the ALC format. The two random groups
// reuse the Keygate alphabet (a subset of the characters ALC accepts).
func GenerateALCKey() string {
	g1, g2 := randomSegment(4), randomSegment(4)
	return alcPrefix + "-" + g1 + "-" + g2 + "-" + ALCChecksum(g1+g2)
}

// ALCChecksum reproduces ALC's LicenseManager::validateKeyChecksum: a uint32
// hash seeded with 0x5A3C over the 8 characters of groups 1 and 2, written
// as 4 base-34 digits (least significant first).
func ALCChecksum(payload string) string {
	hash := uint32(0x5A3C)
	for _, r := range payload {
		hash = ((hash << 5) + hash) + uint32(r)
		hash ^= 0xB7E1
	}
	base := uint32(len(alcChecksumAlphabet))
	out := make([]byte, 4)
	for i := range out {
		out[i] = alcChecksumAlphabet[hash%base]
		hash /= base
	}
	return string(out)
}

// ValidALCKey mirrors ALC's validateKeyFormat + validateKeyChecksum on an
// already trimmed, upper-cased key.
func ValidALCKey(key string) bool {
	if len(key) != alcKeyLength || !strings.HasPrefix(key, alcPrefix+"-") {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if i == 3 || i == 8 || i == 13 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return key[14:18] == ALCChecksum(key[4:8]+key[9:13])
}

// KeyPolicy chooses the key format per product. Products listed explicitly
// (by their real product ID) get ALC-format keys; every other product keeps
// the default Keygate format. The zero value keeps the default for all.
type KeyPolicy struct {
	alcProducts map[string]struct{}
}

// NewKeyPolicy builds a policy from the product IDs that use the ALC format.
func NewKeyPolicy(alcProductIDs []string) KeyPolicy {
	p := KeyPolicy{alcProducts: map[string]struct{}{}}
	for _, id := range alcProductIDs {
		if id = strings.TrimSpace(id); id != "" {
			p.alcProducts[id] = struct{}{}
		}
	}
	return p
}

// UsesALCFormat reports whether productID is configured for ALC-format keys.
func (p KeyPolicy) UsesALCFormat(productID string) bool {
	_, ok := p.alcProducts[productID]
	return ok
}

// Generate returns a new key in the format configured for productID.
func (p KeyPolicy) Generate(productID string) string {
	if p.UsesALCFormat(productID) {
		return GenerateALCKey()
	}
	return GenerateKey("")
}
