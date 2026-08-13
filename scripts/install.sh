#!/usr/bin/env bash
set -euo pipefail

# Installer script
# Usage: curl -sSL https://raw.githubusercontent.com/somaz94/kube-ctx/main/scripts/install.sh | bash

REPO="somaz94/kube-ctx"
BINARY="kctx"
INSTALL_DIR="/usr/local/bin"

RED='\033[31m'
GREEN='\033[32m'
CYAN='\033[36m'
BOLD='\033[1m'
RESET='\033[0m'

info()  { echo -e "${CYAN}▶ $*${RESET}"; }
ok()    { echo -e "${GREEN}✓ $*${RESET}"; }
fail()  { echo -e "${RED}✗ $*${RESET}"; exit 1; }

detect_platform() {
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"

  case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)      fail "Unsupported OS: $OS" ;;
  esac

  case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)             fail "Unsupported architecture: $ARCH" ;;
  esac
}

get_latest_version() {
  VERSION=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
  if [ -z "$VERSION" ]; then
    fail "Could not determine latest version"
  fi
}

main() {
  echo -e "${BOLD}${BINARY} installer${RESET}"
  echo ""

  detect_platform
  info "Detected platform: ${OS}/${ARCH}"

  get_latest_version
  info "Latest version: v${VERSION}"

  ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
  DOWNLOAD_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ARCHIVE}"

  TMPDIR=$(mktemp -d)
  trap 'rm -rf "$TMPDIR"' EXIT

  info "Downloading ${ARCHIVE}..."
  curl -sSL "$DOWNLOAD_URL" -o "${TMPDIR}/${ARCHIVE}" || fail "Download failed: ${DOWNLOAD_URL}"

  info "Extracting..."
  tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"

  info "Installing to ${INSTALL_DIR}/${BINARY}..."
  if [ -w "$INSTALL_DIR" ]; then
    mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  else
    sudo mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  fi
  chmod +x "${INSTALL_DIR}/${BINARY}"

  ok "${BINARY} v${VERSION} installed successfully!"
  echo ""
  echo -e "  Run '${CYAN}${BINARY} --help${RESET}' to get started."
}

main
