package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type chatFocus int

const (
	focusInput chatFocus = iota
	focusSessions
)

type chatConfirm struct {
	action    string
	sessionID string
	name      string
}

type chatModel struct {
	client  *client
	me      meData
	profile *profileData

	sessions []sessionData
	current  *sessionData
	chats    []chatData

	width        int
	height       int
	sidebarWidth int
	mainWidth    int

	focus    chatFocus
	selected int

	loading     bool
	loadingText string
	err         string
	toast       string

	viewport viewport.Model
	input    textarea.Model
	spinner  spinner.Model

	confirm *chatConfirm
}

type sessionsLoadedMsg struct {
	sessions []sessionData
	err      error
}

type sessionOpenedMsg struct {
	session sessionData
	chats   []chatData
	err     error
}

type sessionCreatedMsg struct {
	session sessionData
	pending string
	err     error
}

type chatSentMsg struct {
	prediction chatData
	session    *sessionData
	sessions   []sessionData
	err        error
}

type refreshResultMsg struct {
	sessions []sessionData
	chats    []chatData
	err      error
}

type sessionDeletedMsg struct {
	id  string
	err error
}

func newChatModel(
	c *client,
	me meData,
	profile *profileData,
	sessions []sessionData,
	current *sessionData,
	chats []chatData,
	width, height int,
) chatModel {
	input := textarea.New()
	input.Placeholder = "Ask Jano anything..."
	input.CharLimit = 8000
	input.ShowLineNumbers = false
	input.Prompt = "› "

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = accentStyle

	model := chatModel{
		client:   c,
		me:       me,
		profile:  profile,
		sessions: sessions,
		current:  current,
		chats:    chronological(chats),
		viewport: viewport.New(10, 5),
		input:    input,
		spinner:  spin,
	}
	model.viewport.MouseWheelEnabled = true
	model.input.Focus()
	model.focus = focusInput

	if current != nil {
		model.selected = model.indexOfSession(current.ID)
	}
	if model.selected < 0 {
		model.selected = 0
	}

	model.setSize(width, height)
	model.renderMessages()

	return model
}

func (m chatModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m *chatModel) setSize(width, height int) {
	m.width = width
	m.height = height

	if m.client == nil {
		return
	}

	m.sidebarWidth = 0
	if width >= 82 {
		m.sidebarWidth = min(32, max(26, width/4))
	}

	m.mainWidth = width
	if m.sidebarWidth > 0 {
		m.mainWidth = width - m.sidebarWidth - 1
	}

	bodyHeight := max(10, height-1)
	chatHeight := max(3, bodyHeight-2-1-5)

	m.viewport.Width = max(10, m.mainWidth-2)
	m.viewport.Height = chatHeight

	m.input.SetWidth(max(10, m.mainWidth-6))
	m.input.SetHeight(3)

	m.renderMessages()
}

