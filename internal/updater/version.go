package updater

import (
	"fmt"
	"strconv"
	"strings"
)

type parsedVersion struct {
	core       []int
	prerelease []string
}

func NormalizeVersion(version string) string {
	trimmed := strings.TrimSpace(version)
	trimmed = strings.TrimPrefix(trimmed, "v")
	return strings.TrimPrefix(trimmed, "V")
}

func IsDevelopmentVersion(version string) bool {
	normalized := strings.ToLower(NormalizeVersion(version))
	return normalized == "" ||
		normalized == "0.0.0-dev" ||
		strings.Contains(normalized, "dev") ||
		strings.Contains(normalized, "dirty") ||
		strings.Contains(normalized, "+")
}

func CompareVersions(left string, right string) (int, error) {
	parsedLeft, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	parsedRight, err := parseVersion(right)
	if err != nil {
		return 0, err
	}

	maxParts := len(parsedLeft.core)
	if len(parsedRight.core) > maxParts {
		maxParts = len(parsedRight.core)
	}
	for index := 0; index < maxParts; index++ {
		leftPart := versionPart(parsedLeft.core, index)
		rightPart := versionPart(parsedRight.core, index)
		if leftPart < rightPart {
			return -1, nil
		}
		if leftPart > rightPart {
			return 1, nil
		}
	}

	return comparePrerelease(parsedLeft.prerelease, parsedRight.prerelease), nil
}

func parseVersion(version string) (parsedVersion, error) {
	normalized := NormalizeVersion(version)
	withoutBuild, _, _ := strings.Cut(normalized, "+")
	coreText, prereleaseText, _ := strings.Cut(withoutBuild, "-")
	if strings.TrimSpace(coreText) == "" {
		return parsedVersion{}, fmt.Errorf("version %q is empty", version)
	}

	coreFields := strings.Split(coreText, ".")
	core := make([]int, 0, len(coreFields))
	for _, field := range coreFields {
		if field == "" {
			return parsedVersion{}, fmt.Errorf("version %q has an empty numeric component", version)
		}
		value, err := strconv.Atoi(field)
		if err != nil || value < 0 {
			return parsedVersion{}, fmt.Errorf("version %q has invalid numeric component %q", version, field)
		}
		core = append(core, value)
	}

	var prerelease []string
	if prereleaseText != "" {
		prerelease = strings.Split(prereleaseText, ".")
	}
	return parsedVersion{core: core, prerelease: prerelease}, nil
}

func versionPart(parts []int, index int) int {
	if index >= len(parts) {
		return 0
	}
	return parts[index]
}

func comparePrerelease(left []string, right []string) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}

	maxParts := len(left)
	if len(right) > maxParts {
		maxParts = len(right)
	}
	for index := 0; index < maxParts; index++ {
		if index >= len(left) {
			return -1
		}
		if index >= len(right) {
			return 1
		}
		result := comparePrereleasePart(left[index], right[index])
		if result != 0 {
			return result
		}
	}
	return 0
}

func comparePrereleasePart(left string, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	if leftErr == nil && rightErr == nil {
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
		return 0
	}
	if leftErr == nil {
		return -1
	}
	if rightErr == nil {
		return 1
	}
	return strings.Compare(left, right)
}
