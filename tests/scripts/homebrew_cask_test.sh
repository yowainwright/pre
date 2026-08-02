#!/usr/bin/env sh

set -eu

script_dir="$(dirname "$0")"
repo_root="$(CDPATH='' cd -- "$script_dir/../.." && pwd)"
default_dist="$repo_root/dist"
dist_dir="${1:-$default_dist}"
cask_file="$dist_dir/homebrew/Casks/pre.rb"
tap_name="pre-release/cask-smoke"
cask_name="pre-release-smoke"
cask_ref="$tap_name/$cask_name"
binary_target="pre-release-smoke"

fail() {
  printf "homebrew cask test: %s\n" "$1" >&2
  exit 1
}

require_command() {
  command_name="$1"
  command -v "$command_name" >/dev/null 2>&1 || fail "$command_name is required"
}

cleanup() {
  brew uninstall --cask "$cask_name" >/dev/null 2>&1 || true
  brew untap "$tap_name" >/dev/null 2>&1 || true
  rm -rf -- "$test_root"
}

require_command brew
require_command git
require_command ruby
operating_system="$(uname -s)"
machine="$(uname -m)"
[ "$operating_system" = "Darwin" ] || fail "macOS is required"
[ -f "$cask_file" ] || fail "missing $cask_file"

if brew list --cask "$cask_name" >/dev/null 2>&1; then
  fail "$cask_name is already installed"
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

temp_template="${TMPDIR:-/tmp}/pre-cask-smoke.XXXXXX"
test_root="$(mktemp -d "$temp_template")"
tap_repo="$test_root/tap"
local_artifact="$test_root/$asset_name"
test_cask="$tap_repo/Casks/$cask_name.rb"
rewritten_cask="$test_root/$cask_name.rb"
mkdir -p "$tap_repo/Casks" "$test_root/cache" "$test_root/tmp"
trap cleanup EXIT HUP INT TERM

export HOMEBREW_NO_AUTO_UPDATE=1
export HOMEBREW_CACHE="$test_root/cache"
export HOMEBREW_TEMP="$test_root/tmp"

cp "$snapshot_binary" "$local_artifact"
/usr/bin/xattr -d com.apple.quarantine "$local_artifact" 2>/dev/null || true
if /usr/bin/xattr -p com.apple.quarantine "$local_artifact" >/dev/null 2>&1; then
  fail "test artifact still has a quarantine attribute"
fi

sed \
  -e "s/^cask \"pre\" do$/cask \"$cask_name\" do/" \
  -e "s|url \"[^\"]*/$asset_name\"|url \"file://$local_artifact\"|" \
  -e "s/target: \"pre\"/target: \"$binary_target\"/g" \
  "$cask_file" > "$rewritten_cask"

awk '
  /quarantine = system_command/ {
    print "        system_command \"/usr/bin/xattr\","
    print "                       args: [\"-d\", \"com.apple.quarantine\", binary],"
    print "                       must_succeed: false,"
    print "                       print_stderr: false"
  }
  { print }
' "$rewritten_cask" > "$test_cask"

local_url="url \"file://$local_artifact\""
grep -Fq "$local_url" "$test_cask" || fail "local artifact URL was not substituted"
delete_probe='args: ["-d", "com.apple.quarantine", binary]'
delete_count="$(grep -Fc "$delete_probe" "$test_cask")"
[ "$delete_count" -eq 2 ] || fail "missing-attribute precondition was not inserted"
ruby -c "$test_cask" >/dev/null

git -C "$tap_repo" init --quiet
git -C "$tap_repo" add "Casks/$cask_name.rb"
git -C "$tap_repo" \
  -c user.name=Homebrew \
  -c user.email=brew@localhost \
  commit --quiet -m "test: install cask"

brew tap "$tap_name" "file://$tap_repo"
brew install --cask "$cask_ref"

brew_prefix="$(brew --prefix)"
installed_binary="$brew_prefix/bin/$binary_target"
version_pattern='s/^  version "\([^"]*\)"/\1/p'
expected_version="$(sed -n "$version_pattern" "$cask_file")"
actual_version="$("$installed_binary" --version)"
[ "$actual_version" = "$expected_version" ] || fail "installed binary version mismatch"

caskroom="$(brew --caskroom "$cask_name")"
installed_artifact="$caskroom/$expected_version/$asset_name"
[ -f "$installed_artifact" ] || fail "installed cask artifact is missing"
if /usr/bin/xattr -p com.apple.quarantine "$installed_artifact" >/dev/null 2>&1; then
  fail "installed artifact still has a quarantine attribute"
fi

printf "homebrew cask test: installed %s (%s)\n" "$cask_ref" "$actual_version"
