#!/bin/bash
# Network diagnosis of the outgoing SMTP path from this server. NO credentials are used or needed.
# For each candidate: DNS, TCP connect, TLS (STARTTLS on 587 / implicit on 465), certificate check, AUTH mechanisms offered.
set -u
candidates=${*:-"smtp.mail.ovh.net:587 smtp.mail.ovh.net:465 ssl0.ovh.net:587 ssl0.ovh.net:465"}
for hp in $candidates; do
  host=${hp%:*}; port=${hp#*:}
  echo "--- $host:$port"
  ip=$(getent hosts "$host" | awk '{print $1; exit}'); echo "dns      : ${ip:-FAILED}"
  [ -n "$ip" ] || continue
  if timeout 8 bash -c "exec 3<>/dev/tcp/$host/$port" 2>/dev/null; then echo "tcp      : open"; else echo "tcp      : BLOCKED or closed (outbound $port)"; continue; fi
  if [ "$port" = 465 ]; then mode=""; else mode="-starttls smtp"; fi
  out=$(printf 'EHLO check.local\r\nQUIT\r\n' | timeout 15 openssl s_client $mode -connect "$host:$port" -servername "$host" -verify_return_error -crlf -quiet 2>&1 | tr -d '\r')
  echo "tls      : $(echo "$out" | grep -qiE 'verify error|handshake failure|wrong version' && echo FAILED || echo ok)"
  echo "$out" | grep -E '^(220|250[- ](AUTH|STARTTLS|SIZE))' | sed 's/^/server   : /' | head -6
done
