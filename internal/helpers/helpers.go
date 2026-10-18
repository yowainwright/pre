package helpers

// AppendUniqueStrings appends unseen values in order and records them in seen.
func AppendUniqueStrings(result []string, seen map[string]bool, values []string) []string {
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
