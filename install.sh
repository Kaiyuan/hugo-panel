#!/bin/sh
# Hugo Panel installer for Linux and macOS.
#
# Usage:
#   curl -fsSL https://github.com/Kaiyuan/hugo-panel/releases/latest/download/install.sh | sh
#
# Optional environment variables:
#   HP_VERSION       Release tag to install, for example v0.0.7. Default: latest.
#   HP_INSTALL_DIR   Directory for the hugo-panel binary. Default: ~/.local/bin,
#                    or /usr/local/bin when run as root.
#   HP_REPO          GitHub repository, default: Kaiyuan/hugo-panel.
set -eu

REPO="${HP_REPO:-Kaiyuan/hugo-panel}"
REPO_URL="https://github.com/${REPO}"
VERSION="${HP_VERSION:-latest}"

say() {
  printf '%s\n' "$*"
}

fail() {
  printf 'hugo-panel installer: %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

TMP=""
cleanup() {
  if [ -n "$TMP" ]; then
    rm -rf "$TMP"
  fi
}
trap cleanup EXIT
trap 'exit 1' INT TERM

case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) fail "unsupported operating system: $(uname -s). Linux and macOS are supported." ;;
esac

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) fail "unsupported architecture: $(uname -m). amd64 and arm64 are supported." ;;
esac

need curl
need tar

if [ -n "${HP_INSTALL_DIR:-}" ]; then
  INSTALL_DIR="$HP_INSTALL_DIR"
elif [ "$(id -u)" -eq 0 ]; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="${HOME}/.local/bin"
fi

case "$VERSION" in
  ""|latest|LATEST)
    BASE="${REPO_URL}/releases/latest/download"
    VERSION_LABEL="latest"
    ;;
  *)
    TAG="$VERSION"
    case "$TAG" in
      v*|V*) ;;
      *) TAG="v${TAG}" ;;
    esac
    BASE="${REPO_URL}/releases/download/${TAG}"
    VERSION_LABEL="$TAG"
    ;;
esac

ASSET="hugo-panel_${OS}_${ARCH}.tar.gz"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/hugo-panel.XXXXXX")" || fail "could not create a temporary directory"

say "Downloading Hugo Panel ${VERSION_LABEL} for ${OS}/${ARCH}..."
curl --proto '=https' --tlsv1.2 -fL --retry 3 --connect-timeout 15 \
  -o "${TMP}/${ASSET}" "${BASE}/${ASSET}" \
  || fail "download failed: ${BASE}/${ASSET}"
curl --proto '=https' --tlsv1.2 -fL --retry 3 --connect-timeout 15 \
  -o "${TMP}/checksums.txt" "${BASE}/checksums.txt" \
  || fail "download failed: ${BASE}/checksums.txt"

EXPECTED="$(awk -v file="${ASSET}" '$2 == file { print $1; exit }' "${TMP}/checksums.txt")"
[ -n "$EXPECTED" ] || fail "checksum for ${ASSET} is missing"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMP}/${ASSET}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${TMP}/${ASSET}" | awk '{ print $1 }')"
else
  fail "sha256sum or shasum is required to verify the download"
fi
[ "$ACTUAL" = "$EXPECTED" ] || fail "checksum mismatch for ${ASSET}"

tar -xzf "${TMP}/${ASSET}" -C "$TMP"
[ -f "${TMP}/hugo-panel" ] || fail "archive did not contain the hugo-panel binary"

mkdir -p "$INSTALL_DIR" || fail "could not create ${INSTALL_DIR}"
if command -v install >/dev/null 2>&1; then
  install -m 0755 "${TMP}/hugo-panel" "${INSTALL_DIR}/hugo-panel"
else
  cp "${TMP}/hugo-panel" "${INSTALL_DIR}/hugo-panel"
  chmod 0755 "${INSTALL_DIR}/hugo-panel"
fi

say ""
say "Installed: ${INSTALL_DIR}/hugo-panel"
if "${INSTALL_DIR}/hugo-panel" --version >/dev/null 2>&1; then
  "${INSTALL_DIR}/hugo-panel" --version
fi

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    say ""
    say "Add the install directory to PATH:"
    say "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac

say ""
say "Next steps:"
say "  1. Install Hugo Extended 0.120 or newer and confirm 'hugo version' works."
say "  2. Run: hugo-panel"
say "  3. Open: http://localhost:55530"
