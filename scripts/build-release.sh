#!/usr/bin/env sh
set -eu

VERSION="${DIMENG_VERSION:-v0.3.5}"
RELEASE_BASE_URL="${DIMENG_RELEASE_BASE_URL:-}"
SIGNING_KEY="${DIMENG_RELEASE_SIGNING_KEY:-}"
ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"

case "$VERSION" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) printf 'invalid DIMENG_VERSION: %s\n' "$VERSION" >&2; exit 2 ;;
esac
[ -n "$RELEASE_BASE_URL" ] || { printf 'DIMENG_RELEASE_BASE_URL is required\n' >&2; exit 2; }
[ -f "$SIGNING_KEY" ] || { printf 'DIMENG_RELEASE_SIGNING_KEY is required\n' >&2; exit 2; }

mkdir -p "$DIST_DIR"
for arch in amd64 arm64; do
  agent="dimeng-monitor-agent-linux-${arch}"
  updater="dimeng-agent-updater-linux-${arch}"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "${DIST_DIR}/${agent}" ./cmd/dimeng-monitor-agent
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "${DIST_DIR}/${updater}" ./cmd/dimeng-agent-updater
  sha256sum "${DIST_DIR}/${agent}" >"${DIST_DIR}/${agent}.sha256"
  sha256sum "${DIST_DIR}/${updater}" >"${DIST_DIR}/${updater}.sha256"
done

amd64_sum="$(awk '{print $1}' "${DIST_DIR}/dimeng-monitor-agent-linux-amd64.sha256")"
arm64_sum="$(awk '{print $1}' "${DIST_DIR}/dimeng-monitor-agent-linux-arm64.sha256")"
cat >"${DIST_DIR}/release.manifest.json" <<EOF
{"version":"${VERSION}","assets":[{"os":"linux","arch":"amd64","url":"${RELEASE_BASE_URL}/dimeng-monitor-agent-linux-amd64","sha256":"${amd64_sum}"},{"os":"linux","arch":"arm64","url":"${RELEASE_BASE_URL}/dimeng-monitor-agent-linux-arm64","sha256":"${arm64_sum}"}]}
EOF

openssl pkeyutl -sign -rawin -inkey "$SIGNING_KEY" -in "${DIST_DIR}/release.manifest.json" -out "${DIST_DIR}/release.manifest.sig.bin"
base64 <"${DIST_DIR}/release.manifest.sig.bin" | tr -d '\n' >"${DIST_DIR}/release.manifest.sig"
printf '\n' >>"${DIST_DIR}/release.manifest.sig"
rm -f "${DIST_DIR}/release.manifest.sig.bin"
printf 'release built in %s\n' "$DIST_DIR"
