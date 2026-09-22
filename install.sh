#!/usr/bin/env sh
set -e

REPO="${PRE_REPO:-yowainwright/pre}"
BIN_DIR="${PRE_BIN_DIR:-/usr/local/bin}"
VERSION="${PRE_VERSION:-latest}"

validate_os() {
  os="${1:-$(uname -s)}"
  case "$os" in
  Darwin) return 0 ;;
  Linux) return 0 ;;
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
  arm64 | aarch64) echo "${os}-arm64" ;;
  x86_64) echo "${os}-amd64" ;;
  *)
    echo "pre: unsupported architecture: $machine" >&2
    return 1
    ;;
  esac
}

fetch_latest_version() {
  repo="${1:-$REPO}"
  response="$(curl -fsSL "https://api.github.com/repos/${repo}/releases/latest")" && fetch_status=0 || fetch_status=$?
  case "$fetch_status" in
  0) parse_latest_version "$response" ;;
  *)
    echo "pre: failed to fetch latest release" >&2
    return 1
    ;;
  esac
}

parse_latest_version() {
  version="$(printf '%s\n' "${1:-}" | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')"
  case "$version" in
  "")
    echo "pre: latest release response has no tag" >&2
    return 1
    ;;
  *) printf '%s\n' "$version" ;;
  esac
}

resolve_version() {
  version="${1:-$VERSION}"
  repo="${2:-$REPO}"
  case "$version" in
  latest) fetch_latest_version "$repo" ;;
  *) echo "$version" ;;
  esac
}

build_url() {
  repo="${1:-$REPO}"
  version="${2:-}"
  target="${3:-}"
  echo "https://github.com/${repo}/releases/download/v${version}/pre-${target}"
}

download_file() {
  url="${1:-}"
  dest="${2:-}"
  curl -fsSL "$url" -o "$dest"
}

checksum_for_asset() {
  asset="${1:-}"
  checksums_file="${2:-}"
  awk -v asset="$asset" '$2 == asset { print $1; found=1; exit } END { if (!found) exit 1 }' "$checksums_file"
}

compute_checksum() {
  checksum_tool=shasum
  command -v sha256sum >/dev/null 2>&1 && checksum_tool=sha256sum
  case "$checksum_tool" in
  sha256sum) sha256sum "$1" | awk '{print $1}' ;;
  *) shasum -a 256 "$1" | awk '{print $1}' ;;
  esac
}

verify_checksum() {
  binary="${1:-}"
  expected="${2:-}"
  actual="$(compute_checksum "$binary")"
  [ "$expected" = "$actual" ] && return 0
  echo "pre: checksum mismatch" >&2
  echo "pre:   expected: ${expected}" >&2
  echo "pre:   actual:   ${actual}" >&2
  return 1
}

verify_cosign() {
  bundle="${1:-}"
  file="${2:-}"
  require_cosign || return "$?"
  run_cosign_verification "$bundle" "$file" && verify_status=0 || verify_status=$?
  case "$verify_status" in
  0) echo "pre: cosign signature verified" ;;
  *)
    echo "pre: cosign verification failed" >&2
    return 1
    ;;
  esac
}

require_cosign() {
  command -v cosign >/dev/null 2>&1 && return 0
  echo "pre: cosign is required for signature verification" >&2
  return 1
}

run_cosign_verification() {
  cosign verify-blob \
    --bundle "$1" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    --certificate-identity-regexp "https://github.com/yowainwright/pre/" \
    "$2" 2>/dev/null
}

download_signature_bundle() {
  url="${1:-}"
  dest="${2:-}"
  download_file "$url" "$dest" && download_status=0 || download_status=$?
  case "$download_status" in
  0) require_signature_bundle "$dest" ;;
  *)
    echo "pre: signature bundle download failed" >&2
    return 1
    ;;
  esac
}

require_signature_bundle() {
  [ -s "$1" ] && return 0
  echo "pre: signature bundle is empty" >&2
  return 1
}

ensure_dir() {
  dir="${1:-$BIN_DIR}"
  [ -d "$dir" ] || mkdir -p "$dir"
}

place_binary() {
  src="${1:-}"
  dest="${2:-}"
  mv "$src" "$dest"
}

make_executable() {
  file="${1:-}"
  chmod +x "$file"
}

install_binary() {
  src="${1:-}"
  dest_dir="${2:-$BIN_DIR}"
  ensure_dir "$dest_dir"
  place_binary "$src" "${dest_dir}/pre"
  make_executable "${dest_dir}/pre"
}

prepare_install() {
  validate_os "$(uname -s)"
  target="$(detect_arch "$(uname -m)" "$(uname -s | tr '[:upper:]' '[:lower:]')")"
  version="$(resolve_version "$VERSION" "$REPO")"
  bin_url="$(build_url "$REPO" "$version" "$target")"
  checksums_url="https://github.com/${REPO}/releases/download/v${version}/checksums.txt"
  bundle_url="${checksums_url}.bundle"
  asset_name="${bin_url##*/}"

  echo "pre: installing v${version} (${target}) to ${BIN_DIR}/pre"

  tmp_bin="$(mktemp)"
  tmp_checksums="$(mktemp)"
  tmp_bundle="$(mktemp)"
  trap 'rm -f "$tmp_bin" "$tmp_checksums" "$tmp_bundle"' EXIT

}

require_asset_checksum() {
  checksum_for_asset "$1" "$2" && return 0
  echo "pre: missing checksum for $1" >&2
  return 1
}

download_verified_binary() {
  download_file "$bin_url" "$tmp_bin"
  download_file "$checksums_url" "$tmp_checksums"

  expected_checksum="$(require_asset_checksum "$asset_name" "$tmp_checksums")" || return "$?"
  verify_checksum "$tmp_bin" "$expected_checksum"

  download_signature_bundle "$bundle_url" "$tmp_bundle"
  verify_cosign "$tmp_bundle" "$tmp_checksums"

}

main() {
  prepare_install
  download_verified_binary
  install_binary "$tmp_bin"

  echo "pre: installed (checksum and signature verified)"
  echo "pre: run 'pre setup' to install shell hooks"
}

run_install() {
  [ "${_PRE_INSTALL_SOURCED:-0}" = "1" ] && return 0
  main "$@"
}

run_install "$@"
