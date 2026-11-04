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
	current  *sessionData
	chats    []chatData
	err      error
}

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
			}
			return m, nil
		}

		if m.screen == screenChat {
			switch msg.String() {
			case "ctrl+h":
				m.help = true
				return m, nil
			case "ctrl+p":
				m.profile = newProfileModel(m.client, m.me, m.chat.profile, m.width, m.height)
				m.screen = screenProfile
				return m, m.profile.Init()
			}
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
	m.chat = newChatModel(m.client, msg.me, msg.profile, msg.sessions, msg.current, msg.chats, m.width, m.height)
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
	body.WriteString(logoStyle.Render("✦ JanoLuck"))
	body.WriteString("\n")
	body.WriteString(mutedStyle.Render("your optimistic friend for what-ifs"))
	body.WriteString("\n\n")

	if m.loadingErr == "" {
		body.WriteString(m.spinner.View())
		body.WriteString(" ")
		body.WriteString(titleStyle.Render("Connecting..."))
		body.WriteString("\n")
		body.WriteString(mutedStyle.Render(truncate(m.apiURL, width-10)))
	} else {
		body.WriteString(dangerStyle.Render("Could not connect"))
		body.WriteString("\n\n")
		body.WriteString(mutedStyle.Render(wrapText(m.loadingErr, width-10)))
		body.WriteString("\n\n")
		body.WriteString(accentStyle.Render("[r]") + mutedStyle.Render(" retry    "))
		body.WriteString(accentStyle.Render("[ctrl+c]") + mutedStyle.Render(" quit"))
	}

	return centeredCard(m.width, m.height, cardStyle(width).Render(body.String()))
}

func helpView(width, height int) string {
	cardWidth := min(68, max(40, width-4))

	key := func(k, description string) string {
		return accentStyle.Render(fmt.Sprintf("%-12s", k)) + mutedStyle.Render(description)
	}

	lines := []string{
		titleStyle.Render("Keyboard shortcuts"),
		"",
		mutedStyle.Render("NAVIGATION"),
		key("tab", "switch between sessions and composer"),
		key("j / k", "move through sessions"),
		key("enter", "open a session or send a message"),
		key("ctrl+j", "insert a newline"),
		key("esc", "close help or leave profile"),
		"",
		mutedStyle.Render("CONVERSATIONS"),
		key("ctrl+n", "start a new conversation"),
		key("ctrl+r", "refresh sessions and messages"),
		key("d", "delete the selected session"),
		key("ctrl+p", "edit your profile"),
		key("ctrl+l", "log out"),
		"",
		mutedStyle.Render("GENERAL"),
		key("ctrl+h", "toggle this help"),
		key("ctrl+c", "quit"),
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
		Width(max(20, width-8))
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

		var current *sessionData
		var chats []chatData

		if len(list.Sessions) > 0 {
			chosen := 0
			if c.creds.LastSessionID != "" {
				for i := range list.Sessions {
					if list.Sessions[i].ID == c.creds.LastSessionID {
						chosen = i
						break
					}
				}
			}

			if session, err := c.getSession(ctx, list.Sessions[chosen].ID); err == nil {
				current = &session
				if data, err := c.listChats(ctx, session.ID, 1, 100); err == nil {
					chats = data.Chats
				}
			}
		}

		return bootstrapMsg{
			me:       me,
			sessions: list.Sessions,
			profile:  profile,
			current:  current,
			chats:    chats,
		}
	}
}
