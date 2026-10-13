package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func clientIPSeenBy(t *testing.T, proxies []string, remote, xff string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies(proxies); err != nil {
		t.Fatal(err)
	}
	r.RemoteIPHeaders = []string{"X-Forwarded-For", "X-Real-Ip"}
	var got string
	r.GET("/", func(c *gin.Context) { got = c.ClientIP() })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote + ":4242"
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	r.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// A proxy on the same host connects from loopback. With the default (private
// ranges only) its forwarded client IP is ignored and every visitor shares one
// address, so rate limits and the brute-force guard hit everybody at once.
func TestLoopbackProxyNeedsTrustedProxies(t *testing.T) {
	def, err := trustedProxyCIDRs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := clientIPSeenBy(t, def, "127.0.0.1", "203.0.113.9"); got != "127.0.0.1" {
		t.Fatalf("default list must not trust loopback, saw %s", got)
	}
	cfg, err := trustedProxyCIDRs([]string{"127.0.0.1/32", "::1/128"})
	if err != nil {
		t.Fatal(err)
	}
	if got := clientIPSeenBy(t, cfg, "127.0.0.1", "203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("configured loopback proxy: want the forwarded IP, saw %s", got)
	}
	// A direct client (not a trusted proxy) cannot spoof the header.
	if got := clientIPSeenBy(t, cfg, "198.51.100.7", "203.0.113.9"); got != "198.51.100.7" {
		t.Fatalf("untrusted peer must not be able to spoof its IP, saw %s", got)
	}
	// Private ranges stay trusted by default (Blitz / container platforms).
	if got := clientIPSeenBy(t, def, "10.244.1.2", "203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("default private proxy: want the forwarded IP, saw %s", got)
	}
}

func TestTrustedProxyCIDRsValidation(t *testing.T) {
	for _, bad := range [][]string{{"not-an-ip"}, {"127.0.0.1/99"}, {"127.0.0.1/32", "oops"}} {
		if _, err := trustedProxyCIDRs(bad); err == nil {
			t.Errorf("%v must be rejected", bad)
		}
	}
	if got, err := trustedProxyCIDRs([]string{"127.0.0.1", "::1"}); err != nil || len(got) != 2 {
		t.Errorf("single IPs must be accepted: %v %v", got, err)
	}
}
