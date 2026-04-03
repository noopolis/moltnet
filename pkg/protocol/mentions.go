package protocol

import "regexp"

var mentionPattern = regexp.MustCompile(`@([A-Za-z0-9._-]+)`)

func ParseMentions(text string) []string {
	return parseMentionMatches(mentionPattern.FindAllStringSubmatch(text, -1))
}

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
		if part.Kind != PartKindText || part.Text == "" {
			continue
		}

		for _, match := range parseMentionMatches(mentionPattern.FindAllStringSubmatch(part.Text, -1)) {
			if _, ok := seen[match]; ok {
				continue
			}

			seen[match] = struct{}{}
			mentions = append(mentions, match)
		}
	}

	if len(mentions) == 0 {
		return nil
	}

	return mentions
}

func parseMentionMatches(matches [][]string) []string {
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	mentions := make([]string, 0, len(matches))
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

	return mentions
}
