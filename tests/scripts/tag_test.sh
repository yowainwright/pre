#!/usr/bin/env sh

_PRE_TAG_SOURCED=1
. "$(dirname "$0")/../../scripts/tag.sh"
set +e

passed=0
failed=0

check() {
  label="$1"; expected="$2"; actual="$3"
  if [ "$expected" = "$actual" ]; then
    printf "ok  %s\n" "$label"; passed=$((passed + 1))
  else
    printf "FAIL %s\n  want: %s\n  got:  %s\n" "$label" "$expected" "$actual"
    failed=$((failed + 1))
  fi
}

# runs $@ in a subshell so exit calls don't swallow the exit code
exit_code() { ( "$@" 2>/dev/null ); echo $?; }

# --- validate_tag ---

check "validate_tag accepts v1.0.0"      "0" "$(exit_code validate_tag "v1.0.0")"
check "validate_tag accepts v0.1.2-rc.1" "0" "$(exit_code validate_tag "v0.1.2-rc.1")"
check "validate_tag rejects bare number" "1" "$(exit_code validate_tag "1.0.0")"
check "validate_tag rejects empty"       "1" "$(exit_code validate_tag "")"

# --- check_clean ---

git_is_dirty() { return 0; }
check "check_clean fails when dirty"  "1" "$(exit_code check_clean)"

git_is_dirty() { return 1; }
check "check_clean passes when clean" "0" "$(exit_code check_clean)"

# --- check_exists ---

git_tag_exists() { return 0; }
check "check_exists fails when tag exists"  "1" "$(exit_code check_exists "v1.0.0")"

git_tag_exists() { return 1; }
check "check_exists passes when tag is new" "0" "$(exit_code check_exists "v1.0.0")"

# --- prompt_prerelease ---

svu_prerelease() {
  case "$1/$2" in
    patch/alpha) echo "v1.0.1-alpha.1" ;;
    patch/beta)  echo "v1.0.1-beta.1"  ;;
    patch/rc)    echo "v1.0.1-rc.1"    ;;
    minor/alpha) echo "v1.1.0-alpha.1" ;;
    minor/beta)  echo "v1.1.0-beta.1"  ;;
    minor/rc)    echo "v1.1.0-rc.1"    ;;
  esac
}

read_line() { REPLY="1"; }
check "prompt_prerelease none"              "v1.0.1"         "$(prompt_prerelease patch v1.0.1)"

read_line() { REPLY="2"; }
check "prompt_prerelease alpha"             "v1.0.1-alpha.1" "$(prompt_prerelease patch v1.0.1)"

read_line() { REPLY="3"; }
check "prompt_prerelease beta"              "v1.0.1-beta.1"  "$(prompt_prerelease patch v1.0.1)"

read_line() { REPLY="4"; }
check "prompt_prerelease rc"                "v1.0.1-rc.1"    "$(prompt_prerelease patch v1.0.1)"

read_line() { REPLY=""; }
check "prompt_prerelease empty → none"      "v1.0.1"         "$(prompt_prerelease patch v1.0.1)"

# --- prompt_bump (real stdin via pipe) ---

svu_current() { echo "v1.0.0"; }
svu_patch()   { echo "v1.0.1"; }
svu_minor()   { echo "v1.1.0"; }
svu_major()   { echo "v2.0.0"; }
read_line()   { read -r REPLY; }

check "prompt_bump patch+none"  "v1.0.1"         "$(printf '1\n1\n' | prompt_bump)"
check "prompt_bump minor+none"  "v1.1.0"         "$(printf '2\n1\n' | prompt_bump)"
check "prompt_bump major+none"  "v2.0.0"         "$(printf '3\n1\n' | prompt_bump)"
check "prompt_bump patch+alpha" "v1.0.1-alpha.1" "$(printf '1\n2\n' | prompt_bump)"
check "prompt_bump minor+beta"  "v1.1.0-beta.1"  "$(printf '2\n3\n' | prompt_bump)"
check "prompt_bump custom"      "v1.2.3"         "$(printf '4\nv1.2.3\n' | prompt_bump)"
check "prompt_bump raw version" "v9.0.0"         "$(printf 'v9.0.0\n' | prompt_bump)"

# --- confirm_tag ---

git_short_sha() { echo "abc1234"; }

read_line() { REPLY="y"; }
check "confirm_tag yes passes"   "0" "$(exit_code confirm_tag "v1.0.0")"

read_line() { REPLY="N"; }
check "confirm_tag no exits 0"   "0" "$(exit_code confirm_tag "v1.0.0")"

printf "\n%d passed, %d failed\n" "$passed" "$failed"
[ "$failed" -eq 0 ]
