package manager

import "strings"

const brewLockVersionSeparator = "@@"

func ParseSpec(ecosystem, spec string) (name, version string) {
	switch ecosystem {
	case "npm", "Go":
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
	for _, sep := range []string{"==", ">=", "<=", ">", "<", "!=", "~="} {
		if idx := strings.Index(spec, sep); idx != -1 {
			if sep == "==" {
				return spec[:idx], spec[idx+2:]
			}
			return spec[:idx], ""
		}
	}
	return spec, ""
}
