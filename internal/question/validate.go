package question

import (
	"regexp"
	"strings"
)

var optionLabel = regexp.MustCompile(`^([A-Z])[.、．:：)）][ \t]*(.+)$`)

// ValidateAnswer checks format and unambiguous option membership, not factual truth.
func ValidateAnswer(q Question, answer string) (string, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" || len(answer) > 32768 {
		return "", ErrProtocol
	}
	if q.Type == "judgement" {
		switch strings.ToLower(answer) {
		case "对", "正确", "true":
			return "对", nil
		case "错", "错误", "false":
			return "错", nil
		default:
			return "", ErrProtocol
		}
	}
	if q.Type != "single" && q.Type != "multiple" && q.Type != "completion" {
		return answer, nil
	}
	parts := strings.Split(answer, "###")
	if q.Type == "single" && len(parts) != 1 {
		return "", ErrProtocol
	}
	options := choiceContents(q.Options)
	seen := map[string]bool{}
	output := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", ErrProtocol
		}
		if q.Type != "completion" && options != nil && !options[part] {
			return "", ErrProtocol
		}
		if q.Type != "multiple" || !seen[part] {
			output = append(output, part)
		}
		seen[part] = true
	}
	return strings.Join(output, "###"), nil
}

// Only consecutive labelled lines are unambiguous. Unlabelled/multiline options
// remain opaque; splitting them heuristically could change their meaning.
func choiceContents(raw string) map[string]bool {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n")), "\n")
	if len(lines) < 2 || len(lines) > 26 {
		return nil
	}
	values := map[string]bool{}
	for i, line := range lines {
		match := optionLabel.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil || match[1][0] != byte('A'+i) {
			return nil
		}
		value := strings.TrimSpace(match[2])
		if value == "" || values[value] {
			return nil
		}
		values[value] = true
	}
	return values
}
