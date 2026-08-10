package manager

import (
	"strconv"
	"strings"
)

var cargoWildcardReplacer = strings.NewReplacer("X", "*", "x", "*")

type crateVersionInfo struct {
	Num    string `json:"num"`
	Yanked bool   `json:"yanked"`
}

type cargoSemver struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

type cargoPartialVersion struct {
	version    cargoSemver
	components int
}

func selectCargoVersion(versions []crateVersionInfo, requirement string) (string, bool) {
	var best cargoSemver
	bestRaw := ""
	for _, candidate := range versions {
		version, ok := parseCargoSemver(candidate.Num)
		if candidate.Yanked || !ok || !cargoRequirementMatches(requirement, version) {
			continue
		}
		isNewBest := bestRaw == "" || compareCargoSemver(version, best) > 0
		if isNewBest {
			best = version
			bestRaw = candidate.Num
		}
	}
	return bestRaw, bestRaw != ""
}

func parseCargoSemver(input string) (cargoSemver, bool) {
	partial, ok := parseCargoPartialVersion(input)
	if !ok || partial.components != 3 {
		return cargoSemver{}, false
	}
	return partial.version, true
}

func parseCargoPartialVersion(input string) (cargoPartialVersion, bool) {
	withoutBuild := strings.SplitN(strings.TrimSpace(input), "+", 2)[0]
	core, prerelease, _ := strings.Cut(withoutBuild, "-")
	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return cargoPartialVersion{}, false
	}
	numbers := [3]int{}
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return cargoPartialVersion{}, false
		}
		numbers[index] = value
	}
	version := cargoSemver{major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: prerelease}
	return cargoPartialVersion{version: version, components: len(parts)}, true
}

func compareCargoSemver(left, right cargoSemver) int {
	leftNumbers := []int{left.major, left.minor, left.patch}
	rightNumbers := []int{right.major, right.minor, right.patch}
	for index := range leftNumbers {
		if leftNumbers[index] < rightNumbers[index] {
			return -1
		}
		if leftNumbers[index] > rightNumbers[index] {
			return 1
		}
	}
	return compareCargoPrerelease(left.prerelease, right.prerelease)
}

func compareCargoPrerelease(left, right string) int {
	if left == right {
		return 0
	}
	if left == "" {
		return 1
	}
	if right == "" {
		return -1
	}
	return compareCargoPrereleaseParts(strings.Split(left, "."), strings.Split(right, "."))
}

func compareCargoPrereleaseParts(leftParts, rightParts []string) int {
	limit := min(len(leftParts), len(rightParts))
	for index := 0; index < limit; index++ {
		comparison := compareCargoPrereleasePart(leftParts[index], rightParts[index])
		if comparison != 0 {
			return comparison
		}
	}
	return compareInt(len(leftParts), len(rightParts))
}

