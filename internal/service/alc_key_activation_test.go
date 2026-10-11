package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/store"
	"github.com/tabloy/keygate/pkg/apperr"
)

// The public activation API must accept an ALC-format key exactly like a
// KG- key: same lookup by hash, same status and activation-limit checks.
// Integration test: needs TEST_DATABASE_URL and only touches rows it
// creates itself (its own products, plans and licenses).
func TestActivationWithALCAndLegacyKeys(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping integration test: TEST_DATABASE_URL not set")
	}
	s, err := store.New(dsn)
	if err != nil {
		t.Skipf("skipping integration test: %v", err)
	}
	defer s.Close()
	if err := s.RunMigrations("../../db/migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	ctx := context.Background()
	suffix := time.Now().Format("150405.000")

	mkPlan := func(tag string) *model.Plan {
		prod := &model.Product{Name: tag, Slug: "ak-" + tag + "-" + suffix, Type: "hybrid"}
		if err := s.CreateProduct(ctx, prod); err != nil {
			t.Fatalf("create product: %v", err)
		}
		plan := &model.Plan{ProductID: prod.ID, Name: tag, Slug: "akp-" + tag + "-" + suffix, LicenseType: "perpetual", LicenseModel: "standard", MaxActivations: 1}
		if err := s.CreatePlan(ctx, plan); err != nil {
			t.Fatalf("create plan: %v", err)
		}
		return plan
	}
	alcPlan, otherPlan := mkPlan("alc"), mkPlan("other")
	s.KeyPolicy = license.NewKeyPolicy([]string{alcPlan.ProductID})

	issue := func(plan *model.Plan, email string) *model.License {
		key, err := s.NewLicenseKey(ctx, plan.ProductID)
		if err != nil {
			t.Fatalf("new key: %v", err)
		}
		l := &model.License{ProductID: plan.ProductID, PlanID: plan.ID, Email: email, LicenseKey: key, Status: model.StatusActive}
		if err := s.CreateLicense(ctx, l); err != nil {
			t.Fatalf("create license: %v", err)
		}
		return l
	}
	alcLic, kgLic := issue(alcPlan, "alc-act@example.com"), issue(otherPlan, "kg-act@example.com")
	if !license.ValidALCKey(alcLic.LicenseKey) || !strings.HasPrefix(kgLic.LicenseKey, "KG-") {
		t.Fatalf("unexpected formats: %q / %q", alcLic.LicenseKey, kgLic.LicenseKey)
	}

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	svc := NewLicenseService(s, priv, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	activate := func(key, device string) (*ActivateResult, error) {
		return svc.Activate(ctx, ActivateInput{LicenseKey: key, Identifier: device, IPAddress: "203.0.113.9"})
	}
	code := func(err error) string {
		var ae *apperr.AppError
		if errors.As(err, &ae) {
			return ae.Code
		}
		return ""
	}

	for name, lic := range map[string]*model.License{"ALC": alcLic, "KG": kgLic} {
		// Case-insensitive key, first device gets the single slot.
		r, err := activate(strings.ToLower(lic.LicenseKey), "dev-1")
		if err != nil || r.Status != "activated" || r.Token == "" {
			t.Fatalf("%s: first activation = %+v, %v", name, r, err)
		}
		// Same device again: already activated, no extra slot used.
		if r, err = activate(lic.LicenseKey, "dev-1"); err != nil || r.Status != "already_activated" {
			t.Fatalf("%s: repeat activation = %+v, %v", name, r, err)
		}
		// Activation limit (1) unchanged: a second device is refused.
		if _, err = activate(lic.LicenseKey, "dev-2"); err == nil {
			t.Fatalf("%s: a second device must hit the activation limit", name)
		}
	}
	// Unknown and modified keys are rejected with LICENSE_NOT_FOUND.
	mod := alcLic.LicenseKey[:17] + map[bool]string{true: "B", false: "A"}[alcLic.LicenseKey[17] == 'A']
	for _, bad := range []string{mod, license.GenerateALCKey(), "ALC-AAAA-AAAA-AAAA", "KG-NOPE"} {
		if _, err := activate(bad, "dev-x"); code(err) != "LICENSE_NOT_FOUND" {
			t.Fatalf("key %q: want LICENSE_NOT_FOUND, got %v", bad, err)
		}
	}
}
