package protocol

import "regexp"

var mentionPattern = regexp.MustCompile(`@([A-Za-z0-9._-]+)`)

func NormalizeMentions(parts []Part, explicit []string) []string {
	seen := make(map[string]struct{}, len(explicit))
	mentions := make([]string, 0, len(explicit))

	for _, mention := range explicit {
		if mention == "" {
			continue
		}

		if _, ok := seen[mention]; ok {
			continue
		}

		seen[mention] = struct{}{}
		mentions = append(mentions, mention)
	}

	for _, part := range parts {
		if part.Kind != "text" || part.Text == "" {
			continue
		}

		matches := mentionPattern.FindAllStringSubmatch(part.Text, -1)
		for _, match := range matches {
			if len(match) < 2 || match[1] == "" {
				continue
			}

			if _, ok := seen[match[1]]; ok {
				continue
			}

			seen[match[1]] = struct{}{}
			mentions = append(mentions, match[1])
		}
	}

	if len(mentions) == 0 {
		return nil
	}

	return mentions
}
