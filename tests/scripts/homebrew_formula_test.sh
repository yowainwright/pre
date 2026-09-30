#!/usr/bin/env sh

set -eu

script_dir="$(dirname "$0")"
repo_root="$(CDPATH='' cd -- "$script_dir/../.." && pwd)"
default_dist="$repo_root/dist"
dist_dir="${1:-$default_dist}"
formula_file="$dist_dir/homebrew/Formula/pre.rb"
tap_name="pre-release/formula-smoke"
formula_name="pre-release-smoke"
formula_ref="$tap_name/$formula_name"
binary_target="pre-release-smoke"

fail() {
  printf "homebrew formula test: %s\n" "$1" >&2
  exit 1
}

require_command() {
  command_name="$1"
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
}

cleanup() {
  brew uninstall "$formula_name" >/dev/null 2>&1 || true
  brew untap "$tap_name" >/dev/null 2>&1 || true
  rm -rf -- "$test_root"
}

require_command awk
require_command brew
require_command git
require_command ruby
require_command shasum
operating_system="$(uname -s)"
machine="$(uname -m)"
[ "$operating_system" = "Darwin" ] || fail "macOS is required"
[ -f "$formula_file" ] || fail "missing $formula_file"

if brew list "$formula_name" >/dev/null 2>&1; then
  fail "$formula_name is already installed"
fi
if brew tap | grep -Fxq "$tap_name"; then
  fail "$tap_name is already tapped"
fi

case "$machine" in
  arm64) goarch="arm64" ;;
  x86_64) goarch="amd64" ;;
  *) fail "unsupported architecture: $machine" ;;
esac

set -- "$dist_dir"/pre_darwin_"$goarch"_*/pre
[ "$#" -eq 1 ] || fail "expected one darwin/$goarch snapshot artifact"
[ -f "$1" ] || fail "missing darwin/$goarch snapshot artifact"
snapshot_binary="$1"
asset_name="pre-darwin-$goarch"

temp_template="${TMPDIR:-/tmp}/pre-formula-smoke.XXXXXX"
test_root="$(mktemp -d "$temp_template")"
tap_repo="$test_root/tap"
local_artifact="$test_root/$asset_name"
test_formula="$tap_repo/Formula/$formula_name.rb"
mkdir -p "$tap_repo/Formula" "$test_root/cache" "$test_root/tmp"
trap cleanup EXIT HUP INT TERM

export HOMEBREW_NO_AUTO_UPDATE=1
export HOMEBREW_CACHE="$test_root/cache"
export HOMEBREW_TEMP="$test_root/tmp"

cp "$snapshot_binary" "$local_artifact"
artifact_sha="$(shasum -a 256 "$local_artifact" | awk '{print $1}')"

awk \
  -v asset_name="$asset_name" \
  -v artifact_sha="$artifact_sha" \
  -v binary_target="$binary_target" \
  -v local_url="file://$local_artifact" '
    /^class Pre < Formula$/ {
      print "class PreReleaseSmoke < Formula"
      next
    }
    $0 ~ "url .*" asset_name "\"" {
      sub(/url "[^"]+"/, "url \"" local_url "\"")
      needs_sha = 1
      print
      next
    }
    needs_sha && /sha256 "[^"]+"/ {
      sub(/sha256 "[^"]+"/, "sha256 \"" artifact_sha "\"")
      needs_sha = 0
    }
    {
      gsub(/=> "pre"/, "=> \"" binary_target "\"")
      print
    }
  ' "$formula_file" > "$test_formula"

local_url="url \"file://$local_artifact\""
grep -Fq "$local_url" "$test_formula" || fail "local artifact URL was not substituted"
grep -Fq "sha256 \"$artifact_sha\"" "$test_formula" || fail "local artifact sha256 was not substituted"
ruby -c "$test_formula" >/dev/null

git -C "$tap_repo" init --quiet
git -C "$tap_repo" add "Formula/$formula_name.rb"
git -C "$tap_repo" \
  -c user.name=Homebrew \
  -c user.email=brew@localhost \
  commit --quiet -m "test: install formula"

brew tap "$tap_name" "file://$tap_repo"
brew install "$formula_ref"

installed_binary="$(brew --prefix)/bin/$binary_target"
version_pattern='s/^  version "\([^"]*\)"/\1/p'
expected_version="$(sed -n "$version_pattern" "$formula_file")"
actual_version="$("$installed_binary" --version)"
[ "$actual_version" = "$expected_version" ] || fail "installed binary version mismatch"

printf "homebrew formula test: installed %s (%s)\n" "$formula_ref" "$actual_version"
