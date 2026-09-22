#!/usr/bin/env sh

# shellcheck source-path=SCRIPTDIR
_PRE_INSTALL_SOURCED=1
# shellcheck source=../../install.sh
. "$(dirname "$0")/../../install.sh"
set +e

passed=0
failed=0

check() {
  label="${1:?}"
  expected="${2-}"
  actual="${3-}"
  case "$actual" in
  "$expected")
    printf "ok  %s\n" "$label"
    passed=$((passed + 1))
    ;;
  *)
    printf "FAIL %s\n  want: %s\n  got:  %s\n" "$label" "$expected" "$actual"
    failed=$((failed + 1))
    ;;
  esac
}

exit_code() {
  "$@" 2>/dev/null
  echo $?
}

exit_code_quiet() {
  "$@" >/dev/null 2>&1
  echo $?
}

test_validate_os() {
  check "validate_os accepts Darwin" "0" "$(validate_os_status "Darwin")"
  check "validate_os accepts Linux" "0" "$(validate_os_status "Linux")"
  check "validate_os rejects unknown" "1" "$(validate_os_status "Windows_NT")"
}

test_detect_arch() {
  check "detect_arch darwin arm64" "darwin-arm64" "$(detect_arch "arm64" "darwin")"
  check "detect_arch darwin aarch64" "darwin-arm64" "$(detect_arch "aarch64" "darwin")"
  check "detect_arch darwin x86_64" "darwin-amd64" "$(detect_arch "x86_64" "darwin")"
  check "detect_arch linux arm64" "linux-arm64" "$(detect_arch "arm64" "linux")"
  check "detect_arch linux x86_64" "linux-amd64" "$(detect_arch "x86_64" "linux")"
  check "detect_arch rejects unknown" "1" "$(detect_arch_status "riscv64")"
}

test_resolve_version() {
  check "resolve_version pinned" "1.2.3" "$(resolve_version "1.2.3" "any/repo")"
  check "resolve_version custom repo" "0.9.0" "$(resolve_version "0.9.0" "other/repo")"
  check "fetch_latest_version propagates failure" "1" "$(
    (
      curl() { return 1; }
      fetch_latest_version "repo" >/dev/null 2>&1
      echo $?
    )
  )"
}

test_fetch_version_response() {
  check "fetch_latest_version rejects missing tag" "1" "$(
    (
      curl() { printf '{"message":"rate limited"}'; }
      fetch_latest_version "repo" >/dev/null 2>&1
      echo $?
    )
  )"
  check "fetch_latest_version parses tag" "1.2.3" "$(
    (
      curl() { printf '{"tag_name":"v1.2.3"}'; }
      fetch_latest_version "repo"
    )
  )"
}

test_build_url() {
  check "build_url" \
    "https://github.com/yowainwright/pre/releases/download/v1.0.0/pre-darwin-arm64" \
    "$(build_url "yowainwright/pre" "1.0.0" "darwin-arm64")"
}

test_checksum_for_asset() {
  tmp_checksums="$(mktemp)"
  cat >"$tmp_checksums" <<'EOF'
abc123  pre-darwin-arm64
def456  pre-linux-amd64
EOF
  check "checksum_for_asset finds checksum" "abc123" "$(checksum_for_asset "pre-darwin-arm64" "$tmp_checksums")"
  check "checksum_for_asset missing asset" "1" "$(checksum_for_asset_status "pre-linux-arm64" "$tmp_checksums")"
  rm -f "$tmp_checksums"
}

test_compute_checksum() {
  tmp="$(mktemp)"
  printf "hello" >"$tmp"
  expected_sum="$(shasum -a 256 "$tmp" | awk '{print $1}')"
  check "compute_checksum" "$expected_sum" "$(compute_checksum "$tmp")"
  rm -f "$tmp"
}

test_verify_checksum() {
  tmp="$(mktemp)"
  printf "hello" >"$tmp"
  good_sum="$(shasum -a 256 "$tmp" | awk '{print $1}')"
  bad_sum="$(printf '%064d' 0)"
  check "verify_checksum passes" "0" "$(verify_checksum_status "$tmp" "$good_sum")"
  check "verify_checksum fails" "1" "$(verify_checksum_status "$tmp" "$bad_sum")"
  rm -f "$tmp"
}

