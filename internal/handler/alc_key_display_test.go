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

	"github.com/tabloy/keygate/internal/crypto"
	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/store"
)

// The admin list, the license detail and the customer detail must label a
// masked key with ITS OWN format prefix (ALC-... / KG-...) and never carry
// the full key. Integration test (TEST_DATABASE_URL), own rows only, with
// encryption at rest enabled as on the deployed server.
func TestAdminKeyMaskPrefixMatchesKeyFormat(t *testing.T) {
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
	s.LicenseKeyAEAD = crypto.MustDeriveAEAD([]byte("0123456789abcdef0123456789abcdef"), "license-key")
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	suffix := time.Now().Format("150405.000")
	email := "mask-" + suffix + "@example.com"

	mk := func(tag string) *model.Plan {
		prod := &model.Product{Name: tag, Slug: "mk-" + tag + "-" + suffix, Type: "hybrid"}
		if err := s.CreateProduct(ctx, prod); err != nil {
			t.Fatal(err)
		}
		plan := &model.Plan{ProductID: prod.ID, Name: tag, Slug: "mkp-" + tag + "-" + suffix, LicenseType: "perpetual", LicenseModel: "standard", MaxActivations: 1}
		if err := s.CreatePlan(ctx, plan); err != nil {
			t.Fatal(err)
		}
		return plan
	}
	alcPlan, kgPlan := mk("alc"), mk("kg")
	s.KeyPolicy = license.NewKeyPolicy([]string{alcPlan.ProductID})
	issue := func(plan *model.Plan) *model.License {
		key, err := s.NewLicenseKey(ctx, plan.ProductID)
		if err != nil {
			t.Fatal(err)
		}
		l := &model.License{ProductID: plan.ProductID, PlanID: plan.ID, Email: email, LicenseKey: key, Status: model.StatusActive}
		if err := s.CreateLicense(ctx, l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	alcLic, kgLic := issue(alcPlan), issue(kgPlan)
	alcKey, kgKey := s.DecryptLicenseKey(alcLic), s.DecryptLicenseKey(kgLic)
	tail := func(k string) string { return k[len(k)-4:] }
	h := &AdminHandler{Store: s}

	call := func(fn gin.HandlerFunc, target string, params gin.Params) (string, map[string]any) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, target, nil)
		c.Params = params
		fn(c)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d body %s", target, w.Code, w.Body.String())
		}
		var out struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return w.Body.String(), out.Data
	}
	noFullKey := func(body string) {
		for _, k := range []string{alcKey, kgKey} {
			if strings.Contains(body, k) {
				t.Fatal("a response carries a full license key")
			}
		}
	}
	str := func(m any, id string) string {
		mm, _ := m.(map[string]any)
		v, _ := mm[id].(string)
		return v
	}

	// List: prefixes + hints per license id.
	body, data := call(h.ListLicenses, "/admin/licenses?email="+email+"&limit=50", nil)
	noFullKey(body)
	if str(data["license_key_prefixes"], alcLic.ID) != "ALC" || str(data["license_key_prefixes"], kgLic.ID) != "KG" {
		t.Fatalf("list prefixes wrong: %v", data["license_key_prefixes"])
	}
	if str(data["license_key_hints"], alcLic.ID) != tail(alcKey) || str(data["license_key_hints"], kgLic.ID) != tail(kgKey) {
		t.Fatal("list hints (last four) changed")
	}

	// Detail: license_key_prefix next to the unchanged license_key_hint.
	for _, tc := range []struct {
		lic    *model.License
		key    string
		prefix string
	}{{alcLic, alcKey, "ALC"}, {kgLic, kgKey, "KG"}} {
		body, d := call(h.GetLicense, "/admin/licenses/"+tc.lic.ID, gin.Params{{Key: "id", Value: tc.lic.ID}})
		noFullKey(body)
		if d["license_key_prefix"] != tc.prefix || d["license_key_hint"] != tail(tc.key) {
			t.Fatalf("detail for %s: prefix %v hint %v", tc.prefix, d["license_key_prefix"], d["license_key_hint"])
		}
	}

	// Customer detail.
	u := &model.User{Email: email}
	if err := s.UpsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	body, d := call(h.GetUserDetail, "/admin/users/"+u.ID, gin.Params{{Key: "id", Value: u.ID}})
	noFullKey(body)
	if str(d["license_key_prefixes"], alcLic.ID) != "ALC" || str(d["license_key_prefixes"], kgLic.ID) != "KG" {
		t.Fatalf("customer prefixes wrong: %v", d["license_key_prefixes"])
	}
}
