package proxy

import (
	"os"
	"strconv"
	"strings"
)

const (
	envDisable      = "PRE_DISABLE"
	envQuiet        = "PRE_QUIET"
	envNoBackground = "PRE_NO_BACKGROUND"
	envMaxPackages  = "PRE_MAX_PACKAGES"
)

func envFlag(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func disableEnabled() bool {
	return envFlag(envDisable)
}

func quietEnabled() bool {
	return envFlag(envQuiet)
}

func backgroundDisabled() bool {
	return envFlag(envNoBackground)
}

func maxPackages() int {
	value := strings.TrimSpace(os.Getenv(envMaxPackages))
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func packageLimitExceeded(count int) (int, bool) {
	limit := maxPackages()
	return limit, limit > 0 && count > limit
}
