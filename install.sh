#!/usr/bin/env bash
#
# Star Fleet installer
# Usage:  curl -fsSL https://raw.githubusercontent.com/nullne/star-fleet/main/install.sh | bash
#         curl -fsSL ... | bash -s -- --dir /custom/path
#
set -euo pipefail

REPO="nullne/star-fleet"
BINARY="fleet"
INSTALL_DIR="/usr/local/bin"

# ── Helpers ──────────────────────────────────────────────────────────────────

info()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok()    { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
err()   { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || err "$1 is required but not found"
}

# ── Args ─────────────────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir)  INSTALL_DIR="$2"; shift 2 ;;
    --help) printf 'Usage: install.sh [--dir PATH]\n'; exit 0 ;;
    *)      err "unknown option: $1" ;;
  esac
done

# ── Detect platform ─────────────────────────────────────────────────────────

need curl
need tar

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  linux)  ;;
  darwin) ;;
  *)      err "unsupported OS: $OS" ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)             err "unsupported architecture: $ARCH" ;;
esac

info "Detected platform: ${OS}/${ARCH}"

# ── Fetch latest release tag ────────────────────────────────────────────────

API_URL="https://api.github.com/repos/${REPO}/releases/latest"
info "Fetching latest release..."

TAG="$(curl -fsSL "$API_URL" | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')"

if [[ -z "$TAG" ]]; then
  err "could not determine latest release from ${API_URL}"
fi

info "Latest release: ${TAG}"

# ── Download & extract ──────────────────────────────────────────────────────

ASSET="${BINARY}_${TAG#v}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

info "Downloading ${DOWNLOAD_URL}..."
curl -fsSL "$DOWNLOAD_URL" -o "${TMPDIR}/${ASSET}"

info "Extracting..."
tar -xzf "${TMPDIR}/${ASSET}" -C "$TMPDIR"

# ── Install ─────────────────────────────────────────────────────────────────

if [[ ! -d "$INSTALL_DIR" ]]; then
  mkdir -p "$INSTALL_DIR" 2>/dev/null || sudo mkdir -p "$INSTALL_DIR"
fi

if [[ -w "$INSTALL_DIR" ]]; then
  mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  info "Needs sudo to write to ${INSTALL_DIR}"
  sudo mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

# ── Verify ──────────────────────────────────────────────────────────────────

if command -v "$BINARY" >/dev/null 2>&1; then
  VERSION="$("$BINARY" --version 2>/dev/null || echo "$TAG")"
  ok "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}"
else
  ok "Installed to ${INSTALL_DIR}/${BINARY}"
  printf '    Add %s to your PATH if needed:\n' "$INSTALL_DIR"
  printf '    export PATH="%s:$PATH"\n' "$INSTALL_DIR"
fi
