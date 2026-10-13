#!/bin/bash
# Restarts Keygate after 3 consecutive failed health checks (a hung process that systemd cannot see).
set -u
STATE=/run/keygate-health.fails
if curl -fsS -m 5 http://127.0.0.1:9000/health >/dev/null 2>&1; then
  rm -f "$STATE"; exit 0
fi
n=$(( $(cat "$STATE" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$STATE"
logger -t keygate-health "health check failed ($n/3)"
if [ "$n" -ge 3 ]; then
  rm -f "$STATE"
  logger -t keygate-health "restarting keygate"
  systemctl restart keygate
fi