func (m chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case sessionOpenedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}

		m.current = &msg.session
		m.chats = chronological(msg.chats)
		m.selected = max(0, m.indexOfSession(msg.session.ID))
		m.err = ""
		m.toast = ""
		_ = m.client.setSession(msg.session.ID)
		m.focusInput()
		m.renderMessages()
		return m, nil

	case sessionCreatedMsg:
		if msg.err != nil {
			m.loading = false
			m.err = inline(msg.err.Error())
			m.chats = append(m.chats, chatData{
				Role:      "error",
				Content:   msg.err.Error(),
				CreatedAt: time.Now().Format(time.RFC3339),
			})
			m.renderMessages()
			return m, nil
		}

		m.current = &msg.session
		m.sessions = m.upsertSession(msg.session)
		m.selected = max(0, m.indexOfSession(msg.session.ID))
		_ = m.client.setSession(msg.session.ID)
		m.loadingText = "Jano is thinking..."
		m.renderMessages()

		return m, tea.Batch(sendChatCmd(m.client, msg.session, msg.pending), m.spinner.Tick)

	case chatSentMsg:
		m.loading = false
		m.err = ""

		if msg.session != nil {
			m.current = msg.session
			m.sessions = m.upsertSession(*msg.session)
			_ = m.client.setSession(msg.session.ID)
		}
		if msg.sessions != nil {
			m.sessions = msg.sessions
		}
		if m.current != nil {
			m.selected = max(0, m.indexOfSession(m.current.ID))
		}

		if msg.err != nil {
			if errors.Is(msg.err, errSessionExpired) || errors.Is(msg.err, errNotLoggedIn) {
				return m, func() tea.Msg { return sessionExpiredMsg{err: msg.err} }
			}

			m.err = inline(msg.err.Error())
			m.chats = append(m.chats, chatData{
				Role:      "error",
				Content:   msg.err.Error(),
				CreatedAt: time.Now().Format(time.RFC3339),
			})
			m.renderMessages()
			return m, nil
		}

		m.chats = append(m.chats, msg.prediction)
		m.renderMessages()
		return m, nil

	case refreshResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}

		m.sessions = msg.sessions
		if m.current != nil {
			m.chats = chronological(msg.chats)
			m.selected = max(0, m.indexOfSession(m.current.ID))
		}
		m.err = ""
		m.toast = "Refreshed."
		m.renderMessages()
		return m, nil

	case sessionDeletedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}

		m.sessions = removeSession(m.sessions, msg.id)
		if m.current != nil && m.current.ID == msg.id {
			m.current = nil
			m.chats = nil
			_ = m.client.setSession("")
			m.renderMessages()
		}
		if m.selected >= len(m.sessions) {
			m.selected = max(0, len(m.sessions)-1)
		}
		m.toast = "Conversation deleted."
		return m, nil

	case sessionsLoadedMsg:
		if msg.err != nil {
			m.err = inline(msg.err.Error())
			return m, nil
		}
		m.sessions = msg.sessions
		if m.selected >= len(m.sessions) {
			m.selected = max(0, len(m.sessions)-1)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m chatModel) handleKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.confirm != nil {
		switch msg.String() {
		case "y", "Y", "enter":
			return m.runConfirm()
		case "n", "N", "esc":
			m.confirm = nil
			return m, nil
		}
		return m, nil
	}

	m.toast = ""

	switch msg.String() {
	case "ctrl+n":
		return m.newConversation()
	case "ctrl+r":
		return m.refresh()
	case "ctrl+l":
		m.confirm = &chatConfirm{action: "logout"}
		return m, nil
	case "tab":
		if m.sidebarWidth == 0 {
			return m, nil
		}
		if m.focus == focusInput {
			m.focusSessions()
		} else {
			m.focusInput()
		}
		return m, nil
	case "esc":
		m.focusInput()
		return m, nil
	}

	if m.focus == focusSessions {
		return m.handleSidebarKey(msg)
	}

	switch msg.String() {
	case "enter":
		return m.submit()
	case "ctrl+j":
		m.input.InsertString("\n")
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m chatModel) handleSidebarKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil
	case "down", "j":
		if m.selected < len(m.sessions)-1 {
			m.selected++
		}
		return m, nil
	case "enter":
		return m.openSelected()
	case "n":
		return m.newConversation()
	case "d":
		if len(m.sessions) == 0 {
			return m, nil
		}
		session := m.sessions[m.selected]
		m.confirm = &chatConfirm{
			action:    "delete",
			sessionID: session.ID,
			name:      session.Name,
		}
		return m, nil
	case "r":
		return m.refresh()
	}
	return m, nil
}

