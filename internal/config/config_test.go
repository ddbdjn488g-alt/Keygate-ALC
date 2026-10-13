package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/tabloy/keygate/internal/license"
)

func TestIsDevLoginAllowed(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{"development", true},
		{"Development", true},
		{"DEVELOPMENT", true},
		{" development ", true},
		{"production", false},
		{"staging", false},
		{"", false},
		{"dev", false},
	}
	for _, tt := range tests {
		c := &Config{Environment: tt.env}
		got := c.IsDevLoginAllowed()
		if got != tt.want {
			t.Errorf("IsDevLoginAllowed(%q) = %v, want %v", tt.env, got, tt.want)
		}
	}
}

func TestIsProduction(t *testing.T) {
	tests := []struct {
		env  string
		want bool
	}{
		{"production", true},
		{"development", false},
		{"staging", false},
		{"", false},
	}
	for _, tt := range tests {
		c := &Config{Environment: tt.env}
		if got := c.IsProduction(); got != tt.want {
			t.Errorf("IsProduction(%q) = %v, want %v", tt.env, got, tt.want)
		}
	}
}

func TestIsAdminEmail(t *testing.T) {
	c := &Config{AdminEmails: []string{"admin@keygate.dev", "boss@company.com"}}

	tests := []struct {
		email string
		want  bool
	}{
		{"admin@keygate.dev", true},
		{"ADMIN@KEYGATE.DEV", true},
		{"boss@company.com", true},
		{"user@other.com", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := c.IsAdminEmail(tt.email); got != tt.want {
			t.Errorf("IsAdminEmail(%q) = %v, want %v", tt.email, got, tt.want)
		}
	}
}

func TestValidateSecurityDefaults(t *testing.T) {
	t.Run("valid dev config", func(t *testing.T) {
		c := &Config{
			Environment: "development",
			JWTSecret:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			// 32-byte ed25519 seed in hex (64 chars).
			LicenseSigningKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}
		warnings, fatal := c.ValidateSecurityDefaults()
		if len(fatal) > 0 {
			t.Errorf("unexpected fatal: %v", fatal)
		}
		// Should warn about dev login
		found := false
		for _, w := range warnings {
			if w == "SECURITY: dev-login is enabled (ENVIRONMENT=development) — do NOT use in production" {
				found = true
			}
		}
		if !found {
			t.Error("expected dev-login warning")
		}
	})

	t.Run("production over plain http warns about the session cookie", func(t *testing.T) {
		base := func(url string) *Config {
			return &Config{
				Environment:       "production",
				BaseURL:           url,
				AdminEmails:       []string{"admin@example.com"},
				JWTSecret:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				LicenseSigningKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			}
		}
		const want = "SECURITY: BASE_URL is not https — session cookies travel unprotected; put TLS in front of Keygate"
		warned := func(url string) bool {
			warnings, fatal := base(url).ValidateSecurityDefaults()
			if len(fatal) > 0 {
				t.Fatalf("unexpected fatal for %s: %v", url, fatal)
			}
			return slices.Contains(warnings, want)
		}
		// A remote install over plain HTTP is the one that cannot log
		// anybody in: the browser drops a Secure cookie, and without
		// it the session travels in the clear.
		if !warned("http://keygate.example.com") {
			t.Error("no warning for a plain-http production install")
		}
		// Loopback is trustworthy to browsers, so a local trial is
		// quiet; TLS needs no warning either.
		if warned("http://localhost:9000") || warned("http://127.0.0.1:9000") {
			t.Error("warned about a loopback BASE_URL")
		}
		if warned("https://keygate.example.com") {
			t.Error("warned about an https BASE_URL")
		}
	})

	t.Run("short JWT secret", func(t *testing.T) {
		c := &Config{
			Environment: "production",
			JWTSecret:   "short",
			// 32-byte ed25519 seed in hex (64 chars).
			LicenseSigningKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}
		_, fatal := c.ValidateSecurityDefaults()
		if len(fatal) == 0 {
			t.Error("expected fatal for short JWT secret")
		}
	})

	t.Run("invalid environment", func(t *testing.T) {
		c := &Config{
			Environment: "typo",
			JWTSecret:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			// 32-byte ed25519 seed in hex (64 chars).
			LicenseSigningKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}
		_, fatal := c.ValidateSecurityDefaults()
		if len(fatal) == 0 {
			t.Error("expected fatal for invalid environment")
		}
	})

	t.Run("production without admin emails", func(t *testing.T) {
		c := &Config{
			Environment: "production",
			JWTSecret:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			// 32-byte ed25519 seed in hex (64 chars).
			LicenseSigningKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		}
		warnings, _ := c.ValidateSecurityDefaults()
		found := false
		for _, w := range warnings {
			if w == "SECURITY: ADMIN_EMAILS is empty — no one can access the admin panel" {
				found = true
			}
		}
		if !found {
			t.Error("expected admin emails warning in production")
		}
	})
}

// TestStripeLivemodeDerivation pins the rules described in
// deriveLivemode. The webhook handler rejects events whose Livemode
// flag doesn't match this config field, so any bug here directly
// enables cross-environment forgery (a leaked test secret being
// replayed at the prod endpoint, or vice versa).
func TestStripeLivemodeDerivation(t *testing.T) {
	cases := []struct {
		name      string
		envVal    string
		envSet    bool
		secretKey string
		want      bool
	}{
		// Explicit env wins regardless of key prefix.
		{"env true overrides sk_test", "true", true, "sk_test_xxx", true},
		{"env TRUE case-insensitive", "TRUE", true, "", true},
		{"env 1 numeric", "1", true, "", true},
		{"env false overrides sk_live", "false", true, "sk_live_xxx", false},
		{"env 0 numeric", "0", true, "sk_live_xxx", false},
		{"env garbage → false (only true/1 accepted)", "yes_please", true, "sk_live_xxx", false},
		{"env empty string set → false (env wins, value 'unset live')", "", true, "sk_live_xxx", false},

		// Without env: derive from key prefix.
		{"sk_live_ → true", "", false, "sk_live_abc", true},
		{"sk_test_ → false", "", false, "sk_test_abc", false},
		{"rk_live_ → true (restricted)", "", false, "rk_live_abc", true},
		{"rk_test_ → false (restricted)", "", false, "rk_test_abc", false},
		{"empty key → false (safe default)", "", false, "", false},
		{"unknown prefix (pk_live_) → false", "", false, "pk_live_abc", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveLivemode(tc.envVal, tc.envSet, tc.secretKey)
			if got != tc.want {
				t.Errorf("deriveLivemode(env=%q, set=%v, key=%q) = %v, want %v",
					tc.envVal, tc.envSet, tc.secretKey, got, tc.want)
			}
		})
	}
}

