package protocol

import (
	"slices"
	"strings"
)

func UniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		unique = append(unique, trimmed)
	}

	return unique
}

func SortedUniqueTrimmedStrings(values []string) []string {
	unique := UniqueTrimmedStrings(values)
	slices.Sort(unique)
	return unique
}
