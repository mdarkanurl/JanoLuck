package main

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var pointerOut io.Writer

var pointerShape = "default"

func setPointerShape(shape string) {
	switch shape {
	case "pointer", "text":
	default:
		shape = "default"
	}
	if pointerOut == nil || shape == pointerShape {
		return
	}
	pointerShape = shape
	_, _ = fmt.Fprintf(pointerOut, "\x1b]22;%s\x1b\\", shape)
}

func resetPointerShape() {
	setPointerShape("default")
}

type clickTarget struct {
	action string
	index  int
	x, y   int
	width  int
	height int
}

type clickMap []clickTarget

func (c clickMap) shift(dx, dy int) clickMap {
	out := make(clickMap, len(c))
	for i, target := range c {
		target.x += dx
		target.y += dy
		out[i] = target
	}
	return out
}

func (c clickMap) at(x, y int) (clickTarget, bool) {
	for _, target := range c {
		if x >= target.x && x < target.x+target.width && y >= target.y && y < target.y+target.height {
			return target, true
		}
	}
	return clickTarget{}, false
}

func (t clickTarget) pointerShape() string {
	switch t.action {
	case "field", "composer", "filter":
		return "text"
	default:
		return "pointer"
	}
}

type actionItem struct {
	key    string
	label  string
	action string
}

func renderActions(items []actionItem, hovered string, width int) ([]string, clickMap) {
	if width < 10 {
		width = 10
	}

	var lines []string
	var targets clickMap
	var b strings.Builder
	x, y := 0, 0

	flush := func() {
		lines = append(lines, b.String())
		b.Reset()
		x = 0
		y = len(lines)
	}

	for _, item := range items {
		label := item.label
		if item.action != "" {
			label = "[" + item.key + "] " + item.label
		}
		w := lipgloss.Width(label)

		need := w
		if x > 0 {
			need += 4
		}
		if x > 0 && x+need > width {
			flush()
		}
		if x > 0 {
			b.WriteString("    ")
			x += 4
		}

		switch {
		case item.action == "":
			b.WriteString(mutedStyle.Render(item.label))
		case item.action == hovered:
			b.WriteString(hoverStyle.Render(label))
		default:
			b.WriteString(accentStyle.Render("["+item.key+"]") + mutedStyle.Render(" "+item.label))
		}

		if item.action != "" {
			targets = append(targets, clickTarget{
				action: item.action,
				x:      x,
				y:      y,
				width:  w,
				height: 1,
			})
		}
		x += w
	}

	if b.Len() > 0 || len(lines) == 0 {
		flush()
	}

	return lines, targets
}

type layoutBuilder struct {
	body    strings.Builder
	line    int
	targets clickMap
}

func (b *layoutBuilder) write(s string) int {
	start := b.line
	b.body.WriteString(s)
	b.line += strings.Count(s, "\n")
	return start
}

func (b *layoutBuilder) add(target clickTarget) {
	b.targets = append(b.targets, target)
}

func (b *layoutBuilder) addActions(items []actionItem, hovered string, width, x int) {
	lines, spans := renderActions(items, hovered, width)
	start := b.write(strings.Join(lines, "\n") + "\n")
	for _, span := range spans {
		span.x += x
		span.y += start
		b.targets = append(b.targets, span)
	}
}

func (b *layoutBuilder) addCenteredActions(items []actionItem, hovered string, width int) {
	lines, spans := renderActions(items, hovered, width)
	start := b.write(strings.Join(lines, "\n"))
	for _, span := range spans {
		if span.y >= 0 && span.y < len(lines) {
			span.x += centerPad(max(0, width-lipgloss.Width(lines[span.y])))
		}
		span.y += start
		b.targets = append(b.targets, span)
	}
}

func centerPad(gap int) int {
	if gap <= 0 {
		return 0
	}
	return gap - int(math.Round(float64(gap)*0.5))
}

func (b *layoutBuilder) String() string {
	return b.body.String()
}

var (
	colorPrimary  = lipgloss.AdaptiveColor{Light: "#6D28D9", Dark: "#A78BFA"}
	colorAccent   = lipgloss.AdaptiveColor{Light: "#DB2777", Dark: "#F472B6"}
	colorText     = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"}
	colorMuted    = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#8B8FA3"}
	colorSubtle   = lipgloss.AdaptiveColor{Light: "#EDE9FE", Dark: "#2B2445"}
	colorHover    = lipgloss.AdaptiveColor{Light: "#F5F3FF", Dark: "#2A2540"}
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

	hoverStyle = lipgloss.NewStyle().
			Background(colorHover).
			Foreground(colorText)

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
		Width(max(1, width-2))
}

func hoverBorder(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(0, 1).
		Width(max(1, width-2))
}

func hoverField(label, view string, width int) string {
	return mutedStyle.Render(truncate(label, width)) + "\n" + hoverBorder(width).Render(view)
}

func centeredCard(width, height int, content string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func padLines(content string, width int) string {
	return lipgloss.NewStyle().Width(max(1, width)).Render(content)
}

func panelLayout(width, height int, content string, inner, padX, padY int) (screen string, ox, oy int) {
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(padY, padX).
		Width(inner + 2*padX).
		Render(content)

	lines := strings.Split(panel, "\n")
	top, left := centerOffsets(width, height, lines)

	return centeredCard(width, height, panel), left + 1 + padX, top + 1 + padY
}

func centeredBlock(width, height int, block string) (screen string, ox, oy int) {
	lines := strings.Split(block, "\n")
	top, left := centerOffsets(width, height, lines)
	return centeredCard(width, height, block), left, top
}

func centerOffsets(width, height int, lines []string) (top, left int) {
	if len(lines) == 0 {
		return 0, 0
	}
	blockWidth := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > blockWidth {
			blockWidth = w
		}
	}
	top = 0
	if gap := height - len(lines); gap > 0 {
		top = gap - int(math.Round(float64(gap)*0.5))
	}
	left = 0
	if gap := width - blockWidth; gap > 0 {
		left = gap - int(math.Round(float64(gap)*0.5))
	}
	return top, left
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
