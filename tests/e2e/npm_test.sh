#!/usr/bin/env bash

set -euo pipefail

E2E_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly E2E_DIR
readonly CLEAN_PACKAGE="lodash@4.17.21"
readonly VULNERABLE_PACKAGE="minimist@0.0.8"

source "$E2E_DIR/package_manager_test.sh"

e2e_start npm
e2e_run "initialize a package without interception" npm init --yes
e2e_run "scan a clean package before installation" npm install "$CLEAN_PACKAGE"
e2e_expect_block "decline a package with a known CVE" npm install "$VULNERABLE_PACKAGE"
e2e_finish
