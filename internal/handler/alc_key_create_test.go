package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tabloy/keygate/internal/config"
	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/store"
)

// Admin license creation: the configured product gets an ALC-format key,
// any other product keeps the default KG- format, and the returned key is
// the one stored. Integration test (TEST_DATABASE_URL), own rows only.
func TestAdminCreateLicenseKeyFormatPerProduct(t *testing.T) {
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
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	suffix := time.Now().Format("150405.000")
	mk := func(tag string) *model.Plan {
		prod := &model.Product{Name: tag, Slug: "ck-" + tag + "-" + suffix, Type: "hybrid"}
		if err := s.CreateProduct(ctx, prod); err != nil {
			t.Fatal(err)
		}
		plan := &model.Plan{ProductID: prod.ID, Name: tag, Slug: "ckp-" + tag + "-" + suffix, LicenseType: "perpetual", LicenseModel: "standard", MaxActivations: 1}
		if err := s.CreatePlan(ctx, plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	alcPlan, otherPlan := mk("alc"), mk("other")
	// Same chain as the server: environment value (quoted and padded, as a
	// hosting dashboard may deliver it) -> config parsing -> policy -> handler.
	s.KeyPolicy = license.NewKeyPolicy(config.ParseProductIDs(" \"" + alcPlan.ProductID + "\"\n"))
	h := &AdminHandler{Store: s}

	create := func(plan *model.Plan, email string) (id, key string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := `{"product_id":"` + plan.ProductID + `","plan_id":"` + plan.ID + `","email":"` + email + `"}`
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/licenses", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.CreateLicense(c)
		if w.Code != http.StatusCreated {
			t.Fatalf("create license: status %d body %s", w.Code, w.Body.String())
		}
		var out struct {
			Data struct {
				ID         string `json:"id"`
				LicenseKey string `json:"license_key"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Data.ID, out.Data.LicenseKey
	}

	alcID, alcKey := create(alcPlan, "alc-create@example.com")
	if !license.ValidALCKey(alcKey) {
		t.Fatalf("ALC product: key not in ALC format")
	}
	kgID, kgKey := create(otherPlan, "kg-create@example.com")
	if !strings.HasPrefix(kgKey, "KG-") {
		t.Fatalf("other product: key not in the default KG- format")
	}
	for id, key := range map[string]string{alcID: alcKey, kgID: kgKey} {
		found, err := s.FindLicenseByKey(ctx, key)
		if err != nil || found.ID != id {
			t.Fatalf("stored key not found by lookup: %v", err)
		}
	}
}
