package payment

import (
	"strings"
	"testing"

	"github.com/tabloy/keygate/internal/crypto"
	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
)

// An ALC-format key issued for a configured product is stored and found by
// the same path as any other key (SHA-256 of the normalised key); other
// products keep the KG- format. Runs only with TEST_DATABASE_URL (never
// against production data: it creates its own product and plan).
func TestALCKeyStoredAndFound(t *testing.T) {
	s, ctx := openStore(t)
	defer s.Close()
	alcPlan := seedPlan(t, s, ctx, "alckey", "perpetual")
	otherPlan := seedPlan(t, s, ctx, "otherkey", "perpetual")
	s.KeyPolicy = license.NewKeyPolicy([]string{alcPlan.ProductID})
	// Encryption at rest enabled, as on the deployed server.
	s.LicenseKeyAEAD = crypto.MustDeriveAEAD([]byte("0123456789abcdef0123456789abcdef"), "license-key")

	key, err := s.NewLicenseKey(ctx, alcPlan.ProductID)
	if err != nil || !license.ValidALCKey(key) {
		t.Fatalf("ALC key = %q, err %v", key, err)
	}
	lic := &model.License{ProductID: alcPlan.ProductID, PlanID: alcPlan.ID, Email: "alckey@example.com", LicenseKey: key, Status: model.StatusActive}
	if err := s.CreateLicense(ctx, lic); err != nil {
		t.Fatalf("create license: %v", err)
	}

	if got := s.DecryptLicenseKey(lic); got != key {
		t.Fatal("encrypted copy must decrypt to the issued key")
	}
	// Lookup is case/whitespace-insensitive, like the activation endpoints.
	for _, q := range []string{key, strings.ToLower(key), "  " + key + "  "} {
		found, err := s.FindLicenseByKey(ctx, q)
		if err != nil || found.ID != lic.ID {
			t.Fatalf("lookup %q: %v", q, err)
		}
	}
	// An unknown or modified key is not found.
	modified := key[:17] + map[bool]string{true: "B", false: "A"}[key[17] == 'A']
	if _, err := s.FindLicenseByKey(ctx, modified); err == nil {
		t.Fatal("modified key must not match")
	}
	// The same key cannot be stored twice (unique key_hash).
	dup := &model.License{ProductID: alcPlan.ProductID, PlanID: alcPlan.ID, Email: "dup@example.com", LicenseKey: key, Status: model.StatusActive}
	if err := s.CreateLicense(ctx, dup); err == nil {
		t.Fatal("duplicate key must be refused by the unique index")
	}
	// A newly generated key never reuses a stored hash.
	next, err := s.NewLicenseKey(ctx, alcPlan.ProductID)
	if err != nil || next == key {
		t.Fatalf("next key %q err %v", next, err)
	}
	// The other product keeps the default format.
	other, err := s.NewLicenseKey(ctx, otherPlan.ProductID)
	if err != nil || !strings.HasPrefix(other, "KG-") {
		t.Fatalf("other product key %q err %v", other, err)
	}
	// Legacy KG- keys still resolve exactly as before.
	legacy := &model.License{ProductID: otherPlan.ProductID, PlanID: otherPlan.ID, Email: "legacy@example.com", LicenseKey: other, Status: model.StatusActive}
	if err := s.CreateLicense(ctx, legacy); err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	if found, err := s.FindLicenseByKey(ctx, other); err != nil || found.ID != legacy.ID {
		t.Fatalf("legacy lookup: %v", err)
	}
}