func (m chatModel) submit() (chatModel, tea.Cmd) {
	if m.loading {
		return m, nil
	}

	value := strings.TrimSpace(m.input.Value())
	if value == "" {
		return m, nil
	}

	m.input.Reset()
	m.input.Focus()
	m.err = ""
	m.toast = ""
	m.loading = true

	m.chats = append(m.chats, chatData{
		Role:      "user",
		Content:   value,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	m.renderMessages()

	if m.current == nil {
		m.loadingText = "Starting a new conversation..."
		return m, tea.Batch(createSessionCmd(m.client, value), m.spinner.Tick)
	}

	m.loadingText = "Jano is thinking..."
	return m, tea.Batch(sendChatCmd(m.client, *m.current, value), m.spinner.Tick)
}

func (m chatModel) openSelected() (chatModel, tea.Cmd) {
	if m.loading || len(m.sessions) == 0 {
		return m, nil
	}

	session := m.sessions[m.selected]
	m.loading = true
	m.loadingText = "Loading conversation..."
	m.err = ""
	m.toast = ""

	return m, tea.Batch(loadSessionCmd(m.client, session.ID), m.spinner.Tick)
}

func (m chatModel) newConversation() (chatModel, tea.Cmd) {
	if m.loading {
		return m, nil
	}

	m.current = nil
	m.chats = nil
	m.err = ""
	m.loading = false
	m.toast = "New conversation. Your first message starts it."
	_ = m.client.setSession("")
	m.focusInput()
	m.renderMessages()

	return m, nil
}

func (m chatModel) refresh() (chatModel, tea.Cmd) {
	if m.loading {
		return m, nil
	}

	m.loading = true
	m.loadingText = "Refreshing..."
	m.err = ""

	return m, tea.Batch(refreshSessionsCmd(m.client, m.current), m.spinner.Tick)
}

func (m chatModel) runConfirm() (chatModel, tea.Cmd) {
	confirm := m.confirm
	m.confirm = nil
	if confirm == nil {
		return m, nil
	}

	switch confirm.action {
	case "delete":
		m.loading = true
		m.loadingText = "Deleting conversation..."
		return m, tea.Batch(deleteSessionCmd(m.client, confirm.sessionID), m.spinner.Tick)
	case "logout":
		m.loading = true
		m.loadingText = "Logging out..."
		return m, tea.Batch(logoutCmd(m.client), m.spinner.Tick)
	}

	return m, nil
}

func (m *chatModel) focusInput() {
	if m.sidebarWidth == 0 {
		m.focus = focusInput
	} else {
		m.focus = focusInput
	}
	m.input.Focus()
}

func (m *chatModel) focusSessions() {
	m.focus = focusSessions
	m.input.Blur()
}

func (m *chatModel) renderMessages() {
	if m.client == nil {
		return
	}

	width := max(18, m.mainWidth-2)

	if len(m.chats) == 0 {
		if m.current == nil {
			m.viewport.SetContent(m.welcome(width))
		} else {
			m.viewport.SetContent(mutedStyle.Render(wrapText("This conversation is empty. Say hi below.", width-4)))
		}
		m.viewport.GotoBottom()
		return
	}

	blocks := make([]string, 0, len(m.chats))
	for _, chat := range m.chats {
		blocks = append(blocks, m.renderChat(chat, width))
	}

	m.viewport.SetContent(strings.Join(blocks, "\n\n"))
	m.viewport.GotoBottom()
}

func (m chatModel) welcome(width int) string {
	copy := "Ask me about your career, relationships, a decision you're stuck on, " +
		"or whatever's on your mind. I'll give you a hopeful (and slightly goofy) " +
		"guess about what happens next."

	body := logoStyle.Render("✦ Hey, I'm Jano.") + "\n\n" +
		mutedStyle.Render(wrapText(copy, max(18, width-6))) + "\n\n" +
		mutedStyle.Render("Type below and press enter. ctrl+j adds a newline.")

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorHairline).
		Padding(1, 2).
		Width(max(18, min(width-2, 76)-6))

	return style.Render(body)
}

func (m chatModel) renderChat(chat chatData, width int) string {
	content := strings.TrimSpace(chat.Content)
	if content == "" {
		return ""
	}

	role := "jano"
	roleStyle := primaryStyle
	border := colorPrimary

	switch chat.Role {
	case "user":
		role = "you"
		roleStyle = accentStyle
		border = colorAccent
	case "error":
		role = "error"
		roleStyle = dangerStyle
		border = colorDanger
	}

	maxBubble := min(80, max(26, width*3/4))
	content = wrapText(content, maxBubble-4)

	bubble := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Render(content)

	label := roleStyle.Render(role)
	if clock := formatClock(chat.CreatedAt); clock != "" {
		label += " " + mutedStyle.Render(clock)
	}

	align := lipgloss.Left
	if chat.Role == "user" {
		align = lipgloss.Right
	}

	return lipgloss.NewStyle().
		Width(width).
		Align(align).
		Render(label + "\n" + bubble)
}

func (m chatModel) View() string {
	if m.confirm != nil {
		return m.confirmView()
	}
	if m.width == 0 || m.height == 0 || m.client == nil {
		return ""
	}

	bodyHeight := max(10, m.height-1)
	status := m.statusView()

	if m.sidebarWidth > 0 {
		sidebar := m.sidebarView(bodyHeight)
		main := m.mainView(bodyHeight)
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", main) + "\n" + status
	}

	return m.mainView(bodyHeight) + "\n" + status
}

func (m chatModel) mainView(height int) string {
	header := m.headerView()
	hint := m.hintView()
	input := m.inputView()

	viewportHeight := height - 2 - 1 - 5
	if viewportHeight < 3 {
		viewportHeight = 3
	}
	if m.viewport.Height != viewportHeight {
		m.viewport.Height = viewportHeight
		m.renderMessages()
	}

	parts := []string{
		header,
		m.viewport.View(),
		hint,
		input,
	}

	return strings.Join(parts, "\n")
}

func (m chatModel) headerView() string {
	left := logoStyle.Render("✦ JanoLuck")
	if m.current != nil {
		left += mutedStyle.Render("  /  ")
		left += titleStyle.Render(truncate(m.current.Name, max(8, m.mainWidth/2)))
	} else {
		left += mutedStyle.Render("  /  new conversation")
	}

	right := mutedStyle.Render(fmt.Sprintf("%d messages", len(m.chats)))
	line := spread(left, right, m.mainWidth)

	return line + "\n" + hairlineStyle.Render(strings.Repeat("─", max(1, m.mainWidth)))
}

func (m chatModel) hintView() string {
	width := max(10, m.mainWidth-2)

	switch {
	case m.err != "":
		return dangerStyle.Render(truncate("! "+inline(m.err), width))
	case m.loading:
		return m.spinner.View() + " " + mutedStyle.Render(truncate(m.loadingText, width-4))
	case m.toast != "":
		return successStyle.Render(truncate(m.toast, width))
	default:
		hint := "enter send · ctrl+j newline · tab sessions · ctrl+h help"
		if m.sidebarWidth == 0 {
			hint = "enter send · ctrl+j newline · ctrl+h help"
		}
		return mutedStyle.Render(truncate(hint, width))
	}
}

func (m chatModel) inputView() string {
	border := colorMuted
	if m.focus == focusInput {
		border = colorPrimary
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(10, m.mainWidth-4))

	return style.Render(m.input.View())
}

func (m chatModel) sidebarView(height int) string {
	innerWidth := max(10, m.sidebarWidth-4)
	contentHeight := max(6, height-2)
	listHeight := max(1, contentHeight-4)

	title := mutedStyle.Render("SESSIONS")
	if len(m.sessions) > 0 {
		title += " " + mutedStyle.Render(fmt.Sprintf("%d", len(m.sessions)))
	}

	var listLines []string
	if len(m.sessions) == 0 {
		listLines = []string{mutedStyle.Render("No conversations yet."), mutedStyle.Render("ctrl+n starts one.")}
	} else {
		offset := 0
		if m.selected >= listHeight {
			offset = m.selected - listHeight + 1
		}
		end := min(len(m.sessions), offset+listHeight)

		for i := offset; i < end; i++ {
			listLines = append(listLines, m.sessionLine(i, innerWidth))
		}
	}
	for len(listLines) < listHeight {
		listLines = append(listLines, "")
	}

	footer := mutedStyle.Render("tab composer · n new")
	if m.focus == focusSessions {
		footer = mutedStyle.Render("n new · d delete · r refresh")
	}
	if lipgloss.Width(footer) > innerWidth {
		footer = mutedStyle.Render(truncate("n new · d delete", innerWidth))
	}

	content := strings.Join([]string{
		title,
		"",
		strings.Join(listLines, "\n"),
		footer,
	}, "\n")

	border := colorMuted
	if m.focus == focusSessions {
		border = colorPrimary
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(max(12, m.sidebarWidth-4)).
		Height(max(4, height-2))

	return style.Render(content)
}

func (m chatModel) sessionLine(index, width int) string {
	session := m.sessions[index]

	marker := "  "
	if m.current != nil && m.current.ID == session.ID {
		marker = "● "
	}

	when := relativeTime(session.UpdatedAt)
	nameWidth := width - 2 - lipgloss.Width(when) - 1
	name := truncate(session.Name, max(4, nameWidth))
	line := marker + name

	if when != "" {
		gap := width - lipgloss.Width(line) - lipgloss.Width(when)
		if gap >= 1 {
			line += strings.Repeat(" ", gap) + when
		}
	}

	switch {
	case index == m.selected:
		return selectedStyle.Width(width).Render(line)
	case m.current != nil && m.current.ID == session.ID:
		return accentStyle.Render(line)
	default:
		return line
	}
}

func (m chatModel) statusView() string {
	left := accentStyle.Render("✦") + " " + mutedStyle.Render(truncate(m.me.Email, max(8, m.width/3)))
	right := mutedStyle.Render("ctrl+h help · ctrl+n new · ctrl+c quit")
	if m.sidebarWidth == 0 {
		right = mutedStyle.Render("ctrl+h help · ctrl+c quit")
	}
	return spread(" "+left, right+" ", m.width)
}

func (m chatModel) confirmView() string {
	width := min(58, max(38, m.width-4))
	inner := width - 8

	title := "Delete conversation?"
	body := "This removes the conversation and all of its messages."
	action := "[y] delete    [n] cancel"

	if m.confirm != nil && m.confirm.action == "logout" {
		title = "Log out of JanoLuck?"
		body = "You'll need to sign in again to keep chatting."
		action = "[y] log out    [n] cancel"
	}
	if m.confirm != nil && m.confirm.name != "" {
		body = "“" + truncate(m.confirm.name, max(10, inner-4)) + "” and all of its messages will be removed."
	}

	content := titleStyle.Render(title) + "\n\n" +
		mutedStyle.Render(wrapText(body, inner)) + "\n\n" +
		accentStyle.Render("[y]") + mutedStyle.Render(" confirm    ") +
		accentStyle.Render("[n]") + mutedStyle.Render(" cancel")
	_ = action

	return centeredCard(m.width, m.height, cardStyle(width).Render(content))
}

func (m chatModel) indexOfSession(id string) int {
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			return i
		}
	}
	return -1
}

