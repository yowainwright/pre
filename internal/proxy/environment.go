package proxy

import (
	"os"
	"strconv"
	"strings"
)

const (
	envDisable     = "PRE_DISABLE"
	envQuiet       = "PRE_QUIET"
	envMaxPackages = "PRE_MAX_PACKAGES"
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

func maxPackages() int {
	value := strings.TrimSpace(os.Getenv(envMaxPackages))
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	invalidLimit := err != nil || n <= 0
	if invalidLimit {
		return 0
	}
	return n
}

func packageLimitExceeded(count int) (int, bool) {
	limit := maxPackages()
	exceeded := limit > 0 && count > limit
	return limit, exceeded
}
