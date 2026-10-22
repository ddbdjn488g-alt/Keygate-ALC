#!/bin/bash
# Forced command for the dedicated backup-sync SSH key.
# Streams only timestamped encrypted archive files and their SHA-256 sidecars.
set -euo pipefail

EXPORT_DIR=/home/debian/keygate-export
[ -d "$EXPORT_DIR" ] || { echo "encrypted backup export is not ready" >&2; exit 1; }

shopt -s nullglob
candidates=( "$EXPORT_DIR"/keygate-*.tar.enc "$EXPORT_DIR"/keygate-*.tar.enc.sha256 )
files=()
for path in "${candidates[@]}"; do
  [ -f "$path" ] && [ ! -L "$path" ] || continue
  name=${path##*/}
  [[ "$name" =~ ^keygate-[0-9]{8}T[0-9]{6}Z\.tar\.enc(\.sha256)?$ ]] || continue
  files+=( "$name" )
done

(("${#files[@]}" > 0)) || { echo "no encrypted backups available" >&2; exit 1; }
cd "$EXPORT_DIR"
exec /usr/bin/tar --format=ustar --no-recursion -cf - -- "${files[@]}"