func (m chatModel) upsertSession(session sessionData) []sessionData {
	for i := range m.sessions {
		if m.sessions[i].ID == session.ID {
			m.sessions[i] = session
			return m.sessions
		}
	}
	return append([]sessionData{session}, m.sessions...)
}

func chronological(chats []chatData) []chatData {
	reversed := make([]chatData, len(chats))
	for i := range chats {
		reversed[i] = chats[len(chats)-1-i]
	}
	return reversed
}

func removeSession(sessions []sessionData, id string) []sessionData {
	out := sessions[:0]
	for _, session := range sessions {
		if session.ID != id {
			out = append(out, session)
		}
	}
	return out
}

func createSessionCmd(c *client, pending string) tea.Cmd {
	return func() tea.Msg {
		session, err := c.createSession(context.Background(), truncate(pending, 1000))
		return sessionCreatedMsg{session: session, pending: pending, err: err}
	}
}

func sendChatCmd(c *client, session sessionData, message string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		prediction, err := c.createChat(ctx, session.ID, message)
		var replacement *sessionData

		if err != nil && isNotFound(err) {
			created, createErr := c.createSession(ctx, truncate(message, 1000))
			if createErr != nil {
				return chatSentMsg{err: createErr}
			}
			replacement = &created
			prediction, err = c.createChat(ctx, created.ID, message)
		}

		if err != nil {
			return chatSentMsg{session: replacement, err: err}
		}

		id := session.ID
		if replacement != nil {
			id = replacement.ID
		}
		_ = c.setSession(id)

		var sessions []sessionData
		if list, listErr := c.listSessions(ctx, 1, 50); listErr == nil {
			sessions = list.Sessions
		}

		return chatSentMsg{
			prediction: prediction,
			session:    replacement,
			sessions:   sessions,
		}
	}
}

func loadSessionCmd(c *client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		session, err := c.getSession(ctx, sessionID)
		if err != nil {
			return sessionOpenedMsg{err: err}
		}

		list, err := c.listChats(ctx, sessionID, 1, 100)
		if err != nil {
			return sessionOpenedMsg{err: err}
		}

		return sessionOpenedMsg{session: session, chats: list.Chats}
	}
}

func refreshSessionsCmd(c *client, current *sessionData) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		list, err := c.listSessions(ctx, 1, 50)
		if err != nil {
			return refreshResultMsg{err: err}
		}

		var chats []chatData
		if current != nil {
			data, err := c.listChats(ctx, current.ID, 1, 100)
			if err != nil {
				return refreshResultMsg{err: err}
			}
			chats = data.Chats
		}

		return refreshResultMsg{sessions: list.Sessions, chats: chats}
	}
}

func deleteSessionCmd(c *client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := c.deleteSession(context.Background(), sessionID)
		return sessionDeletedMsg{id: sessionID, err: err}
	}
}

func logoutCmd(c *client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		_ = c.logout(ctx)
		_ = c.clear()
		return loggedOutMsg{}
	}
}
