#!/usr/bin/env sh

_PRE_INSTALL_SOURCED=1
. "$(dirname "$0")/../../install.sh"
set +e

passed=0
failed=0

check() {
  label="$1"
  expected="$2"
  actual="$3"
  if [ "$expected" = "$actual" ]; then
    printf "ok  %s\n" "$label"
    passed=$((passed + 1))
  else
    printf "FAIL %s\n  want: %s\n  got:  %s\n" "$label" "$expected" "$actual"
    failed=$((failed + 1))
  fi
}

exit_code() {
  "$@" 2>/dev/null; echo $?
}

# validate_os
check "validate_os accepts Darwin"  "0" "$(exit_code validate_os "Darwin")"
check "validate_os accepts Linux"   "0" "$(exit_code validate_os "Linux")"
check "validate_os rejects unknown" "1" "$(exit_code validate_os "Windows_NT")"

# detect_arch
check "detect_arch darwin arm64"    "darwin-arm64" "$(detect_arch "arm64" "darwin")"
check "detect_arch darwin aarch64"  "darwin-arm64" "$(detect_arch "aarch64" "darwin")"
check "detect_arch darwin x86_64"   "darwin-amd64" "$(detect_arch "x86_64" "darwin")"
check "detect_arch linux arm64"     "linux-arm64"  "$(detect_arch "arm64" "linux")"
check "detect_arch linux x86_64"    "linux-amd64"  "$(detect_arch "x86_64" "linux")"
check "detect_arch rejects unknown" "1"            "$(exit_code detect_arch "riscv64")"

# resolve_version
check "resolve_version pinned"       "1.2.3" "$(resolve_version "1.2.3" "any/repo")"
check "resolve_version custom repo"  "0.9.0" "$(resolve_version "0.9.0" "other/repo")"

# build_url
check "build_url" \
  "https://github.com/yowainwright/pre/releases/download/v1.0.0/pre-darwin-arm64" \
  "$(build_url "yowainwright/pre" "1.0.0" "darwin-arm64")"

# checksum_for_asset
tmp_checksums="$(mktemp)"
cat > "$tmp_checksums" <<'EOF'
abc123  pre-darwin-arm64
def456  pre-linux-amd64
EOF
check "checksum_for_asset finds checksum" "abc123" "$(checksum_for_asset "pre-darwin-arm64" "$tmp_checksums")"
check "checksum_for_asset missing asset" "1" "$(exit_code checksum_for_asset "pre-linux-arm64" "$tmp_checksums")"
rm -f "$tmp_checksums"

# compute_checksum
tmp="$(mktemp)"
printf "hello" > "$tmp"
expected_sum="$(shasum -a 256 "$tmp" | awk '{print $1}')"
check "compute_checksum" "$expected_sum" "$(compute_checksum "$tmp")"
rm -f "$tmp"

# verify_checksum
tmp="$(mktemp)"
printf "hello" > "$tmp"
good_sum="$(shasum -a 256 "$tmp" | awk '{print $1}')"
bad_sum="0000000000000000000000000000000000000000000000000000000000000000"
check "verify_checksum passes" "0" "$(exit_code verify_checksum "$tmp" "$good_sum")"
check "verify_checksum fails"  "1" "$(exit_code verify_checksum "$tmp" "$bad_sum")"
rm -f "$tmp"

# ensure_dir
tmp_dir="$(mktemp -d)"
rm -rf "$tmp_dir"
ensure_dir "$tmp_dir"
check "ensure_dir creates dir" "0" "$(exit_code test -d "$tmp_dir")"
ensure_dir "$tmp_dir"
check "ensure_dir is idempotent" "0" "$(exit_code test -d "$tmp_dir")"
rm -rf "$tmp_dir"

# place_binary
src="$(mktemp)"
dest_dir="$(mktemp -d)"
place_binary "$src" "${dest_dir}/pre"
check "place_binary moves file"        "0" "$(exit_code test -f "${dest_dir}/pre")"
check "place_binary removes source"    "1" "$(exit_code test -f "$src")"
rm -rf "$dest_dir"

# make_executable
tmp="$(mktemp)"
chmod -x "$tmp"
make_executable "$tmp"
check "make_executable sets +x" "0" "$(exit_code test -x "$tmp")"
rm -f "$tmp"

# install_binary
src="$(mktemp)"
dest_dir="$(mktemp -d)"
rm -rf "$dest_dir"
install_binary "$src" "$dest_dir"
check "install_binary creates dest dir"    "0" "$(exit_code test -d "$dest_dir")"
check "install_binary places binary"       "0" "$(exit_code test -f "${dest_dir}/pre")"
check "install_binary sets executable"     "0" "$(exit_code test -x "${dest_dir}/pre")"
rm -rf "$dest_dir"

printf "\n%d passed, %d failed\n" "$passed" "$failed"
[ "$failed" -eq 0 ]
