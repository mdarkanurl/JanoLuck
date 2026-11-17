package llm

import "testing"

func TestFormatProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile *Profile
		want    string
	}{
		{name: "nil", profile: nil, want: ""},
		{name: "empty", profile: &Profile{}, want: ""},
		{
			name:    "name only",
			profile: &Profile{Name: "  Ada  "},
			want:    "Name: Ada",
		},
		{
			name: "skips blank questions and answers",
			profile: &Profile{
				Name: "Ada",
				Questions: []ProfileQuestion{
					{Question: " ", Answer: "ignored"},
					{Question: "ignored", Answer: " "},
					{Question: "What is your name?", Answer: " Ada "},
				},
			},
			want: "Name: Ada\n1. What is your name?\n   Answer: Ada",
		},
		{
			name: "numbers every answer",
			profile: &Profile{
				Questions: []ProfileQuestion{
					{Question: "One?", Answer: "first"},
					{Question: "Two?", Answer: "second"},
				},
			},
			want: "1. One?\n   Answer: first\n2. Two?\n   Answer: second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatProfile(tt.profile); got != tt.want {
				t.Errorf("formatProfile() = %q, want %q", got, tt.want)
			}
		})
	}
}
