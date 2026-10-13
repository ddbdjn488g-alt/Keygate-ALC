#!/bin/bash
# Install a release tarball built by build-release.sh: atomic switch of /opt/keygate/current, health check,
# automatic rollback to the previous release if the new one does not become healthy.
#   install-release.sh keygate-<id>.tar.gz [--no-backup]
# Database migrations run when the new binary starts and only go forward: before any update a backup is taken
# (unless --no-backup); if a migration must be undone, restore that backup (restore.sh).
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
tarball=${1:?usage: $0 keygate-<id>.tar.gz [--no-backup]}
[ -f "$tarball" ] || { echo "no such file: $tarball" >&2; exit 1; }
backup=yes; [ "${2:-}" = --no-backup ] && backup=no

if [ -f "$tarball.sha256" ]; then
  ( cd "$(dirname "$tarball")" && sha256sum -c "$(basename "$tarball").sha256" ) || { echo "checksum mismatch" >&2; exit 1; }
else
  echo "warning: no $tarball.sha256 next to the tarball (integrity not verified)" >&2
fi

id=$(basename "$tarball" .tar.gz)
REL=/opt/keygate/releases/$id
CUR=/opt/keygate/current
[ -e "$REL" ] && { echo "release $id already installed (remove /opt/keygate/releases/$id to reinstall)" >&2; exit 1; }

mkdir -p "$REL"
tar -C "$REL" -xzf "$tarball"
[ -x "$REL/keygate" ] && [ -d "$REL/db/migrations" ] && [ -d "$REL/web/dist" ] || { echo "incomplete release" >&2; rm -rf "$REL"; exit 1; }
chown -R root:root "$REL"; chmod -R go-w "$REL"

if [ "$backup" = yes ] && [ -x /opt/keygate/deploy/backup.sh ] && systemctl is-active --quiet postgresql; then
  /opt/keygate/deploy/backup.sh
fi

prev=""; [ -L "$CUR" ] && prev=$(readlink -f "$CUR")
ln -sfn "$REL" "$CUR.new" && mv -T "$CUR.new" "$CUR"
systemctl restart keygate

healthy=no
for _ in $(seq 1 45); do
  if curl -fsS -m 3 http://127.0.0.1:9000/health >/dev/null 2>&1; then healthy=yes; break; fi
  sleep 1
done
if [ "$healthy" != yes ]; then
  echo "new release is NOT healthy; last log lines:" >&2
  journalctl -u keygate -n 15 --no-pager >&2 || true
  if [ -n "$prev" ] && [ -d "$prev" ]; then
    echo "rolling back to $prev" >&2
    ln -sfn "$prev" "$CUR.new" && mv -T "$CUR.new" "$CUR"
    systemctl restart keygate
  else
    systemctl stop keygate || true
  fi
  exit 1
fi
echo "release $id is live:"; curl -fsS -m 5 http://127.0.0.1:9000/api/v1/version; echo

# Keep the 5 most recent releases.
ls -1dt /opt/keygate/releases/*/ 2>/dev/null | tail -n +6 | xargs -r rm -rf
