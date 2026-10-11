package license

import (
	"strings"
	"testing"
)

// Checksum vectors computed independently from ALC's
// LicenseManager::validateKeyChecksum (uint32 hash seeded with 0x5A3C,
// hash = hash*33 + c, ^= 0xB7E1, then 4 base-34 digits, least significant
// first). They pin the Go port to the desktop validator.
func TestALCChecksumVectors(t *testing.T) {
	for payload, want := range map[string]string{
		"ABCDEFGH": "LESM",
		"23456789": "A9T2",
		"KMNPQRST": "CBY1",
	} {
		if got := ALCChecksum(payload); got != want {
			t.Errorf("ALCChecksum(%q) = %q, want %q", payload, got, want)
		}
	}
}

func TestGenerateALCKeyFormat(t *testing.T) {
	for range 2000 {
		key := GenerateALCKey()
		if len(key) != 18 || !strings.HasPrefix(key, "ALC-") {
			t.Fatalf("bad shape: %q", key)
		}
		for i, c := range key {
			switch {
			case i == 3 || i == 8 || i == 13:
				if c != '-' {
					t.Fatalf("expected '-' at %d in %q", i, key)
				}
			case !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9'):
				t.Fatalf("character %q at %d not upper-case alphanumeric in %q", c, i, key)
			}
		}
		if key != strings.ToUpper(key) {
			t.Fatalf("key not upper-case: %q", key)
		}
		if !ValidALCKey(key) {
			t.Fatalf("generated key rejected by the ALC validator mirror: %q", key)
		}
		// Groups 1 and 2 only use the Keygate alphabet.
		for _, c := range key[4:8] + key[9:13] {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatalf("random group character %q outside the alphabet in %q", c, key)
			}
		}
	}
}

func TestGenerateALCKeyUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for range 5000 {
		k := GenerateALCKey()
		if seen[k] {
			t.Fatalf("duplicate key in 5000 draws: %q", k)
		}
		seen[k] = true
	}
}

func TestValidALCKeyRejects(t *testing.T) {
	good := GenerateALCKey()
	tamper := func(i int, c byte) string { b := []byte(good); b[i] = c; return string(b) }
	cases := map[string]string{
		"empty":            "",
		"keygate default":  GenerateKey(""),
		"wrong prefix":     "XYZ" + good[3:],
		"lower-case":       strings.ToLower(good),
		"too short":        good[:17],
		"too long":         good + "A",
		"bad separator":    tamper(8, 'X'),
		"modified group 1": tamper(4, map[bool]byte{true: 'B', false: 'A'}[good[4] == 'A']),
		"modified group 3": tamper(17, map[bool]byte{true: 'B', false: 'A'}[good[17] == 'A']),
		"non alnum":        tamper(5, '*'),
	}
	for name, key := range cases {
		if ValidALCKey(key) {
			t.Errorf("%s: %q must be rejected", name, key)
		}
	}
	if !ValidALCKey(good) {
		t.Errorf("good key %q rejected", good)
	}
}

func TestKeyPolicyPerProduct(t *testing.T) {
	p := NewKeyPolicy([]string{" prod-alc ", "", "prod-other"})
	if !p.UsesALCFormat("prod-alc") || !p.UsesALCFormat("prod-other") {
		t.Fatal("configured products must use the ALC format")
	}
	if p.UsesALCFormat("prod-keygate") || p.UsesALCFormat("") {
		t.Fatal("unlisted products must keep the default format")
	}
	if !ValidALCKey(p.Generate("prod-alc")) {
		t.Fatal("ALC product must get an ALC key")
	}
	other := p.Generate("prod-keygate")
	if !strings.HasPrefix(other, "KG-") || len(strings.Split(other, "-")) != 5 {
		t.Fatalf("other product must keep the KG- format, got %q", other)
	}
	var zero KeyPolicy
	if !strings.HasPrefix(zero.Generate("prod-alc"), "KG-") {
		t.Fatal("zero policy must keep the default format for every product")
	}
}

// HashKey normalisation must treat an ALC key like any other key (stored
// and looked up by the SHA-256 of the upper-cased, trimmed key).
func TestHashKeyALCNormalization(t *testing.T) {
	k := GenerateALCKey()
	if HashKey(k) != HashKey("  "+strings.ToLower(k)+" ") {
		t.Fatal("hash must ignore case and surrounding whitespace")
	}
}
