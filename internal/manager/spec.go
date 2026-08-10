package manager

import (
	"regexp"
	"strings"
)

const (
	brewLockVersionSeparator = "@@"
	unsupportedPyRequirement = "<unsupported>"
)

var pyRequirementRE = regexp.MustCompile(`^([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)(?:\[[A-Za-z0-9._,-]+\])?\s*(.*)$`)

func ParseSpec(ecosystem, spec string) (name, version string) {
	switch ecosystem {
	case "npm", "Go", "crates.io":
		return parseAtSeparator(spec)
	case "Homebrew":
		return parseHomebrewSpec(spec)
	case "PyPI":
		return parsePySpec(spec)
	default:
		return spec, ""
	}
}

func parseAtSeparator(spec string) (string, string) {
	idx := strings.LastIndex(spec, "@")
	if idx <= 0 {
		return spec, ""
	}
	return spec[:idx], spec[idx+1:]
}

func parseHomebrewSpec(spec string) (string, string) {
	idx := strings.LastIndex(spec, brewLockVersionSeparator)
	if idx <= 0 {
		return spec, ""
	}
	return spec[:idx], spec[idx+len(brewLockVersionSeparator):]
}

func parsePySpec(spec string) (string, string) {
	requirement, _, _ := strings.Cut(spec, ";")
	matches := pyRequirementRE.FindStringSubmatch(strings.TrimSpace(requirement))
	if len(matches) != 3 {
		return "", unsupportedPyRequirement
	}
	name := matches[1]
	constraint := strings.TrimSpace(matches[2])
	return parsePyConstraint(name, constraint)
}

func parsePyConstraint(name, constraint string) (string, string) {
	if constraint == "" {
		return name, ""
	}
	isSupportedConstraint := strings.ContainsAny(constraint[:1], "<>=!~@")
	if !isSupportedConstraint {
		return name, unsupportedPyRequirement
	}
	if !strings.HasPrefix(constraint, "==") {
		return name, constraint
	}
	version := strings.TrimSpace(strings.TrimPrefix(constraint, "=="))
	isExactPin := version != "" && !strings.ContainsAny(version, "*,")
	if !isExactPin {
		return name, constraint
	}
	return name, version
}
