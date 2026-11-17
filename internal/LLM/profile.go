package llm

import (
	"fmt"
	"strings"
)

type ProfileQuestion struct {
	Question string
	Answer   string
}

type Profile struct {
	Name      string
	Questions []ProfileQuestion
}

func formatProfile(profile *Profile) string {
	if profile == nil {
		return ""
	}

	var sb strings.Builder

	if name := strings.TrimSpace(profile.Name); name != "" {
		sb.WriteString("Name: ")
		sb.WriteString(name)
	}

	index := 0
	for _, qa := range profile.Questions {
		question := strings.TrimSpace(qa.Question)
		answer := strings.TrimSpace(qa.Answer)
		if question == "" || answer == "" {
			continue
		}

		index++
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "%d. %s\n   Answer: %s", index, question, answer)
	}

	return strings.TrimSpace(sb.String())
}
