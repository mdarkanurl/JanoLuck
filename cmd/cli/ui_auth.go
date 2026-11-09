package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type authMode int

const (
	authLogin authMode = iota
	authSignup
	authVerify
)

type authModel struct {
	client *client
	apiURL string

	width  int
	height int

	mode  authMode
	focus int

	email    textinput.Model
	password textinput.Model
	confirm  textinput.Model
	code     textinput.Model

	spinner spinner.Model
	loading bool

	signupEmail string
	notice      string
	err         string
}

type loginResultMsg struct {
	ok  bool
	err error
}

type signupResultMsg struct {
	email string
	err   error
}

type verifyResultMsg struct {
	ok  bool
	err error
}

func newAuthModel(c *client, apiURL string, width, height int) authModel {
	email := textinput.New()
	email.Placeholder = "you@example.com"
	email.CharLimit = 254
	email.Prompt = "› "

	password := textinput.New()
	password.Placeholder = "your password"
	password.CharLimit = 128
	password.EchoMode = textinput.EchoPassword
	password.Prompt = "› "

	confirm := textinput.New()
	confirm.Placeholder = "repeat your password"
	confirm.CharLimit = 128
	confirm.EchoMode = textinput.EchoPassword
	confirm.Prompt = "› "

	code := textinput.New()
	code.Placeholder = "123456"
	code.CharLimit = 6
	code.Prompt = "› "

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = primaryStyle

	if c.creds.Email != "" {
		email.SetValue(c.creds.Email)
		email.CursorEnd()
	}

	model := authModel{
		client:   c,
		apiURL:   apiURL,
		width:    width,
		height:   height,
		mode:     authLogin,
		email:    email,
		password: password,
		confirm:  confirm,
		code:     code,
		spinner:  spin,
	}

	model.setSize(width, height)
	model.focusField(0)

	return model
}

func (m authModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *authModel) setSize(width, height int) {
	m.width = width
	m.height = height

	cardWidth := min(64, max(40, width-4))
	inner := cardWidth - 2
	inputWidth := max(12, inner-6)

	m.email.Width = inputWidth
	m.password.Width = inputWidth
	m.confirm.Width = inputWidth
	m.code.Width = inputWidth
}

func (m authModel) Update(msg tea.Msg) (authModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case loginResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}
		return m, func() tea.Msg { return authenticatedMsg{} }

	case signupResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}
		m.mode = authVerify
		m.signupEmail = strings.ToLower(strings.TrimSpace(msg.email))
		m.notice = "We sent a 6-digit code to " + m.signupEmail
		m.err = ""
		m.focus = 0
		m.code.SetValue("")
		cmd := m.focusField(0)
		return m, cmd

	case verifyResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}
		return m, func() tea.Msg { return authenticatedMsg{} }

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+n":
			if m.mode != authVerify {
				m.mode = authSignup
				m.err = ""
				m.notice = ""
				return m, m.focusField(0)
			}
		case "ctrl+l":
			if m.mode != authVerify {
				m.mode = authLogin
				m.err = ""
				m.notice = ""
				return m, m.focusField(0)
			}
		case "tab":
			return m, m.cycleFocus(1)
		case "shift+tab":
			return m, m.cycleFocus(-1)
		case "enter":
			return m.submit()
		case "esc":
			if m.mode != authLogin {
				m.mode = authLogin
				m.err = ""
				m.notice = ""
				return m, m.focusField(0)
			}
		}
	}

	return m.updateFocused(msg)
}

func (m *authModel) fields() []*textinput.Model {
	switch m.mode {
	case authLogin:
		return []*textinput.Model{&m.email, &m.password}
	case authSignup:
		return []*textinput.Model{&m.email, &m.password, &m.confirm}
	case authVerify:
		return []*textinput.Model{&m.code}
	}
	return nil
}

func (m *authModel) focusField(index int) tea.Cmd {
	fields := m.fields()
	if len(fields) == 0 {
		return nil
	}
	if index < 0 {
		index = len(fields) - 1
	}
	index %= len(fields)
	m.focus = index

	for i, field := range fields {
		if i == index {
			field.Focus()
		} else {
			field.Blur()
		}
	}
	return nil
}

func (m *authModel) cycleFocus(delta int) tea.Cmd {
	return m.focusField(m.focus + delta)
}

// handleMouse focuses the field under the pointer, so the form can be navigated
// with the mouse as well as with tab/shift+tab.
func (m authModel) handleMouse(msg tea.MouseMsg) (authModel, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	box, cell, ok := m.fieldAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}

	cmd := m.focusField(box.index)
	m.setCursorAt(box.index, cell)
	return m, cmd
}

