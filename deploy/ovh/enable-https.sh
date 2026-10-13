#!/bin/bash
# Opens ports 80/443 and starts Caddy ONLY when the domain already points to this server, then checks HTTPS for real.
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
DOMAIN=${KEYGATE_DOMAIN:-api.alc-control.com}
PUBLIC_IP=${PUBLIC_IP:-$(ip -4 route get 1.1.1.1 | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}')}
echo "domain: $DOMAIN  server IPv4: $PUBLIC_IP"
got=$(dig +short A "$DOMAIN" @1.1.1.1 | tail -1 || true)
if [ "$got" != "$PUBLIC_IP" ]; then
  echo "DNS not ready: $DOMAIN resolves to '${got:-nothing}', expected $PUBLIC_IP." >&2
  echo "Add at the DNS provider:   $DOMAIN.   A   $PUBLIC_IP   (TTL 300)   then retry (propagation can take a few minutes)." >&2
  exit 1
fi
curl -fsS -m 5 http://127.0.0.1:9000/health >/dev/null || { echo "Keygate is not healthy on 127.0.0.1:9000: install a release first" >&2; exit 1; }
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
ufw allow 80/tcp comment 'caddy http (ACME + redirect)' >/dev/null
ufw allow 443/tcp comment 'caddy https' >/dev/null
systemctl enable --now caddy
echo "waiting for the certificate..."
for _ in $(seq 1 60); do
  if curl -fsS -m 5 "https://$DOMAIN/health" >/dev/null 2>&1; then
    echo "HTTPS OK: https://$DOMAIN/health"
    echo | openssl s_client -servername "$DOMAIN" -connect "$DOMAIN:443" 2>/dev/null | openssl x509 -noout -issuer -subject -dates
    exit 0
  fi
  sleep 2
done
echo "HTTPS not reachable after 120 s. Caddy log:" >&2
journalctl -u caddy -n 25 --no-pager >&2 || true
exit 1