func TestALCKeyProductIDsFromEnv(t *testing.T) {
	t.Setenv("ALC_KEY_PRODUCT_IDS", " prod-a , ,prod-b ")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", strings.Repeat("s", 48))
	t.Setenv("LICENSE_SIGNING_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.ALCKeyProductIDs) != 2 || cfg.ALCKeyProductIDs[0] != "prod-a" || cfg.ALCKeyProductIDs[1] != "prod-b" {
		t.Fatalf("ALCKeyProductIDs = %v", cfg.ALCKeyProductIDs)
	}
}

// The ALC product (real Keygate product ID) is the only one that switches to
// ALC-format keys; any other product ID keeps the default format.
func TestALCKeyPolicyTargetsOnlyConfiguredProduct(t *testing.T) {
	const alcProductID = "afa82897-e92c-4cfe-9d9c-e04e198961b4"
	t.Setenv("ALC_KEY_PRODUCT_IDS", alcProductID)
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", strings.Repeat("s", 48))
	t.Setenv("LICENSE_SIGNING_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := license.NewKeyPolicy(cfg.ALCKeyProductIDs)
	if !license.ValidALCKey(p.Generate(alcProductID)) {
		t.Fatal("the ALC product must get ALC-format keys")
	}
	for _, other := range []string{"afa82897-e92c-4cfe-9d9c-e04e198961b5", "AFA82897-E92C-4CFE-9D9C-E04E198961B4", "", "autotune-live-control"} {
		if p.UsesALCFormat(other) {
			t.Fatalf("product %q must keep the default key format", other)
		}
		if !strings.HasPrefix(p.Generate(other), "KG-") {
			t.Fatalf("product %q must get a KG- key", other)
		}
	}
}

// A hosting dashboard may hand the variable over with surrounding quotes,
// padding, a trailing newline or another separator. The product ID must
// still be recognised: otherwise the policy silently falls back to KG- keys.
func TestALCKeyProductIDsTolerantParsing(t *testing.T) {
	const id = "afa82897-e92c-4cfe-9d9c-e04e198961b4"
	for name, raw := range map[string]string{
		"plain":            id,
		"double quotes":    `"` + id + `"`,
		"single quotes":    `'` + id + `'`,
		"padded":           "  " + id + "  ",
		"trailing newline": id + "\n",
		"crlf":             id + "\r\n",
		"semicolon list":   "other-id;" + id,
		"space list":       "other-id " + id,
		"comma + quotes":   `"other-id", "` + id + `"`,
		"BOM":              "\ufeff" + id,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ALC_KEY_PRODUCT_IDS", raw)
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv("JWT_SECRET", strings.Repeat("s", 48))
			t.Setenv("LICENSE_SIGNING_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !license.NewKeyPolicy(cfg.ALCKeyProductIDs).UsesALCFormat(id) {
				t.Fatalf("ALC product not recognised from %q (parsed %q)", raw, cfg.ALCKeyProductIDs)
			}
		})
	}
	t.Run("empty", func(t *testing.T) {
		t.Setenv("ALC_KEY_PRODUCT_IDS", " \"\" ")
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("JWT_SECRET", strings.Repeat("s", 48))
		t.Setenv("LICENSE_SIGNING_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
		cfg, err := Load()
		if err != nil || len(cfg.ALCKeyProductIDs) != 0 {
			t.Fatalf("empty value must yield no product: %v %v", cfg.ALCKeyProductIDs, err)
		}
	})
}

func TestTrustedProxiesFromEnv(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "127.0.0.1/32, ::1/128\n")
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", strings.Repeat("s", 48))
	t.Setenv("LICENSE_SIGNING_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "127.0.0.1/32" || cfg.TrustedProxies[1] != "::1/128" {
		t.Fatalf("TrustedProxies = %v", cfg.TrustedProxies)
	}
}
