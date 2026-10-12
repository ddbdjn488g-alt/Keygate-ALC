package payment

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tabloy/keygate/internal/crypto"
	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/service"
	"github.com/tabloy/keygate/pkg/apperr"
)

// Purchase to activation, Stripe in test mode (stubbed API, really signed webhooks):
// Payment Link session -> checkout.session.completed webhook (signature, replay) ->
// ONE license for the right product and plan in ALC format -> key in the queued
// e-mail -> that key activates online on one device and is refused on a second one.
// Integration test: TEST_DATABASE_URL, own rows only. No real Stripe call, no real payment.
func TestALCPurchaseToActivation(t *testing.T) {
	s, ctx := openStore(t)
	defer s.Close()
	s.LicenseKeyAEAD = crypto.MustDeriveAEAD([]byte("0123456789abcdef0123456789abcdef"), "license-key")

	suffix := time.Now().Format("150405.000")
	priceID, sessionID := "price_alc_"+suffix, "cs_test_alc_"+suffix
	var lineItemCalls int32
	stubStripe(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkout/sessions/"+sessionID+"/line_items" {
			t.Errorf("unexpected Stripe call: %s %s", r.Method, r.URL.Path)
			http.Error(w, `{"error":{"message":"unexpected"}}`, http.StatusNotFound)
			return
		}
		atomic.AddInt32(&lineItemCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"object":"list","has_more":false,"url":"%s","data":[{"id":"li_1","object":"item","price":{"id":"%s","object":"price"}}]}`, r.URL.Path, priceID)
	})

	alcProd := &model.Product{Name: "Autotune Live Control (test)", Slug: "alcp-" + suffix, Type: "hybrid"}
	otherProd := &model.Product{Name: "Other (test)", Slug: "othp-" + suffix, Type: "hybrid"}
	for _, p := range []*model.Product{alcProd, otherProd} {
		if err := s.CreateProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	alcPlan := &model.Plan{ProductID: alcProd.ID, Name: "ALC Perpetuelle", Slug: "alcpp-" + suffix, LicenseType: "perpetual", LicenseModel: "standard", MaxActivations: 1, TokenTTLDays: 7, StripePriceID: priceID}
	otherPlan := &model.Plan{ProductID: otherProd.ID, Name: "Other", Slug: "othpp-" + suffix, LicenseType: "perpetual", LicenseModel: "standard", MaxActivations: 1, StripePriceID: "price_other_" + suffix}
	for _, p := range []*model.Plan{alcPlan, otherPlan} {
		if err := s.CreatePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	s.KeyPolicy = license.NewKeyPolicy([]string{alcProd.ID})

	var logs strings.Builder
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))

	secret := "whsec_alc_" + suffix
	h := &StripeHandler{Store: s, Email: &service.EmailService{}}
	h.SetWebhookSecret(secret)
	buyer := "buyer-" + suffix + "@example.com"
	session := map[string]any{
		"id": sessionID, "object": "checkout.session", "mode": "payment", "payment_status": "paid",
		"customer_details": map[string]any{"email": buyer}, "payment_intent": "pi_alc_" + suffix, "metadata": map[string]any{},
	}

	// A forged signature is refused and nothing is created.
	if code, _ := signedWebhook(t, h, "whsec_wrong", "evt_alc_bad_"+suffix, "checkout.session.completed", session); code != http.StatusBadRequest {
		t.Fatalf("forged signature: want 400, got %d", code)
	}
	if lics, _ := s.ListLicensesByEmail(ctx, buyer); len(lics) != 0 {
		t.Fatalf("forged event created %d license(s)", len(lics))
	}

	// The real event, then the same event again (Stripe retry), then another event for the same session.
	for _, id := range []string{"evt_alc_" + suffix, "evt_alc_" + suffix, "evt_alc_again_" + suffix} {
		if code, out := signedWebhook(t, h, secret, id, "checkout.session.completed", session); code != http.StatusOK {
			t.Fatalf("webhook %s: %d %v", id, code, out)
		}
	}
	lics, err := s.ListLicensesByEmail(ctx, buyer)
	if err != nil || len(lics) != 1 {
		t.Fatalf("one paid session must give exactly one license, got %d (%v)", len(lics), err)
	}
	lic := lics[0]
	key := s.DecryptLicenseKey(lic)
	if lic.ProductID != alcProd.ID || lic.PlanID != alcPlan.ID || lic.Status != model.StatusActive || lic.StripePaymentIntentID != "pi_alc_"+suffix {
		t.Fatalf("wrong product/plan/status/payment: %+v", lic)
	}
	if !license.ValidALCKey(key) {
		t.Fatal("license key for the ALC product is not in the ALC format")
	}

	// E-mail: queued once, carries the key, key never in the logs.
	var bodies []string
	if err := s.DB.NewRaw("SELECT body FROM email_queue WHERE to_addr = ?", buyer).Scan(ctx, &bodies); err != nil || len(bodies) != 1 {
		t.Fatalf("expected one queued e-mail, got %d (%v)", len(bodies), err)
	}
	if !strings.Contains(bodies[0], key) || !strings.Contains(bodies[0], "Autotune Live Control") {
		t.Fatal("queued e-mail lacks the key or the product name")
	}
	t.Cleanup(func() { _, _ = s.DB.NewRaw("DELETE FROM email_queue WHERE to_addr = ?", buyer).Exec(ctx) })
	if strings.Contains(logs.String(), key) {
		t.Fatal("full license key found in logs")
	}

	// The key read from the e-mail activates online (one device) and is refused on a second one.
	received := regexp.MustCompile(`ALC-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}`).FindString(bodies[0])
	if received != key {
		t.Fatal("the key found in the e-mail is not the issued key")
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	svc := service.NewLicenseService(s, priv, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	act := func(dev string) (*service.ActivateResult, error) {
		return svc.Activate(ctx, service.ActivateInput{LicenseKey: received, Identifier: dev, ProductID: alcProd.ID, IPAddress: "203.0.113.5"})
	}
	r1, err := act("mac-1")
	if err != nil {
		t.Fatalf("activation with the e-mailed key: %v", err)
	}
	if tok, err := license.Verify(r1.Token, pub); err != nil || tok.LicenseID != lic.ID || tok.Identifier != "mac-1" {
		t.Fatalf("proof invalid: %v", err)
	}
	if r, err := act("mac-1"); err != nil || r.Status != "already_activated" {
		t.Fatalf("same device must be idempotent: %+v %v", r, err)
	}
	var ae *apperr.AppError
	if _, err := act("mac-2"); !errors.As(err, &ae) || ae.Code != "ACTIVATION_LIMIT" {
		t.Fatalf("second device must be refused: %v", err)
	}
	if n, _ := s.DB.NewSelect().Model((*model.Activation)(nil)).Where("license_id = ?", lic.ID).Count(ctx); n != 1 {
		t.Fatalf("expected 1 stored activation, got %d", n)
	}
	if atomic.LoadInt32(&lineItemCalls) == 0 {
		t.Fatal("plan was not resolved from the Payment Link line items")
	}
}
