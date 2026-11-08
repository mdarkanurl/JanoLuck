package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenLoading screen = iota
	screenAuth
	screenChat
	screenProfile
)

type appModel struct {
	client *client
	apiURL string

	width  int
	height int

	screen     screen
	spinner    spinner.Model
	loadingErr string
	me         meData

	auth    authModel
	chat    chatModel
	profile profileModel

	help bool
}

type bootstrapMsg struct {
	me       meData
	sessions []sessionData
	profile  *profileData
	err      error
}

type openHelpMsg struct{}

type openProfileMsg struct{}

type authenticatedMsg struct{}

type loggedOutMsg struct{}

type sessionExpiredMsg struct {
	err error
}

type profileSavedMsg struct {
	profile profileData
}

func newAppModel(c *client, apiURL string) appModel {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = primaryStyle

	model := appModel{
		client: c,
		apiURL: apiURL,
		width:  80,
		height: 24,
		screen: screenLoading,
		auth:   newAuthModel(c, apiURL, 80, 24),
	}
	model.spinner = spin

	if !c.loggedIn() {
		model.screen = screenAuth
	}

	return model
}

func (m appModel) Init() tea.Cmd {
	if m.screen == screenLoading {
		return tea.Batch(bootstrapCmd(m.client), m.spinner.Tick)
	}
	return m.auth.Init()
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.auth.setSize(m.width, m.height)
		m.chat.setSize(m.width, m.height)
		m.profile.setSize(m.width, m.height)
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.help {
			switch msg.String() {
			case "esc", "ctrl+h", "?":
				m.help = false
				if m.screen == screenChat {
					m.chat.focusInput()
				}
			}
			return m, nil
		}

		if m.screen == screenChat && msg.String() == "ctrl+h" {
			m.help = true
			return m, nil
		}

		if m.screen == screenProfile && msg.String() == "esc" && !m.profile.saving {
			m.screen = screenChat
			m.chat.focusInput()
			return m, nil
		}

	case spinner.TickMsg:
		if m.screen == screenLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case authenticatedMsg:
		m.screen = screenLoading
		m.loadingErr = ""
		return m, tea.Batch(bootstrapCmd(m.client), m.spinner.Tick)

	case openHelpMsg:
		m.help = true
		return m, nil

	case openProfileMsg:
		m.profile = newProfileModel(m.client, m.me, m.chat.profile, m.width, m.height)
		m.screen = screenProfile
		return m, m.profile.Init()

	case bootstrapMsg:
		return m.handleBootstrap(msg)

	case loggedOutMsg:
		m.screen = screenAuth
		m.auth = newAuthModel(m.client, m.apiURL, m.width, m.height)
		m.auth.notice = "You're logged out."
		m.chat = chatModel{}
		m.profile = profileModel{}
		return m, m.auth.Init()

	case sessionExpiredMsg:
		m.screen = screenAuth
		m.auth = newAuthModel(m.client, m.apiURL, m.width, m.height)
		m.auth.notice = "Your session expired. Please log in again."
		m.auth.err = inline(msg.err.Error())
		m.chat = chatModel{}
		m.profile = profileModel{}
		return m, m.auth.Init()

	case profileSavedMsg:
		m.chat.profile = &msg.profile
		m.chat.toast = "Profile saved."
		m.screen = screenChat
		m.chat.focusInput()
		return m, nil
	}

	switch m.screen {
	case screenLoading:
		return m.updateLoading(msg)
	case screenAuth:
		var cmd tea.Cmd
		m.auth, cmd = m.auth.Update(msg)
		return m, cmd
	case screenChat:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	case screenProfile:
		var cmd tea.Cmd
		m.profile, cmd = m.profile.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m appModel) handleBootstrap(msg bootstrapMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if errors.Is(msg.err, errNotLoggedIn) || errors.Is(msg.err, errSessionExpired) {
			m.screen = screenAuth
			m.auth = newAuthModel(m.client, m.apiURL, m.width, m.height)
			m.auth.notice = "Please log in to continue."
			m.auth.err = inline(msg.err.Error())
			return m, m.auth.Init()
		}

		m.loadingErr = inline(msg.err.Error())
		return m, nil
	}

	m.me = msg.me
	m.chat = newChatModel(m.client, msg.me, msg.profile, msg.sessions, m.width, m.height)
	m.screen = screenChat

	return m, m.chat.Init()
}

