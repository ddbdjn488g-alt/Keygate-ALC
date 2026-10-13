#!/bin/bash
# Build a release tarball on YOUR machine (not on the server): frontend + linux binary + migrations + docs.
#   deploy/ovh/build-release.sh            -> $OUT/keygate-<commit>-<utc>.tar.gz (+ .sha256)
# Needs: go (see go.mod), node/npm. The version, commit and build date are stamped into the binary (/api/v1/version).
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT=${OUT:-/tmp/keygate-release}
GOARCH=${GOARCH:-amd64}
commit=$(git rev-parse --short=12 HEAD)
dirty=""; [ -n "$(git status --porcelain --untracked-files=no)" ] && dirty="-dirty"
branch=$(git rev-parse --abbrev-ref HEAD)
now=$(date -u +%Y%m%dT%H%M%SZ)
id="keygate-$commit$dirty-$now"
mkdir -p "$OUT"; stage=$(mktemp -d); trap 'rm -rf "$stage"' EXIT

echo "== frontend"
( cd web && npm install --no-package-lock --ignore-scripts --no-audit --no-fund >/dev/null && npm run build >/dev/null )
echo "== backend (linux/$GOARCH)"
CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH go build -trimpath \
  -ldflags "-s -w -X github.com/tabloy/keygate/internal/version.Version=$branch -X github.com/tabloy/keygate/internal/version.Commit=$commit$dirty -X github.com/tabloy/keygate/internal/version.BuildDate=$now" \
  -o "$stage/keygate" ./cmd/server
mkdir -p "$stage/web"; cp -R db "$stage/db"; cp -R web/dist "$stage/web/dist"; cp -R docs "$stage/docs"
find "$stage" -name .DS_Store -delete
tar -C "$stage" -czf "$OUT/$id.tar.gz" keygate db web docs
( cd "$OUT" && shasum -a 256 "$id.tar.gz" > "$id.tar.gz.sha256" )
echo "release: $OUT/$id.tar.gz"
echo "commit : $commit$dirty  branch: $branch"
