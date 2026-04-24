#!/usr/bin/env sh
set -e

REPO="${PRE_REPO:-yowainwright/pre}"
BIN_DIR="${PRE_BIN_DIR:-/usr/local/bin}"
VERSION="${PRE_VERSION:-latest}"

validate_os() {
  os="${1:-$(uname -s)}"
  case "$os" in
    Darwin) return 0 ;;
    Linux)  return 0 ;;
    *)
      echo "pre: unsupported OS: $os" >&2
      return 1
      ;;
  esac
}

detect_arch() {
  machine="${1:-$(uname -m)}"
  os="${2:-$(uname -s | tr '[:upper:]' '[:lower:]')}"
  case "$machine" in
    arm64|aarch64) echo "${os}-arm64" ;;
    x86_64)        echo "${os}-amd64" ;;
    *)
      echo "pre: unsupported architecture: $machine" >&2
      return 1
      ;;
  esac
}

fetch_latest_version() {
  repo="${1:-$REPO}"
  curl -fsSL "https://api.github.com/repos/${repo}/releases/latest" \
    | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/'
}

resolve_version() {
  version="${1:-$VERSION}"
  repo="${2:-$REPO}"
  if [ "$version" = "latest" ]; then
    fetch_latest_version "$repo"
  else
    echo "$version"
  fi
}

build_url() {
  repo="${1:-$REPO}"
  version="$2"
  target="$3"
  echo "https://github.com/${repo}/releases/download/v${version}/pre-${target}"
}

download_file() {
  url="$1"
  dest="$2"
  curl -fsSL "$url" -o "$dest"
}

checksum_for_asset() {
  asset="$1"
  checksums_file="$2"
  awk -v asset="$asset" '$2 == asset { print $1; found=1; exit } END { if (!found) exit 1 }' "$checksums_file"
}

compute_checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

verify_checksum() {
  binary="$1"
  expected="$2"
  actual="$(compute_checksum "$binary")"
  if [ "$expected" != "$actual" ]; then
    echo "pre: checksum mismatch" >&2
    echo "pre:   expected: ${expected}" >&2
    echo "pre:   actual:   ${actual}" >&2
    return 1
  fi
}

verify_cosign() {
  bundle="$1"
  file="$2"
  if command -v cosign >/dev/null 2>&1; then
    cosign verify-blob \
      --bundle "$bundle" \
      --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
      --certificate-identity-regexp "https://github.com/yowainwright/pre/" \
      "$file" 2>/dev/null && \
      echo "pre: cosign signature verified" || \
      echo "pre: cosign verification failed (continuing)" >&2
  else
    echo "pre: cosign not found, skipping signature verification"
  fi
}

ensure_dir() {
  dir="${1:-$BIN_DIR}"
  [ -d "$dir" ] || mkdir -p "$dir"
}

place_binary() {
  src="$1"
  dest="$2"
  mv "$src" "$dest"
}

make_executable() {
  file="$1"
  chmod +x "$file"
}

install_binary() {
  src="$1"
  dest_dir="${2:-$BIN_DIR}"
  ensure_dir "$dest_dir"
  place_binary "$src" "${dest_dir}/pre"
  make_executable "${dest_dir}/pre"
}

main() {
  validate_os
  target="$(detect_arch)"
  version="$(resolve_version)"
  artifact="pre-${target}"
  bin_url="$(build_url "$REPO" "$version" "$target")"
  checksums_url="https://github.com/${REPO}/releases/download/v${version}/checksums.txt"
  bundle_url="${checksums_url}.bundle"
  asset_name="${bin_url##*/}"

  echo "pre: installing v${version} (${target}) to ${BIN_DIR}/pre"

  tmp_bin="$(mktemp)"
  tmp_checksums="$(mktemp)"
  tmp_bundle="$(mktemp)"
  trap 'rm -f "$tmp_bin" "$tmp_checksums" "$tmp_bundle"' EXIT

  download_file "$bin_url" "$tmp_bin"
  download_file "$checksums_url" "$tmp_checksums"

  expected_checksum="$(checksum_for_asset "$asset_name" "$tmp_checksums")" || {
    echo "pre: missing checksum for ${asset_name}" >&2
    return 1
  }
  verify_checksum "$tmp_bin" "$expected_checksum"

  download_file "$bundle_url" "$tmp_bundle" 2>/dev/null || true

  if [ -s "$tmp_bundle" ] && [ -s "$tmp_checksums" ]; then
    verify_cosign "$tmp_bundle" "$tmp_checksums"
  fi

  install_binary "$tmp_bin"

  echo "pre: installed (checksum verified)"
  echo "pre: run 'pre setup' to install shell hooks"
}

if [ "${_PRE_INSTALL_SOURCED:-0}" != "1" ]; then
  main "$@"
fi