func (m appModel) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.loadingErr != "" && key.String() == "r" {
		m.loadingErr = ""
		return m, tea.Batch(bootstrapCmd(m.client), m.spinner.Tick)
	}

	return m, nil
}

func (m appModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.help {
		return helpView(m.width, m.height)
	}

	switch m.screen {
	case screenLoading:
		return m.loadingView()
	case screenAuth:
		return m.auth.View()
	case screenChat:
		return m.chat.View()
	case screenProfile:
		return m.profile.View()
	}

	return ""
}

func (m appModel) loadingView() string {
	width := min(66, max(34, m.width-4))

	var body strings.Builder
	body.WriteString(logoStyle.Render("JanoLuck"))
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render("your optimistic friend for what-ifs"))
	body.WriteString("\n\n")

	if m.loadingErr == "" {
		body.WriteString(m.spinner.View())
		body.WriteString(" ")
		body.WriteString(mutedStyle.Render("Connecting..."))
		body.WriteString("\n")
		body.WriteString(mutedStyle.Render(truncate(m.apiURL, width)))
	} else {
		body.WriteString(dangerStyle.Render("Could not connect"))
		body.WriteString("\n\n")
		body.WriteString(mutedStyle.Render(wrapText(m.loadingErr, width)))
		body.WriteString("\n\n")
		body.WriteString(accentStyle.Render("[r]") + mutedStyle.Render(" retry    "))
		body.WriteString(accentStyle.Render("[ctrl+c]") + mutedStyle.Render(" quit"))
	}

	block := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		Render(body.String())

	return centeredCard(m.width, m.height, block)
}

func helpView(width, height int) string {
	cardWidth := min(68, max(40, width-4))

	entry := func(k, description string) string {
		return accentStyle.Render(fmt.Sprintf("%-12s", k)) + mutedStyle.Render(description)
	}

	lines := []string{
		titleStyle.Render("Commands & shortcuts"),
		"",
		mutedStyle.Render("COMMANDS"),
		entry("/sessions", "browse and open previous chats"),
		entry("/new", "start a new conversation"),
		entry("/profile", "edit your profile"),
		entry("/logout", "log out"),
		entry("/help", "show this help"),
		entry("/quit", "quit JanoLuck"),
		"",
		mutedStyle.Render("COMPOSER"),
		entry("enter", "send a message"),
		entry("ctrl+j", "insert a newline"),
		entry("tab", "complete a slash command"),
		entry("ctrl+c", "quit"),
		"",
		mutedStyle.Render("CONVERSATION"),
		entry("↑ / ↓", "scroll messages line by line"),
		entry("wheel", "scroll messages with the mouse"),
		entry("pgup/pgdn", "scroll a page"),
		entry("ctrl+u/d", "scroll half a page"),
		"",
		mutedStyle.Render("SESSIONS"),
		entry("type", "filter conversations"),
		entry("↑ / ↓", "move through conversations"),
		entry("enter", "open the selected conversation"),
		entry("ctrl+d", "delete the selected conversation"),
		entry("esc", "close the picker"),
		"",
		mutedStyle.Render("Press esc or ctrl+h to close."),
	}

	content := strings.Join(lines, "\n")
	return centeredCard(width, height, cardStyle(cardWidth).Render(content))
}

func cardStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 3).
		Width(max(20, width-2))
}

func bootstrapCmd(c *client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		me, err := c.me(ctx)
		if err != nil {
			return bootstrapMsg{err: err}
		}

		list, err := c.listSessions(ctx, 1, 50)
		if err != nil {
			return bootstrapMsg{err: err}
		}

		var profile *profileData
		if value, err := c.getProfile(ctx); err == nil {
			profile = &value
		}

		return bootstrapMsg{
			me:       me,
			sessions: list.Sessions,
			profile:  profile,
		}
	}
}