func compareCargoPrereleasePart(left, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	switch {
	case leftErr == nil && rightErr == nil:
		return compareInt(leftNumber, rightNumber)
	case leftErr == nil:
		return -1
	case rightErr == nil:
		return 1
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareInt(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func cargoRequirementMatches(requirement string, version cargoSemver) bool {
	requirement = strings.TrimSpace(requirement)
	if version.prerelease != "" && !cargoRequirementAllowsPrerelease(requirement, version) {
		return false
	}
	for _, comparator := range strings.Split(requirement, ",") {
		if !cargoComparatorMatches(strings.TrimSpace(comparator), version) {
			return false
		}
	}
	return true
}

func cargoRequirementAllowsPrerelease(requirement string, version cargoSemver) bool {
	for _, comparator := range strings.Split(requirement, ",") {
		_, rawVersion := splitCargoComparator(strings.TrimSpace(comparator))
		partial, ok := parseCargoPartialVersion(rawVersion)
		if !ok || partial.version.prerelease == "" {
			continue
		}
		if sameCargoVersionCore(partial.version, version) {
			return true
		}
	}
	return false
}

func sameCargoVersionCore(left, right cargoSemver) bool {
	sameMajor := left.major == right.major
	sameMinor := left.minor == right.minor
	samePatch := left.patch == right.patch
	return sameMajor && sameMinor && samePatch
}

func cargoComparatorMatches(comparator string, version cargoSemver) bool {
	if comparator == "" || comparator == "*" {
		return true
	}
	if hasCargoWildcard(comparator) {
		return cargoWildcardMatches(comparator, version)
	}
	operator, rawVersion := splitCargoComparator(comparator)
	partial, ok := parseCargoPartialVersion(rawVersion)
	if !ok {
		return false
	}
	return cargoOperatorMatches(operator, partial, version)
}

func hasCargoWildcard(comparator string) bool {
	if strings.Contains(comparator, "*") {
		return true
	}
	for _, component := range strings.Split(strings.ToLower(comparator), ".") {
		if component == "x" {
			return true
		}
	}
	return false
}

func splitCargoComparator(comparator string) (string, string) {
	operators := []string{">=", "<=", ">", "<", "=", "^", "~"}
	for _, operator := range operators {
		if strings.HasPrefix(comparator, operator) {
			rawVersion := strings.TrimSpace(strings.TrimPrefix(comparator, operator))
			return operator, rawVersion
		}
	}
	return "^", comparator
}

func cargoOperatorMatches(operator string, partial cargoPartialVersion, version cargoSemver) bool {
	comparison := compareCargoSemver(version, partial.version)
	switch operator {
	case "=":
		return cargoExactMatches(partial, version, comparison)
	case ">=":
		return comparison >= 0
	case ">":
		return cargoGreaterMatches(partial, version, comparison)
	case "<=":
		return cargoLessOrEqualMatches(partial, version, comparison)
	case "<":
		return comparison < 0
	case "~":
		return cargoRangeMatches(comparison, version, cargoTildeUpperBound(partial))
	default:
		return cargoRangeMatches(comparison, version, cargoCaretUpperBound(partial))
	}
}

func cargoExactMatches(partial cargoPartialVersion, version cargoSemver, comparison int) bool {
	if partial.components == 3 {
		return comparison == 0
	}
	return cargoRangeMatches(comparison, version, cargoPartialUpperBound(partial))
}

func cargoGreaterMatches(partial cargoPartialVersion, version cargoSemver, comparison int) bool {
	if partial.components == 3 {
		return comparison > 0
	}
	return compareCargoSemver(version, cargoPartialUpperBound(partial)) >= 0
}

func cargoLessOrEqualMatches(partial cargoPartialVersion, version cargoSemver, comparison int) bool {
	if partial.components == 3 {
		return comparison <= 0
	}
	return compareCargoSemver(version, cargoPartialUpperBound(partial)) < 0
}

func cargoRangeMatches(lowerComparison int, version, upper cargoSemver) bool {
	return lowerComparison >= 0 && compareCargoSemver(version, upper) < 0
}

func cargoCaretUpperBound(partial cargoPartialVersion) cargoSemver {
	version := partial.version
	if version.major > 0 || partial.components == 1 {
		return cargoSemver{major: version.major + 1}
	}
	if version.minor > 0 || partial.components == 2 {
		return cargoSemver{minor: version.minor + 1}
	}
	return cargoSemver{patch: version.patch + 1}
}

func cargoTildeUpperBound(partial cargoPartialVersion) cargoSemver {
	version := partial.version
	if partial.components == 1 {
		return cargoSemver{major: version.major + 1}
	}
	return cargoSemver{major: version.major, minor: version.minor + 1}
}

func cargoPartialUpperBound(partial cargoPartialVersion) cargoSemver {
	version := partial.version
	if partial.components == 1 {
		return cargoSemver{major: version.major + 1}
	}
	if partial.components == 2 {
		return cargoSemver{major: version.major, minor: version.minor + 1}
	}
	return cargoSemver{major: version.major, minor: version.minor, patch: version.patch + 1}
}

func cargoWildcardMatches(comparator string, version cargoSemver) bool {
	normalized := cargoWildcardReplacer.Replace(comparator)
	prefix := strings.TrimSuffix(normalized, ".*")
	if prefix == "*" || prefix == "" {
		return true
	}
	partial, ok := parseCargoPartialVersion(prefix)
	if !ok {
		return false
	}
	lowerComparison := compareCargoSemver(version, partial.version)
	upper := cargoWildcardUpperBound(partial)
	upperComparison := compareCargoSemver(version, upper)
	return lowerComparison >= 0 && upperComparison < 0
}

func cargoWildcardUpperBound(partial cargoPartialVersion) cargoSemver {
	version := partial.version
	if partial.components == 1 {
		return cargoSemver{major: version.major + 1}
	}
	return cargoSemver{major: version.major, minor: version.minor + 1}
}
