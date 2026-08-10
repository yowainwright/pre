#!/usr/bin/env bash

set -euo pipefail

E2E_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly E2E_DIR
readonly CLEAN_PACKAGE="urllib3"
readonly VULNERABLE_PACKAGE="urllib3==1.24.1"

source "$E2E_DIR/package_manager_test.sh"

e2e_start pip
e2e_run "scan a clean package before installation" pip install "$CLEAN_PACKAGE"
e2e_expect_block "decline a package with a known CVE" pip install "$VULNERABLE_PACKAGE"
e2e_finish
