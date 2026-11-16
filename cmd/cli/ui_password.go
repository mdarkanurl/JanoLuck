package main

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type openPasswordMsg struct{}

type passwordChangedMsg struct{}

type passwordModel struct {
	client *client

	width  int
	height int

	current textinput.Model
	next    textinput.Model
	confirm textinput.Model

	focus  int
	hover  int
	saving bool
	err    string

	footerHover string

	spinner spinner.Model
}

type passwordResultMsg struct {
	err error
}

func newPasswordModel(c *client, width, height int) passwordModel {
	current := textinput.New()
	current.Placeholder = "your current password"
	current.CharLimit = 128
	current.EchoMode = textinput.EchoPassword
	current.Prompt = "› "

	next := textinput.New()
	next.Placeholder = "at least 8 characters with a letter and a number"
	next.CharLimit = 128
	next.EchoMode = textinput.EchoPassword
	next.Prompt = "› "

	confirm := textinput.New()
	confirm.Placeholder = "repeat your new password"
	confirm.CharLimit = 128
	confirm.EchoMode = textinput.EchoPassword
	confirm.Prompt = "› "

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = primaryStyle

	model := passwordModel{
		client:  c,
		current: current,
		next:    next,
		confirm: confirm,
		spinner: spin,
	}

	model.setSize(width, height)
	model.focusField(0)

	return model
}

func (m passwordModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *passwordModel) setSize(width, height int) {
	m.width = width
	m.height = height

	cardWidth := min(64, max(40, width-4))
	inputWidth := max(12, cardWidth-2-6)

	m.current.Width = inputWidth
	m.next.Width = inputWidth
	m.confirm.Width = inputWidth
}

func (m *passwordModel) fields() []*textinput.Model {
	return []*textinput.Model{&m.current, &m.next, &m.confirm}
}

func (m *passwordModel) focusField(index int) tea.Cmd {
	fields := m.fields()
	if index < 0 {
		index = len(fields) - 1
	}
	m.focus = index % len(fields)

	for i, field := range fields {
		if i == m.focus {
			field.Focus()
		} else {
			field.Blur()
		}
	}
	return nil
}

func (m passwordModel) passwordLayout() (string, clickMap) {
	cardWidth := min(64, max(40, m.width-4))
	inner := cardWidth - 2

	b := &layoutBuilder{}

	field := func(index int, label string, input textinput.Model, focused bool) {
		start := b.line
		rendered := m.renderField(label, input, focused, inner, index == m.hover)
		b.write(rendered)

		boxLines := strings.Split(rendered, "\n")
		b.add(clickTarget{
			action: "field",
			index:  index,
			x:      0,
			y:      start,
			width:  lipgloss.Width(boxLines[len(boxLines)-1]),
			height: len(boxLines),
		})
	}

	b.write(titleStyle.Render("Change password") + "\n")
	b.write(mutedStyle.Render("Enter your current password, then choose a new one.") + "\n\n")

	field(0, "Current password", m.current, m.focus == 0)
	b.write("\n")
	field(1, "New password", m.next, m.focus == 1)
	b.write("\n")
	field(2, "Confirm new password", m.confirm, m.focus == 2)
	b.write("\n\n")

	if m.err != "" {
		b.write(dangerStyle.Render(wrapText(m.err, inner)) + "\n")
	}
	if m.saving {
		b.write(m.spinner.View() + " " + mutedStyle.Render("Saving...") + "\n")
	}

	b.write("\n")
	b.addActions([]actionItem{
		{key: "enter", label: "save", action: "save"},
		{key: "tab", label: "next", action: "next"},
		{key: "esc", label: "back", action: "back"},
	}, m.footerHover, inner, 0)

	block := padLines(strings.TrimSuffix(b.String(), "\n"), cardWidth)
	screen, ox, oy := centeredBlock(m.width, m.height, block)

	return screen, b.targets.shift(ox, oy)
}

func (m passwordModel) handlePasswordMouse(msg tea.MouseMsg) (passwordModel, tea.Cmd) {
	_, targets := m.passwordLayout()
	target, ok := targets.at(msg.X, msg.Y)

	switch msg.Action {
	case tea.MouseActionMotion:
		m.hover = -1
		m.footerHover = ""
		if ok {
			if target.action == "field" {
				m.hover = target.index
			} else {
				m.footerHover = target.action
			}
			setPointerShape(target.pointerShape())
		} else {
			resetPointerShape()
		}
		return m, nil
	case tea.MouseActionRelease:
		m.hover = -1
		m.footerHover = ""
		return m, nil
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft || !ok {
		return m, nil
	}

	switch target.action {
	case "field":
		return m, m.focusField(target.index)
	case "save":
		return m.save()
	case "next":
		return m, m.focusField(m.focus + 1)
	case "back":
		return m, func() tea.Msg { return backToChatMsg{} }
	}

	return m, nil
}

func (m passwordModel) Update(msg tea.Msg) (passwordModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.saving {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case passwordResultMsg:
		m.saving = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}
		return m, func() tea.Msg { return passwordChangedMsg{} }

	case tea.MouseMsg:
		return m.handlePasswordMouse(msg)

	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down":
			return m, m.focusField(m.focus + 1)
		case "shift+tab", "up":
			return m, m.focusField(m.focus - 1)
		case "enter":
			return m.save()
		}
	}

	if m.saving {
		return m, nil
	}

	field := m.fields()[m.focus]
	updated, cmd := field.Update(msg)
	*field = updated
	return m, cmd
}

func (m passwordModel) save() (passwordModel, tea.Cmd) {
	if m.saving {
		return m, nil
	}

	m.err = ""

	current := m.current.Value()
	next := m.next.Value()

	switch {
	case current == "":
		m.err = "Enter your current password."
		return m, nil
	case len([]rune(current)) < 8:
		m.err = "Your current password is at least 8 characters."
		return m, nil
	case !strongEnough(next):
		m.err = "Use at least 8 characters with a letter and a number."
		return m, nil
	case next != m.confirm.Value():
		m.err = "Passwords do not match."
		return m, nil
	}

	m.saving = true

	return m, tea.Batch(changePasswordCmd(m.client, current, next), m.spinner.Tick)
}

func (m passwordModel) View() string {
	screen, _ := m.passwordLayout()
	return screen
}

func (m passwordModel) renderField(label string, input textinput.Model, focused bool, width int, hovered bool) string {
	labelStyle := mutedStyle
	if focused {
		labelStyle = accentStyle
	}
	if hovered && !focused {
		return hoverField(label, input.View(), width)
	}
	return labelStyle.Render(truncate(label, width)) + "\n" + fieldBorder(focused, width).Render(input.View())
}

func changePasswordCmd(c *client, current, next string) tea.Cmd {
	return func() tea.Msg {
		err := c.changePassword(context.Background(), current, next)
		return passwordResultMsg{err: err}
	}
}
