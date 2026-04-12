#!/usr/bin/env sh
set -e

REPO="${PRE_REPO:-yowainwright/pre}"
BIN_DIR="${PRE_BIN_DIR:-/usr/local/bin}"
VERSION="${PRE_VERSION:-latest}"

validate_os() {
  os="${1:-$(uname -s)}"
  case "$os" in
    Darwin) return 0 ;;
    *)
      echo "pre: unsupported OS: $os" >&2
      return 1
      ;;
  esac
}

detect_arch() {
  machine="${1:-$(uname -m)}"
  case "$machine" in
    arm64|aarch64) echo "darwin-arm64" ;;
    x86_64)        echo "darwin-amd64" ;;
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

compute_checksum() {
  binary="$1"
  shasum -a 256 "$binary" | awk '{print $1}'
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
  bin_url="$(build_url "$REPO" "$version" "$target")"
  sum_url="${bin_url}.sha256"

  echo "pre: installing v${version} (${target}) to ${BIN_DIR}/pre"

  tmp_bin="$(mktemp)"
  tmp_sum="$(mktemp)"
  trap 'rm -f "$tmp_bin" "$tmp_sum"' EXIT

  download_file "$bin_url" "$tmp_bin"
  download_file "$sum_url" "$tmp_sum"

  verify_checksum "$tmp_bin" "$(cat "$tmp_sum")"
  install_binary "$tmp_bin"

  echo "pre: installed (checksum verified)"
  echo "pre: run 'pre setup' to install shell hooks"
}

if [ "${_PRE_INSTALL_SOURCED:-0}" != "1" ]; then
  main "$@"
fi