test_verify_cosign() {
  orig_path="$PATH"
  tmp_dir="$(mktemp -d)"
  cat >"${tmp_dir}/cosign" <<'EOF'
#!/usr/bin/env sh
exit 0
EOF
  chmod +x "${tmp_dir}/cosign"
  PATH="${tmp_dir}:$orig_path"
  check "verify_cosign passes" "0" "$(verify_cosign_status "bundle" "file")"
  cat >"${tmp_dir}/cosign" <<'EOF'
#!/usr/bin/env sh
exit 1
EOF
  chmod +x "${tmp_dir}/cosign"
  check "verify_cosign fails" "1" "$(verify_cosign_status "bundle" "file")"
  PATH="$orig_path"
  rm -rf "$tmp_dir"

}

test_missing_cosign() {
  orig_path="$PATH"
  tmp_dir="$(mktemp -d)"
  PATH="$tmp_dir"
  check "verify_cosign requires cosign" "1" "$(verify_cosign_status "bundle" "file")"
  PATH="$orig_path"
  rm -rf "$tmp_dir"
}

test_download_signature_bundle() {
  tmp_bundle="$(mktemp)"
  check "download_signature_bundle rejects failed download" "1" "$(
    (
      download_file() { return 1; }
      download_signature_bundle "bundle-url" "$tmp_bundle"
    ) >/dev/null 2>&1
    echo $?
  )"
  check "download_signature_bundle rejects empty bundle" "1" "$(
    (
      download_file() { : >"${2-}"; }
      download_signature_bundle "bundle-url" "$tmp_bundle"
    ) >/dev/null 2>&1
    echo $?
  )"
  rm -f "$tmp_bundle"
}

test_valid_signature_bundle() {
  tmp_bundle="$(mktemp)"
  check "download_signature_bundle accepts bundle" "0" "$(
    (
      download_file() { printf "signature" >"${2-}"; }
      download_signature_bundle "bundle-url" "$tmp_bundle"
    ) >/dev/null 2>&1
    echo $?
  )"
  rm -f "$tmp_bundle"
}

test_ensure_dir() {
  tmp_dir="$(mktemp -d)"
  rm -rf "$tmp_dir"
  ensure_dir "$tmp_dir"
  check "ensure_dir creates dir" "0" "$(test_status -d "$tmp_dir")"
  ensure_dir "$tmp_dir"
  check "ensure_dir is idempotent" "0" "$(test_status -d "$tmp_dir")"
  rm -rf "$tmp_dir"
}

test_place_binary() {
  src="$(mktemp)"
  dest_dir="$(mktemp -d)"
  place_binary "$src" "${dest_dir}/pre"
  check "place_binary moves file" "0" "$(test_status -f "${dest_dir}/pre")"
  check "place_binary removes source" "1" "$(test_status -f "$src")"
  rm -rf "$dest_dir"
}

test_make_executable() {
  tmp="$(mktemp)"
  chmod -x "$tmp"
  make_executable "$tmp"
  check "make_executable sets +x" "0" "$(test_status -x "$tmp")"
  rm -f "$tmp"
}

test_install_binary() {
  src="$(mktemp)"
  dest_dir="$(mktemp -d)"
  rm -rf "$dest_dir"
  install_binary "$src" "$dest_dir"
  check "install_binary creates dest dir" "0" "$(test_status -d "$dest_dir")"
  check "install_binary places binary" "0" "$(test_status -f "${dest_dir}/pre")"
  check "install_binary sets executable" "0" "$(test_status -x "${dest_dir}/pre")"
  rm -rf "$dest_dir"
}

validate_os_status() {
  (validate_os "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

detect_arch_status() {
  (detect_arch "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

checksum_for_asset_status() {
  (checksum_for_asset "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

verify_checksum_status() {
  (verify_checksum "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

verify_cosign_status() {
  (verify_cosign "$@") >/dev/null 2>&1
  status=$?
  printf '%s\n' "$status"
}

test_status() {
  test "$@" 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

run_tests() {
  test_validate_os
  test_detect_arch
  test_resolve_version
  test_fetch_version_response
  test_build_url
  test_checksum_for_asset
  test_compute_checksum
  test_verify_checksum
  test_verify_cosign
  test_missing_cosign
  test_download_signature_bundle
  test_valid_signature_bundle
  test_ensure_dir
  test_place_binary
  test_make_executable
  test_install_binary
  printf "\n%d passed, %d failed\n" "$passed" "$failed"
  [ "$failed" -eq 0 ]
}

run_tests
