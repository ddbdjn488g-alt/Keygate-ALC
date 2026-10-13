#!/bin/bash
# Real test of the OVH mailbox: authenticated submission, then YOU confirm reception.
#   sudo keygate-smtp-test-send you@example.com [host] [port]
# Defaults come from /etc/keygate/keygate.env (SMTP_HOST / SMTP_PORT / SMTP_USERNAME / SMTP_FROM).
# The password is read from SMTP_PASSWORD in that file or typed hidden; it is never printed or put on a command line.
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
rcpt=${1:?usage: $0 recipient@example.com [host] [port]}
ENVF=/etc/keygate/keygate.env
get() { grep -m1 "^$1=" "$ENVF" | cut -d= -f2- | sed -e 's/^"//' -e 's/"$//'; }
host=${2:-$(get SMTP_HOST)}; port=${3:-$(get SMTP_PORT)}; user=$(get SMTP_USERNAME); from=$(get SMTP_FROM); pass=$(get SMTP_PASSWORD)
bare=$(echo "$from" | sed -E 's/.*<([^>]+)>.*/\1/')
[ -n "$pass" ] || { read -rsp "SMTP password for $user (hidden): " pass; echo; }
msg=$(mktemp); chmod 600 "$msg"; trap 'rm -f "$msg"' EXIT
cat > "$msg" <<MSG
From: $from
To: $rcpt
Subject: Keygate SMTP test $(date -u +%FT%TZ)
Date: $(date -R)
Message-ID: <keygate-test-$(date +%s)@$(hostname -f 2>/dev/null || hostname)>
Content-Type: text/plain; charset=utf-8

This is a test message from the Keygate server. If you read it, authenticated SMTP submission works.
MSG
if [ "$port" = 465 ]; then url="smtps://$host:465"; else url="smtp://$host:$port"; ssl="--ssl-reqd"; fi
esc=${pass//\\/\\\\}; esc=${esc//\"/\\\"}
if curl -sS --fail-with-body ${ssl:-} --url "$url" --mail-from "$bare" --mail-rcpt "$rcpt" --upload-file "$msg" -K - <<CFG
user = "$user:$esc"
CFG
then echo "SUBMITTED via $host:$port as $user. Now check the inbox (and spam) of $rcpt: only a received message proves delivery."
else echo "submission FAILED (see the curl error above; no secret was printed)" >&2; exit 1; fi
unset pass esc
