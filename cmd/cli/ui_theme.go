package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorPrimary  = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}
	colorAccent   = lipgloss.AdaptiveColor{Light: "#DB2777", Dark: "#F472B6"}
	colorText     = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"}
	colorMuted    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#8B8FA3"}
	colorSubtle   = lipgloss.AdaptiveColor{Light: "#EDE9FE", Dark: "#2B2445"}
	colorSuccess  = lipgloss.AdaptiveColor{Light: "#047857", Dark: "#34D399"}
	colorDanger   = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	colorHairline = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#343648"}
)

var (
	logoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorText)

	accentStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	primaryStyle = lipgloss.NewStyle().
			Foreground(colorPrimary)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	dangerStyle = lipgloss.NewStyle().
			Foreground(colorDanger)

	selectedStyle = lipgloss.NewStyle().
			Background(colorSubtle).
			Foreground(colorPrimary).
			Bold(true)

	hairlineStyle = lipgloss.NewStyle().
			Foreground(colorHairline)
)

func fieldBorder(focused bool, width int) lipgloss.Style {
	color := colorMuted
	if focused {
		color = colorPrimary
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(color).
		Padding(0, 1).
		Width(max(1, width-4))
}

func centeredCard(width, height int, content string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func truncate(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func wrapText(value string, width int) string {
	if width < 1 {
		width = 1
	}

	var out []string
	for _, paragraph := range strings.Split(value, "\n") {
		if strings.TrimSpace(paragraph) == "" {
			out = append(out, "")
			continue
		}

		var line strings.Builder
		lineWidth := 0

		for _, word := range strings.Fields(paragraph) {
			wordWidth := lipgloss.Width(word)
			if wordWidth > width {
				if lineWidth > 0 {
					out = append(out, line.String())
					line.Reset()
					lineWidth = 0
				}
				out = append(out, splitLongWord(word, width)...)
				continue
			}

			switch {
			case lineWidth == 0:
				line.WriteString(word)
				lineWidth = wordWidth
			case lineWidth+1+wordWidth <= width:
				line.WriteByte(' ')
				line.WriteString(word)
				lineWidth += 1 + wordWidth
			default:
				out = append(out, line.String())
				line.Reset()
				line.WriteString(word)
				lineWidth = wordWidth
			}
		}

		if lineWidth > 0 {
			out = append(out, line.String())
		}
	}

	return strings.Join(out, "\n")
}

func splitLongWord(word string, width int) []string {
	runes := []rune(word)
	var chunks []string
	for len(runes) > width {
		chunks = append(chunks, string(runes[:width]))
		runes = runes[width:]
	}
	if len(runes) > 0 {
		chunks = append(chunks, string(runes))
	}
	return chunks
}

func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func inline(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", " · ")
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func relativeTime(raw string) string {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return ""
	}

	age := time.Since(parsed)
	switch {
	case age < time.Minute:
		return "now"
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	default:
		return parsed.Local().Format("Jan 2")
	}
}

func formatClock(raw string) string {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return ""
	}
	return parsed.Local().Format("15:04")
}