// fieldAt maps a screen cell to the field rendered under it, returning the box
// and the click's column within it. It mirrors how View centres the card.
func (m authModel) fieldAt(x, y int) (authFieldBox, int, bool) {
	block, boxes := m.layout()
	if len(boxes) == 0 {
		return authFieldBox{}, 0, false
	}

	lines := strings.Split(block, "\n")

	// lipgloss.Place centres the block by padding the smaller side, and pads
	// each line by its own width, so the remainder goes to the left.
	top := 0
	if gap := m.height - len(lines); gap > 0 {
		top = gap - int(math.Round(float64(gap)*0.5))
	}

	for _, box := range boxes {
		relY := y - top
		if relY < box.labelLine || relY > box.bottom || relY >= len(lines) {
			continue
		}

		lineWidth := lipgloss.Width(lines[relY])
		left := 0
		if gap := m.width - lineWidth; gap > 0 {
			left = gap - int(math.Round(float64(gap)*0.5))
		}

		if relX := x - left; relX >= 0 && relX < box.width {
			return box, relX, true
		}
	}
	return authFieldBox{}, 0, false
}

// setCursorAt puts the text caret at the column that was clicked.
func (m *authModel) setCursorAt(index int, boxCell int) {
	fields := m.fields()
	if index < 0 || index >= len(fields) {
		return
	}
	field := fields[index]

	// Column where the text starts: border + padding + prompt.
	textCell := boxCell - (1 + 1 + lipgloss.Width(field.Prompt))
	if textCell < 0 {
		return // clicked the border or prompt: focus only
	}

	pos := 0
	used := 0
	for _, char := range field.Value() {
		width := lipgloss.Width(string(char))
		if used+width > textCell {
			break
		}
		used += width
		pos++
	}
	field.SetCursor(pos)
}

func (m *authModel) updateFocused(msg tea.Msg) (authModel, tea.Cmd) {
	fields := m.fields()
	if len(fields) == 0 {
		return *m, nil
	}

	field := fields[m.focus]
	updated, cmd := field.Update(msg)
	*field = updated
	return *m, cmd
}

func (m authModel) submit() (authModel, tea.Cmd) {
	if m.loading {
		return m, nil
	}

	m.err = ""
	m.notice = ""

	switch m.mode {
	case authLogin:
		email := strings.ToLower(strings.TrimSpace(m.email.Value()))
		password := m.password.Value()
		if !looksLikeEmail(email) {
			m.err = "Enter a valid email address."
			return m, nil
		}
		if password == "" {
			m.err = "Enter your password."
			return m, nil
		}

		m.loading = true
		return m, tea.Batch(loginCmd(m.client, email, password), m.spinner.Tick)

	case authSignup:
		email := strings.ToLower(strings.TrimSpace(m.email.Value()))
		password := m.password.Value()
		if !looksLikeEmail(email) {
			m.err = "Enter a valid email address."
			return m, nil
		}
		if !strongEnough(password) {
			m.err = "Use at least 8 characters with a letter and a number."
			return m, nil
		}
		if password != m.confirm.Value() {
			m.err = "Passwords do not match."
			return m, nil
		}

		m.loading = true
		return m, tea.Batch(signupCmd(m.client, email, password), m.spinner.Tick)

	case authVerify:
		code := strings.TrimSpace(m.code.Value())
		if len(code) != 6 || !allDigits(code) {
			m.err = "Enter the 6-digit code from your email."
			return m, nil
		}

		m.loading = true
		return m, tea.Batch(verifyCmd(m.client, m.signupEmail, code), m.spinner.Tick)
	}

	return m, nil
}

// authFieldBox is where a field's label and bordered input sit inside the
// rendered auth card. Coordinates are relative to the card block.
type authFieldBox struct {
	index     int
	labelLine int // line index of the field's label
	top       int // line index of the border's top edge
	bottom    int // line index of the border's bottom edge (inclusive)
	width     int // rendered width of the bordered box
}

