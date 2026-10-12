package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/store"
	"github.com/tabloy/keygate/pkg/apperr"
)

// End-to-end server flow the ALC desktop client relies on: activation,
// idempotent retry, one-device limit (including concurrent attempts), signed
// proof, renewal through verify, revocation, device release and no leakage of
// the full key into logs or errors. Integration test: TEST_DATABASE_URL, own
// rows only.
func TestALCOnlineActivationFlow(t *testing.T) {
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
		prod := &model.Product{Name: tag, Slug: "of-" + tag + "-" + suffix, Type: "hybrid"}
		if err := s.CreateProduct(ctx, prod); err != nil {
			t.Fatal(err)
		}
		plan := &model.Plan{ProductID: prod.ID, Name: tag, Slug: "ofp-" + tag + "-" + suffix, LicenseType: "perpetual", LicenseModel: "standard", MaxActivations: 1, TokenTTLDays: 7}
		if err := s.CreatePlan(ctx, plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	alcPlan, otherPlan := mkPlan("alc"), mkPlan("other")
	s.KeyPolicy = license.NewKeyPolicy([]string{alcPlan.ProductID})
	issue := func(plan *model.Plan, email string) (*model.License, string) {
		key, err := s.NewLicenseKey(ctx, plan.ProductID)
		if err != nil {
			t.Fatal(err)
		}
		l := &model.License{ProductID: plan.ProductID, PlanID: plan.ID, Email: email, LicenseKey: key, Status: model.StatusActive}
		if err := s.CreateLicense(ctx, l); err != nil {
			t.Fatal(err)
		}
		return l, key
	}
	lic, key := issue(alcPlan, "flow@example.com")
	_, otherKey := issue(otherPlan, "flow-other@example.com")

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	slog.SetDefault(logger)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	svc := NewLicenseService(s, priv, logger, nil, nil)
	code := func(err error) string {
		var ae *apperr.AppError
		if errors.As(err, &ae) {
			return ae.Code
		}
		return ""
	}
	activate := func(k, dev string) (*ActivateResult, error) {
		return svc.Activate(ctx, ActivateInput{LicenseKey: k, Identifier: dev, ProductID: alcPlan.ProductID, IPAddress: "203.0.113.7"})
	}
	count := func() int {
		n, err := s.DB.NewSelect().Model((*model.Activation)(nil)).Where("license_id = ?", lic.ID).Count(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 1. First activation: recorded, signed proof bound to license, product and device.
	r, err := activate(key, "mac-A")
	if err != nil || r.Status != "activated" {
		t.Fatalf("first activation: %+v %v", r, err)
	}
	tok, err := license.Verify(r.Token, pub)
	if err != nil {
		t.Fatalf("proof must verify with the public key: %v", err)
	}
	if tok.LicenseID != lic.ID || tok.ProductID != alcPlan.ProductID || tok.Identifier != "mac-A" ||
		tok.Fingerprint != license.Fingerprint("mac-A", alcPlan.ProductID) {
		t.Fatalf("proof does not bind license/product/device: %+v", tok)
	}
	if d := time.Until(time.Unix(tok.ExpiresAt, 0)); d < 6*24*time.Hour || d > 7*24*time.Hour+time.Minute {
		t.Fatalf("offline window should be the configured 7 days, got %v", d)
	}
	if tok.ValidUntil != 0 {
		t.Fatal("a perpetual license must not carry an end date")
	}
	// A tampered proof is rejected.
	if _, err := license.Verify(strings.Replace(r.Token, ".", "A.", 1), pub); err == nil {
		t.Fatal("tampered proof must be rejected")
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := license.Verify(r.Token, otherPub); err == nil {
		t.Fatal("proof must not verify under another key")
	}
	if count() != 1 {
		t.Fatalf("expected 1 stored activation, got %d", count())
	}

	// 2. Same device again: idempotent, no extra slot.
	if r, err = activate(key, "mac-A"); err != nil || r.Status != "already_activated" || count() != 1 {
		t.Fatalf("repeat: %+v %v count=%d", r, err, count())
	}

	// 3. Second device refused (clearly), also under concurrency.
	if _, err = activate(key, "mac-B"); code(err) != "ACTIVATION_LIMIT" {
		t.Fatalf("second device: want ACTIVATION_LIMIT, got %v", err)
	}
	var wg sync.WaitGroup
	wins := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := activate(key, "race-"+string(rune('a'+i))); err == nil {
				wins <- "ok"
			}
		}(i)
	}
	wg.Wait()
	close(wins)
	if len(wins) != 0 || count() != 1 {
		t.Fatalf("concurrent attempts beyond the limit: wins=%d count=%d", len(wins), count())
	}

	// 4. Renewal through verify returns a fresh proof; another device cannot renew.
	v, err := svc.Verify(ctx, VerifyInput{LicenseKey: key, Identifier: "mac-A", ProductID: alcPlan.ProductID, IPAddress: "203.0.113.7"})
	if err != nil {
		t.Fatalf("renewal: %v", err)
	}
	if t2, err := license.Verify(v.Token, pub); err != nil || t2.Identifier != "mac-A" || t2.ExpiresAt < tok.ExpiresAt {
		t.Fatalf("renewed proof invalid: %v", err)
	}
	if _, err := svc.Verify(ctx, VerifyInput{LicenseKey: key, Identifier: "mac-B", ProductID: alcPlan.ProductID}); code(err) != "LICENSE_NOT_FOUND" {
		t.Fatalf("another device must not renew: %v", err)
	}

	// 5. Unknown key, modified key, wrong product.
	mod := key[:17] + map[bool]string{true: "B", false: "A"}[key[17] == 'A']
	for _, bad := range []string{mod, license.GenerateALCKey()} {
		if _, err := activate(bad, "mac-A"); code(err) != "LICENSE_NOT_FOUND" {
			t.Fatalf("unknown key: %v", err)
		}
	}
	if _, err := svc.Activate(ctx, ActivateInput{LicenseKey: otherKey, Identifier: "mac-A", ProductID: alcPlan.ProductID}); code(err) != "LICENSE_NOT_FOUND" {
		t.Fatalf("key of another product must be refused: %v", err)
	}

	// 6. Authorised release (device or admin) frees the slot for a new device.
	if err := svc.Deactivate(ctx, DeactivateInput{LicenseKey: key, Identifier: "mac-A", ProductID: alcPlan.ProductID}); err != nil || count() != 0 {
		t.Fatalf("deactivate: %v count=%d", err, count())
	}
	if r, err = activate(key, "mac-B"); err != nil || r.Status != "activated" {
		t.Fatalf("new device after release: %+v %v", r, err)
	}
	act, err := s.FindActivation(ctx, lic.ID, "mac-B")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteActivationByID(ctx, act.ID); err != nil || count() != 0 {
		t.Fatalf("admin release: %v", err)
	}
	if r, err = activate(key, "mac-A"); err != nil || r.Status != "activated" {
		t.Fatalf("re-activation: %+v %v", r, err)
	}

	// 7. Revocation: no activation of a new device and no renewal.
	if _, err := s.DB.NewRaw("UPDATE licenses SET status = 'revoked' WHERE id = ?", lic.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, VerifyInput{LicenseKey: key, Identifier: "mac-A", ProductID: alcPlan.ProductID}); err == nil {
		t.Fatal("a revoked license must not renew")
	}
	if _, err := activate(key, "mac-C"); err == nil {
		t.Fatal("a revoked license must not activate")
	}

	// 8. The full key never reaches the logs.
	if strings.Contains(logs.String(), key) {
		t.Fatal("full license key found in logs")
	}
}
