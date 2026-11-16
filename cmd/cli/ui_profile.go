package main

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	hover  int
	saving bool
	err    string

	footerHover string

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
	inner := cardWidth - 2
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

	case tea.MouseMsg:
		return m.handleMouse(msg)

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
	screen, _ := m.profileLayout()
	return screen
}

func (m profileModel) profileLayout() (string, clickMap) {
	cardWidth := min(64, max(40, m.width-4))
	inner := cardWidth - 2

	b := &layoutBuilder{}

	field := func(index int, label string, input textinput.Model, focused bool) {
		start := b.line
		rendered := m.renderField(label, input, focused, inner, index == m.hover)
		b.write(rendered + "\n")

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

	b.write(logoStyle.Render("JanoLuck") + "\n")
	b.write(mutedStyle.Render(truncate(m.me.Email, inner)) + "\n\n")

	if m.existing {
		b.write(titleStyle.Render("Edit your profile") + "\n")
		b.write(mutedStyle.Render("Jano uses this to make its guesses feel personal.") + "\n")
	} else {
		b.write(titleStyle.Render("Tell Jano about you") + "\n")
		b.write(mutedStyle.Render("A little context makes the what-ifs better.") + "\n")
	}
	b.write("\n")

	field(0, "Name", m.name, m.focus == 0)
	b.write("\n")
	field(1, questionOne, m.qOne, m.focus == 1)
	b.write("\n")
	field(2, questionTwo, m.qTwo, m.focus == 2)
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

func (m profileModel) handleMouse(msg tea.MouseMsg) (profileModel, tea.Cmd) {
	_, targets := m.profileLayout()
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

func (m profileModel) renderField(label string, input textinput.Model, focused bool, width int, hovered bool) string {
	labelStyle := mutedStyle
	if focused {
		labelStyle = accentStyle
	}
	if hovered && !focused {
		return hoverField(label, input.View(), width)
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
