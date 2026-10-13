#!/bin/bash
# Enter a secret directly in the terminal (hidden, never in shell history, arguments or logs).
#   sudo keygate-set-secret SMTP_PASSWORD        (also: STRIPE_SECRET_KEY, STRIPE_WEBHOOK_SECRET)
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
key=${1:-}
case "$key" in SMTP_PASSWORD|STRIPE_SECRET_KEY|STRIPE_WEBHOOK_SECRET) ;; *) echo "usage: $0 SMTP_PASSWORD|STRIPE_SECRET_KEY|STRIPE_WEBHOOK_SECRET" >&2; exit 2 ;; esac
ENVF=/etc/keygate/keygate.env
read -rsp "$key (hidden): " val; echo
[ -n "$val" ] || { echo "empty value, nothing changed" >&2; exit 1; }
case "$val" in *$'\n'*|*$'\r'*) echo "newline in value refused" >&2; exit 1 ;; esac
esc=${val//\\/\\\\}; esc=${esc//\"/\\\"}
tmp=$(mktemp /etc/keygate/.env.XXXXXX); chmod 600 "$tmp"
grep -v "^$key=" "$ENVF" > "$tmp" || true
printf '%s="%s"\n' "$key" "$esc" >> "$tmp"
mv "$tmp" "$ENVF"; chmod 600 "$ENVF"; chown root:root "$ENVF"
unset val esc
echo "$key stored in $ENVF (value not displayed)."
if systemctl is-active --quiet keygate; then read -rp "Restart keygate now to apply? [y/N] " a; [ "$a" = y ] && systemctl restart keygate && echo restarted; fi
