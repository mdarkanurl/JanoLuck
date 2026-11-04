package main

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type profileModel struct {
	client *client
	me     meData

	width  int
	height int

	name textinput.Model
	qOne textinput.Model
	qTwo textinput.Model

	existing bool

	focus  int
	saving bool
	err    string

	spinner spinner.Model
}

const (
	questionOne = "Where are you from?"
	questionTwo = "What do you do for living?"
)

type profileResultMsg struct {
	profile profileData
	err     error
}

func newProfileModel(c *client, me meData, profile *profileData, width, height int) profileModel {
	name := textinput.New()
	name.Placeholder = "What should Jano call you?"
	name.CharLimit = 120
	name.Prompt = "› "

	qOne := textinput.New()
	qOne.Placeholder = "A town, a city, a whole country..."
	qOne.CharLimit = 500
	qOne.Prompt = "› "

	qTwo := textinput.New()
	qTwo.Placeholder = "Student, engineer, dreamer..."
	qTwo.CharLimit = 500
	qTwo.Prompt = "› "

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = primaryStyle

	model := profileModel{
		client:  c,
		me:      me,
		name:    name,
		qOne:    qOne,
		qTwo:    qTwo,
		width:   width,
		height:  height,
		spinner: spin,
	}

	if profile != nil && profile.ID != "" {
		model.existing = true
		model.name.SetValue(profile.Name)
		model.name.CursorEnd()

		for _, question := range profile.Questions {
			switch question.Index {
			case 0:
				model.qOne.SetValue(question.Answer)
				model.qOne.CursorEnd()
			case 1:
				model.qTwo.SetValue(question.Answer)
				model.qTwo.CursorEnd()
			}
		}
	}

	model.setSize(width, height)
	model.focusField(0)

	return model
}

func (m profileModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *profileModel) setSize(width, height int) {
	m.width = width
	m.height = height

	cardWidth := min(64, max(40, width-4))
	inner := cardWidth - 8
	inputWidth := max(12, inner-6)

	m.name.Width = inputWidth
	m.qOne.Width = inputWidth
	m.qTwo.Width = inputWidth
}

func (m *profileModel) fields() []*textinput.Model {
	return []*textinput.Model{&m.name, &m.qOne, &m.qTwo}
}

func (m *profileModel) focusField(index int) tea.Cmd {
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

func (m profileModel) Update(msg tea.Msg) (profileModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.saving {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case profileResultMsg:
		m.saving = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}
		return m, func() tea.Msg { return profileSavedMsg{profile: msg.profile} }

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

func (m profileModel) save() (profileModel, tea.Cmd) {
	if m.saving {
		return m, nil
	}

	m.err = ""

	name := strings.TrimSpace(m.name.Value())
	qOne := strings.TrimSpace(m.qOne.Value())
	qTwo := strings.TrimSpace(m.qTwo.Value())

	if name == "" {
		m.err = "Tell Jano your name."
		return m, nil
	}

	m.saving = true
	existing := m.existing

	return m, tea.Batch(saveProfileCmd(m.client, existing, name, qOne, qTwo), m.spinner.Tick)
}

func (m profileModel) View() string {
	cardWidth := min(64, max(40, m.width-4))
	inner := cardWidth - 8

	var body strings.Builder
	body.WriteString(logoStyle.Render("✦ JanoLuck"))
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render(truncate(m.me.Email, inner)))
	body.WriteString("\n\n")

	if m.existing {
		body.WriteString(titleStyle.Render("Edit your profile"))
		body.WriteString("\n")
		body.WriteString(mutedStyle.Render("Jano uses this to make its guesses feel personal."))
	} else {
		body.WriteString(titleStyle.Render("Tell Jano about you"))
		body.WriteString("\n")
		body.WriteString(mutedStyle.Render("A little context makes the what-ifs better."))
	}
	body.WriteString("\n\n")

	body.WriteString(m.renderField("Name", m.name, m.focus == 0, inner))
	body.WriteString("\n")
	body.WriteString(m.renderField(questionOne, m.qOne, m.focus == 1, inner))
	body.WriteString("\n")
	body.WriteString(m.renderField(questionTwo, m.qTwo, m.focus == 2, inner))
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

	return centeredCard(m.width, m.height, cardStyle(cardWidth).Render(body.String()))
}

func (m profileModel) renderField(label string, input textinput.Model, focused bool, width int) string {
	labelStyle := mutedStyle
	if focused {
		labelStyle = accentStyle
	}
	return labelStyle.Render(truncate(label, width)) + "\n" + fieldBorder(focused, width).Render(input.View())
}

func saveProfileCmd(c *client, existing bool, name, qOne, qTwo string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		if !existing {
			profile, err := c.createProfile(ctx, name, qOne, qTwo)
			return profileResultMsg{profile: profile, err: err}
		}

		req := updateProfileRequest{
			Name: &name,
			Questions: []questionAnswerUpdate{
				{Index: 0, Answer: qOne},
				{Index: 1, Answer: qTwo},
			},
		}
		profile, err := c.updateProfile(ctx, req)
		return profileResultMsg{profile: profile, err: err}
	}
}
