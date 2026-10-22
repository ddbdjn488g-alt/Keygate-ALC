#!/bin/bash
# Encrypted, verified backup of the Keygate database + its environment file (the secrets without
# which the database cannot be used). Run by keygate-backup.timer; safe to run by hand as root.
# Output: /var/backups/keygate/keygate-<UTC timestamp>.tar.enc  (AES-256, key file /etc/keygate/backup.pass)
set -euo pipefail
umask 077

BACKUP_DIR=${BACKUP_DIR:-/var/backups/keygate}
PASS_FILE=${PASS_FILE:-/etc/keygate/backup.pass}
ENV_FILE=${ENV_FILE:-/etc/keygate/keygate.env}
DB_NAME=${DB_NAME:-keygate}
STATE_DIR=/var/lib/keygate-backup
KEEP_DAILY=${KEEP_DAILY:-14}
KEEP_WEEKLY_DAYS=${KEEP_WEEKLY_DAYS:-56}

[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
[ -r "$PASS_FILE" ] || { echo "missing $PASS_FILE (run bootstrap.sh)" >&2; exit 1; }
mkdir -p "$BACKUP_DIR" "$STATE_DIR"; chmod 700 "$BACKUP_DIR" "$STATE_DIR"

ts=$(date -u +%Y%m%dT%H%M%SZ)
work=$(mktemp -d /var/tmp/keygate-backup.XXXXXX)
trap 'rm -rf "$work"' EXIT

# 1. Consistent logical dump (custom format, compressed).
runuser -u postgres -- pg_dump --format=custom --no-owner "$DB_NAME" > "$work/db.dump"
[ -s "$work/db.dump" ] || { echo "empty dump" >&2; exit 1; }
cp "$ENV_FILE" "$work/keygate.env"

# 2. Encrypt (nothing readable is ever left in $BACKUP_DIR).
out="$BACKUP_DIR/keygate-$ts.tar.enc"
tar -C "$work" -cf - db.dump keygate.env \
  | openssl enc -aes-256-cbc -pbkdf2 -iter 600000 -salt -pass "file:$PASS_FILE" -out "$out.partial"

# 3. Verify: decrypts, contains both files, and the dump's table of contents is readable.
mkdir "$work/check"
openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 -pass "file:$PASS_FILE" -in "$out.partial" | tar -C "$work/check" -xf -
[ -s "$work/check/db.dump" ] && [ -s "$work/check/keygate.env" ] || { echo "verification failed: archive incomplete" >&2; rm -f "$out.partial"; exit 1; }
pg_restore --list "$work/check/db.dump" >/dev/null || { echo "verification failed: dump unreadable" >&2; rm -f "$out.partial"; exit 1; }
mv "$out.partial" "$out"
sha256sum "$out" | awk '{print $1}' > "$out.sha256"

# 4. Retention: newest $KEEP_DAILY, plus Sunday backups younger than $KEEP_WEEKLY_DAYS days.
mapfile -t files < <(ls -1 "$BACKUP_DIR"/keygate-*.tar.enc 2>/dev/null | sort -r)
i=0
for f in "${files[@]}"; do
  i=$((i+1))
  [ "$i" -le "$KEEP_DAILY" ] && continue
  stamp=$(basename "$f" | sed -E 's/keygate-([0-9]{8})T.*/\1/')
  dow=$(date -u -d "$stamp" +%u 2>/dev/null || echo 0)
  age=$(( ( $(date -u +%s) - $(date -u -d "$stamp" +%s 2>/dev/null || echo 0) ) / 86400 ))
  if [ "$dow" = 7 ] && [ "$age" -le "$KEEP_WEEKLY_DAYS" ]; then continue; fi
  rm -f "$f" "$f.sha256"
done

# 5. Stage retained encrypted archives for the Mac's restricted offsite sync.
# This directory contains encrypted archives and checksums only, never the passphrase.
EXPORT_DIR=${EXPORT_DIR:-/home/debian/keygate-export}
id debian >/dev/null 2>&1 || { echo "missing debian account for encrypted export" >&2; exit 1; }
install -d -o debian -g debian -m 0700 "$EXPORT_DIR"
find "$EXPORT_DIR" -maxdepth 1 -type f \
  \( -name 'keygate-*.tar.enc' -o -name 'keygate-*.tar.enc.sha256' \) -delete
while IFS= read -r f; do
  install -o debian -g debian -m 0600 "$f" "$EXPORT_DIR/$(basename "$f")"
  [ ! -f "$f.sha256" ] || install -o debian -g debian -m 0600 "$f.sha256" "$EXPORT_DIR/$(basename "$f").sha256"
done < <(find "$BACKUP_DIR" -maxdepth 1 -type f -name 'keygate-*.tar.enc' -print | sort)

date -u +%FT%TZ > "$STATE_DIR/last_success"
size=$(du -h "$out" | cut -f1)
logger -t keygate-backup "backup OK: $(basename "$out") ($size); encrypted export staging refreshed"
echo "backup OK: $out ($size)"
echo "encrypted export staging refreshed: $EXPORT_DIR"
