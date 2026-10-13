package main

import (
	"fmt"
	"net"
)

// defaultTrustedProxies are the private ranges a container platform or an
// internal load balancer connects from.
var defaultTrustedProxies = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

// trustedProxyCIDRs returns the proxies whose forwarding headers are trusted:
// the configured list (every entry validated as a CIDR or a single IP) or the
// default private ranges when none is configured.
func trustedProxyCIDRs(configured []string) ([]string, error) {
	if len(configured) == 0 {
		return defaultTrustedProxies, nil
	}
	for _, c := range configured {
		if _, _, err := net.ParseCIDR(c); err == nil {
			continue
		}
		if net.ParseIP(c) == nil {
			return nil, fmt.Errorf("%q is neither a CIDR nor an IP address", c)
		}
	}
	return configured, nil
}