// layout renders the auth card and reports where each field sits in it, so a
// mouse click can be mapped back to the field under the pointer.
func (m authModel) layout() (string, []authFieldBox) {
	cardWidth := min(64, max(40, m.width-4))
	inner := cardWidth - 2

	var body strings.Builder
	lines := 0 // finished lines == index of the line currently being written
	write := func(s string) {
		body.WriteString(s)
		lines += strings.Count(s, "\n")
	}

	boxes := make([]authFieldBox, 0, 3)
	field := func(index int, label string, input textinput.Model, focused bool) {
		labelLine := lines
		rendered := m.renderField(label, input, focused, inner)
		write(rendered)

		boxLines := strings.Split(rendered, "\n")
		boxes = append(boxes, authFieldBox{
			index:     index,
			labelLine: labelLine,
			top:       labelLine + 1,
			bottom:    labelLine + len(boxLines) - 1,
			width:     lipgloss.Width(boxLines[len(boxLines)-1]),
		})
	}

	write(logoStyle.Render("JanoLuck") + "\n")
	write(mutedStyle.Render("your optimistic friend for what-ifs") + "\n\n")

	switch m.mode {
	case authLogin:
		write(titleStyle.Render("Welcome back") + "\n")
		write(mutedStyle.Render("Sign in to pick up where you left off.") + "\n\n")
		field(0, "Email", m.email, m.focus == 0)
		write("\n")
		field(1, "Password", m.password, m.focus == 1)

	case authSignup:
		write(titleStyle.Render("Create your account") + "\n")
		write(mutedStyle.Render("We'll email you a 6-digit verification code.") + "\n\n")
		field(0, "Email", m.email, m.focus == 0)
		write("\n")
		field(1, "Password", m.password, m.focus == 1)
		write("\n")
		field(2, "Confirm password", m.confirm, m.focus == 2)

	case authVerify:
		write(titleStyle.Render("Check your email") + "\n")
		write(mutedStyle.Render("Enter the code sent to " + truncate(m.signupEmail, inner-4)) + "\n\n")
		field(0, "Verification code", m.code, true)
	}

	write("\n\n")

	if m.notice != "" {
		write(successStyle.Render(wrapText(m.notice, inner)) + "\n")
	}
	if m.err != "" {
		write(dangerStyle.Render(wrapText(m.err, inner)) + "\n")
	}
	if m.loading {
		write(m.spinner.View() + " " + mutedStyle.Render("One moment...") + "\n")
	}

	write("\n")
	write(m.footer() + "\n\n")
	write(mutedStyle.Render("server: " + truncate(m.apiURL, inner-10)))

	block := lipgloss.NewStyle().
		Width(cardWidth).
		Render(body.String())

	return block, boxes
}

func (m authModel) View() string {
	block, _ := m.layout()

	return centeredCard(m.width, m.height, block)
}

func (m authModel) renderField(label string, input textinput.Model, focused bool, width int) string {
	labelStyle := mutedStyle
	if focused {
		labelStyle = accentStyle
	}
	return labelStyle.Render(label) + "\n" + fieldBorder(focused, width).Render(input.View())
}

func (m authModel) footer() string {
	key := func(k string) string { return accentStyle.Render(k) }

	switch m.mode {
	case authLogin:
		return key("[enter]") + mutedStyle.Render(" log in    ") +
			key("[ctrl+n]") + mutedStyle.Render(" sign up    ") +
			key("[ctrl+c]") + mutedStyle.Render(" quit")
	case authSignup:
		return key("[enter]") + mutedStyle.Render(" sign up    ") +
			key("[tab]") + mutedStyle.Render(" next    ") +
			key("[esc]") + mutedStyle.Render(" back")
	case authVerify:
		return key("[enter]") + mutedStyle.Render(" verify    ") +
			key("[esc]") + mutedStyle.Render(" back")
	}
	return ""
}

func loginCmd(c *client, email, password string) tea.Cmd {
	return func() tea.Msg {
		tokens, err := c.login(context.Background(), email, password)
		if err != nil {
			return loginResultMsg{err: err}
		}
		if err := c.saveTokens(email, tokens); err != nil {
			return loginResultMsg{err: err}
		}
		return loginResultMsg{ok: true}
	}
}

func signupCmd(c *client, email, password string) tea.Cmd {
	return func() tea.Msg {
		if err := c.signup(context.Background(), email, password); err != nil {
			return signupResultMsg{err: err}
		}
		return signupResultMsg{email: email}
	}
}

func verifyCmd(c *client, email, code string) tea.Cmd {
	return func() tea.Msg {
		value, err := parseVerificationCode(code)
		if err != nil {
			return verifyResultMsg{err: err}
		}

		tokens, err := c.verifyEmail(context.Background(), email, value)
		if err != nil {
			return verifyResultMsg{err: err}
		}
		if err := c.saveTokens(email, tokens); err != nil {
			return verifyResultMsg{err: err}
		}
		return verifyResultMsg{ok: true}
	}
}

func parseVerificationCode(value string) (int, error) {
	if len(value) != 6 || !allDigits(value) {
		return 0, fmt.Errorf("code must be 6 digits")
	}

	code := 0
	for _, char := range value {
		code = code*10 + int(char-'0')
	}
	return code, nil
}

func allDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func looksLikeEmail(value string) bool {
	if strings.ContainsAny(value, " \t\r\n") {
		return false
	}

	at := strings.LastIndexByte(value, '@')
	if at <= 0 || at == len(value)-1 || strings.Contains(value[:at], "@") {
		return false
	}

	domain := value[at+1:]
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

func strongEnough(value string) bool {
	if len([]rune(value)) < 8 {
		return false
	}

	var hasLetter, hasNumber bool
	for _, char := range value {
		switch {
		case unicode.IsLetter(char):
			hasLetter = true
		case unicode.IsNumber(char):
			hasNumber = true
		}
	}
	return hasLetter && hasNumber
}
