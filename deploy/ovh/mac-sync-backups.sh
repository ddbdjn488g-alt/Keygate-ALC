#!/bin/bash
# Pull only encrypted Keygate backup archives through a forced-command SSH key,
# verify every SHA-256 sidecar, sync to iCloud Drive, then mirror server retention.
set -euo pipefail
umask 077

HOST="debian@vps-3b448d48.vps.ovh.net"
KEY="$HOME/.ssh/keygate_backup_sync_ed25519"
DEST="$HOME/Library/Mobile Documents/com~apple~CloudDocs/Keygate-Backups"

[ -r "$KEY" ] || { echo "backup-sync SSH key is missing"; exit 1; }
mkdir -p "$DEST"
TMP=$(mktemp -d "${TMPDIR:-/tmp}/keygate-backup-sync.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
mkdir "$TMP/files"

# The server-side authorized_keys entry forces a read-only export command.
# No shell command or caller-supplied path is executed on the VPS.
ssh -i "$KEY" -o IdentitiesOnly=yes -o BatchMode=yes \
  -o StrictHostKeyChecking=yes -o ConnectTimeout=15 "$HOST" > "$TMP/export.tar"

/usr/bin/tar -tf "$TMP/export.tar" > "$TMP/names.txt"
[ -s "$TMP/names.txt" ] || { echo "empty export bundle"; exit 1; }
while IFS= read -r name; do
  [[ "$name" =~ ^keygate-[0-9]{8}T[0-9]{6}Z\.tar\.enc(\.sha256)?$ ]] || {
    echo "unexpected name in export bundle; refusing extraction"; exit 1;
  }
done < "$TMP/names.txt"

/usr/bin/tar -xf "$TMP/export.tar" -C "$TMP/files"
shopt -s nullglob
archives=( "$TMP/files"/keygate-*.tar.enc )
(("${#archives[@]}" > 0)) || { echo "no encrypted archives in export"; exit 1; }

# Validate every archive before touching the iCloud destination.
for archive in "${archives[@]}"; do
  sidecar="$archive.sha256"
  [ -s "$sidecar" ] || { echo "missing SHA-256 sidecar"; exit 1; }
  expected=$(tr -d '[:space:]' < "$sidecar")
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || { echo "malformed SHA-256 sidecar"; exit 1; }
  actual=$(/usr/bin/shasum -a 256 "$archive" | /usr/bin/awk '{print $1}')
  [ "$expected" = "$actual" ] || { echo "archive SHA-256 mismatch"; exit 1; }
done

# Copy verified files; keep the exported archive encrypted at rest.
for archive in "${archives[@]}"; do
  name=${archive##*/}
  /bin/cp -p "$archive" "$DEST/$name"
  /bin/cp -p "$archive.sha256" "$DEST/$name.sha256"
  expected=$(tr -d '[:space:]' < "$DEST/$name.sha256")
  actual=$(/usr/bin/shasum -a 256 "$DEST/$name" | /usr/bin/awk '{print $1}')
  [ "$expected" = "$actual" ] || { echo "destination SHA-256 mismatch"; exit 1; }
done

# Mirror the server's retained set only after the complete incoming set validates.
for existing in "$DEST"/keygate-*.tar.enc; do
  [ -f "$existing" ] || continue
  name=${existing##*/}
  if [ ! -f "$TMP/files/$name" ]; then
    /bin/rm -f "$existing" "$existing.sha256"
  fi
done

echo "KEYGATE_BACKUP_SYNC_OK archives=${#archives[@]} destination=$DEST"
