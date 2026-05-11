package clisession

import "strings"

func shouldRetryWithFreshSession(existingSession bool, err error) bool {
	if !existingSession || err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "session id") &&
		(strings.Contains(message, "already in use") || strings.Contains(message, "in use"))
}
