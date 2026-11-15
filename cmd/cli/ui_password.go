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
	saving bool
	err    string

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
	cardWidth := min(64, max(40, m.width-4))
	inner := cardWidth - 2

	var body strings.Builder
	body.WriteString(titleStyle.Render("Change password"))
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render("Enter your current password, then choose a new one."))
	body.WriteString("\n\n")

	body.WriteString(m.renderField("Current password", m.current, m.focus == 0, inner))
	body.WriteString("\n")
	body.WriteString(m.renderField("New password", m.next, m.focus == 1, inner))
	body.WriteString("\n")
	body.WriteString(m.renderField("Confirm new password", m.confirm, m.focus == 2, inner))
	body.WriteString("\n\n")

	if m.err != "" {
		body.WriteString(dangerStyle.Render(wrapText(m.err, inner)))
		body.WriteString("\n")
	}
	if m.saving {
		body.WriteString(m.spinner.View() + " " + mutedStyle.Render("Saving..."))
		body.WriteString("\n")
	}

	body.WriteString("\n")
	body.WriteString(accentStyle.Render("[enter]") + mutedStyle.Render(" save    "))
	body.WriteString(accentStyle.Render("[tab]") + mutedStyle.Render(" next    "))
	body.WriteString(accentStyle.Render("[esc]") + mutedStyle.Render(" back"))

	block := lipgloss.NewStyle().
		Width(cardWidth).
		Render(body.String())

	return centeredCard(m.width, m.height, block)
}

func (m passwordModel) renderField(label string, input textinput.Model, focused bool, width int) string {
	labelStyle := mutedStyle
	if focused {
		labelStyle = accentStyle
	}
	return labelStyle.Render(truncate(label, width)) + "\n" + fieldBorder(focused, width).Render(input.View())
}

func changePasswordCmd(c *client, current, next string) tea.Cmd {
	return func() tea.Msg {
		err := c.changePassword(context.Background(), current, next)
		return passwordResultMsg{err: err}
	}
}
