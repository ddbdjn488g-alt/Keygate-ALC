#!/bin/bash
# Ship the deploy tooling and a release to the VPS and install it.
#   deploy/ovh/deploy.sh --bootstrap     first time: preparation of the server, then installs the release
#   deploy/ovh/deploy.sh                 update: backup, atomic switch, health check, automatic rollback
#   deploy/ovh/deploy.sh --build-only    just build the tarball
# Environment: VPS_HOST (default 57.131.165.209), VPS_USER (debian), SSH_KEY (~/.ssh/alc_ovh_ed25519), TARBALL (skip the build).
set -euo pipefail
cd "$(dirname "$0")/../.."
HOST=${VPS_HOST:-57.131.165.209}; USER_=${VPS_USER:-debian}; KEY=${SSH_KEY:-$HOME/.ssh/alc_ovh_ed25519}
SSHO=(-i "$KEY" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15)
mode=${1:-update}

tarball=${TARBALL:-}
if [ -z "$tarball" ]; then
  tarball=$(deploy/ovh/build-release.sh | awk '/^release:/ {print $2}')
fi
[ "$mode" = --build-only ] && { echo "built: $tarball"; exit 0; }

ssh "${SSHO[@]}" "$USER_@$HOST" 'echo connected as $(whoami); sudo -n true && echo sudo ok' 
rdir=/tmp/keygate-deploy
ssh "${SSHO[@]}" "$USER_@$HOST" "rm -rf $rdir && mkdir -p $rdir"
scp "${SSHO[@]}" -q -r deploy/ovh "$USER_@$HOST:$rdir/deploy"
scp "${SSHO[@]}" -q "$tarball" "$tarball.sha256" "$USER_@$HOST:$rdir/"
base=$(basename "$tarball")
if [ "$mode" = --bootstrap ]; then
  ssh "${SSHO[@]}" -t "$USER_@$HOST" "sudo bash $rdir/deploy/bootstrap.sh"
fi
ssh "${SSHO[@]}" -t "$USER_@$HOST" "sudo /opt/keygate/deploy/install-release.sh $rdir/$base && sudo systemctl enable --now keygate-health.timer >/dev/null && rm -rf $rdir"
