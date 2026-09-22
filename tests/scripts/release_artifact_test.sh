#!/usr/bin/env sh
# POSIX sh has no pipefail; commands are checked separately without pipelines.
set -eu

fail() {
	printf 'release artifact test: %s\n' "${1:?}" >&2
	exit 1
}

host_platform() {
	host="$(uname -s)-$(uname -m)"
	case "$host" in
	Darwin-arm64) printf 'darwin_arm64\n' ;;
	Darwin-x86_64) printf 'darwin_amd64\n' ;;
	Linux-aarch64 | Linux-arm64) printf 'linux_arm64\n' ;;
	Linux-x86_64) printf 'linux_amd64\n' ;;
	*) fail "unsupported host: $host" ;;
	esac
}

snapshot_binary() {
	platform="$(host_platform)"
	set -- "${1:?}/pre_${platform}"_*/pre
	[ "$#" -eq 1 ] || fail "expected one $platform binary"
	[ -x "$1" ] || fail "missing executable: $1"
	printf '%s\n' "$1"
}

check_binary() {
	actual_version="$(env HOME="$test_root" XDG_CONFIG_HOME="$test_root/config" PRE_OBS=0 "$test_root/bin/pre" --version)"
	[ "$actual_version" = "$expected_version" ] || fail "version mismatch: $actual_version != $expected_version"
	env HOME="$test_root" XDG_CONFIG_HOME="$test_root/config" PRE_OBS=0 "$test_root/bin/pre" config >"$test_root/config-output"
	grep -Fq 'api.endpoint  https://api.osv.dev/v1/query' "$test_root/config-output" || fail "unexpected default configuration"
}

cleanup() {
	rm -rf -- "${test_root:?}"
}

main() {
	dist_input="${1:-dist}"
	dist_path="$(CDPATH='' cd -- "$dist_input" && pwd)"
	binary="$(snapshot_binary "$dist_path")"
	expected_version="$(ruby -r json -e 'puts JSON.parse(File.read(ARGV.fetch(0))).fetch("version")' "$dist_path/metadata.json")"
	[ -n "$expected_version" ] || fail "missing snapshot version"
	test_root="$(mktemp -d "$dist_path/pre-smoke.XXXXXX")"
	trap cleanup EXIT
	trap 'exit 129' HUP
	trap 'exit 130' INT
	trap 'exit 143' TERM
	mkdir -p "$test_root/bin"
	cp "$binary" "$test_root/bin/pre"
	check_binary
	printf 'release artifact test: passed (%s)\n' "$expected_version"
}

main "$@"
