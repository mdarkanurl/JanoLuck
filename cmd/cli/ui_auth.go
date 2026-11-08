package main

import (
	"context"
	"fmt"
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

func (m authModel) View() string {
	cardWidth := min(64, max(40, m.width-4))
	inner := cardWidth - 2

	var body strings.Builder
	body.WriteString(logoStyle.Render("JanoLuck"))
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render("your optimistic friend for what-ifs"))
	body.WriteString("\n\n")

	switch m.mode {
	case authLogin:
		body.WriteString(titleStyle.Render("Welcome back"))
		body.WriteString("\n")
		body.WriteString(mutedStyle.Render("Sign in to pick up where you left off."))
		body.WriteString("\n\n")
		body.WriteString(m.renderField("Email", m.email, m.focus == 0, inner))
		body.WriteString("\n")
		body.WriteString(m.renderField("Password", m.password, m.focus == 1, inner))

	case authSignup:
		body.WriteString(titleStyle.Render("Create your account"))
		body.WriteString("\n")
		body.WriteString(mutedStyle.Render("We'll email you a 6-digit verification code."))
		body.WriteString("\n\n")
		body.WriteString(m.renderField("Email", m.email, m.focus == 0, inner))
		body.WriteString("\n")
		body.WriteString(m.renderField("Password", m.password, m.focus == 1, inner))
		body.WriteString("\n")
		body.WriteString(m.renderField("Confirm password", m.confirm, m.focus == 2, inner))

	case authVerify:
		body.WriteString(titleStyle.Render("Check your email"))
		body.WriteString("\n")
		body.WriteString(mutedStyle.Render("Enter the code sent to " + truncate(m.signupEmail, inner-4)))
		body.WriteString("\n\n")
		body.WriteString(m.renderField("Verification code", m.code, true, inner))
	}

	body.WriteString("\n\n")

	if m.notice != "" {
		body.WriteString(successStyle.Render(wrapText(m.notice, inner)))
		body.WriteString("\n")
	}
	if m.err != "" {
		body.WriteString(dangerStyle.Render(wrapText(m.err, inner)))
		body.WriteString("\n")
	}
	if m.loading {
		body.WriteString(m.spinner.View() + " " + mutedStyle.Render("One moment..."))
		body.WriteString("\n")
	}

	body.WriteString("\n")
	body.WriteString(m.footer())
	body.WriteString("\n\n")
	body.WriteString(mutedStyle.Render("server: " + truncate(m.apiURL, inner-10)))

	block := lipgloss.NewStyle().
		Width(cardWidth).
		Render(body.String())

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
