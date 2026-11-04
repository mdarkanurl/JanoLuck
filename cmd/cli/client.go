package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiPrefix = "/api/v1"

var (
	errNotLoggedIn    = errors.New("not logged in")
	errSessionExpired = errors.New("session expired")
)

type client struct {
	baseURL   string
	http      *http.Client
	creds     credentials
	statePath string
}

func newClient(apiURL string) (*client, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}

	creds, err := loadCredentials(path)
	if err != nil {
		return nil, err
	}

	return &client{
		baseURL:   strings.TrimRight(apiURL, "/"),
		http:      &http.Client{Timeout: 3 * time.Minute},
		creds:     creds,
		statePath: path,
	}, nil
}

func (c *client) loggedIn() bool {
	return c.creds.AccessToken != ""
}

func (c *client) do(ctx context.Context, method, path string, body any, auth bool) ([]byte, error) {
	if auth {
		if c.creds.AccessToken == "" {
			return nil, errNotLoggedIn
		}
		if c.accessExpired() {
			if err := c.refresh(ctx); err != nil {
				return nil, err
			}
		}
	}

	raw, status, err := c.send(ctx, method, path, body, auth)
	if err != nil {
		return nil, err
	}

	if auth && status == http.StatusUnauthorized {
		if c.creds.RefreshToken == "" {
			_ = c.clear()
			return nil, errSessionExpired
		}

		if rerr := c.refresh(ctx); rerr != nil {
			var apiErr *apiError
			if errors.As(rerr, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusBadRequest) {
				_ = c.clear()
				return nil, errSessionExpired
			}
			return nil, fmt.Errorf("could not refresh session: %w", rerr)
		}

		raw, status, err = c.send(ctx, method, path, body, auth)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized {
			_ = c.clear()
			return nil, errSessionExpired
		}
	}

	if status < 200 || status >= 300 {
		return nil, parseAPIError(status, raw)
	}

	return raw, nil
}

func (c *client) send(ctx context.Context, method, path string, body any, auth bool) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("encode request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+apiPrefix+path, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "janoluck-cli/"+version)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth && c.creds.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return data, resp.StatusCode, nil
}

func (c *client) accessExpired() bool {
	return !c.creds.AccessExpiresAt.IsZero() && time.Now().Add(5*time.Second).After(c.creds.AccessExpiresAt)
}

func (c *client) refresh(ctx context.Context) error {
	if c.creds.RefreshToken == "" {
		return errNotLoggedIn
	}
	if !c.creds.RefreshExpiresAt.IsZero() && time.Now().After(c.creds.RefreshExpiresAt) {
		_ = c.clear()
		return errSessionExpired
	}

	raw, status, err := c.send(ctx, http.MethodPost, "/auth/refresh", map[string]string{
		"refreshToken": c.creds.RefreshToken,
	}, false)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return parseAPIError(status, raw)
	}

	var resp tokenResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode refresh response: %w", err)
	}
	if resp.Data.AccessToken == "" {
		return errors.New("server returned an empty access token")
	}

	return c.saveTokens(c.creds.Email, resp.Data)
}

func (c *client) saveTokens(email string, tokens authTokens) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email != "" && c.creds.Email != "" && !strings.EqualFold(email, c.creds.Email) {
		c.creds.LastSessionID = ""
	}
	if email != "" {
		c.creds.Email = email
	}

	c.creds.AccessToken = tokens.AccessToken
	c.creds.RefreshToken = tokens.RefreshToken

	now := time.Now()
	if tokens.ExpiresIn > 0 {
		c.creds.AccessExpiresAt = now.Add(time.Duration(tokens.ExpiresIn) * time.Second)
	} else {
		c.creds.AccessExpiresAt = time.Time{}
	}
	if tokens.RefreshExpiresIn > 0 {
		c.creds.RefreshExpiresAt = now.Add(time.Duration(tokens.RefreshExpiresIn) * time.Second)
	} else {
		c.creds.RefreshExpiresAt = time.Time{}
	}

	return saveCredentials(c.statePath, c.creds)
}

func (c *client) clear() error {
	c.creds = credentials{}
	return removeCredentials(c.statePath)
}

func (c *client) setSession(sessionID string) error {
	c.creds.LastSessionID = strings.TrimSpace(sessionID)
	return saveCredentials(c.statePath, c.creds)
}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	return e.Message
}

type errorField struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error  string       `json:"error"`
	Fields []errorField `json:"fields"`
}

func parseAPIError(status int, body []byte) error {
	var resp errorResponse
	if err := json.Unmarshal(body, &resp); err == nil && (resp.Error != "" || len(resp.Fields) > 0) {
		message := resp.Error
		if message == "" {
			message = http.StatusText(status)
		}
		for _, field := range resp.Fields {
			message += fmt.Sprintf("\n  %s: %s", field.Field, field.Message)
		}
		return &apiError{Status: status, Message: message}
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	return &apiError{Status: status, Message: message}
}

func isNotFound(err error) bool {
	var apiErr *apiError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

func decodeResponse[T any](raw []byte) (T, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode response: %w", err)
	}
	return value, nil
}

// API response types.

type authTokens struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	TokenType        string `json:"tokenType"`
	ExpiresIn        int64  `json:"expiresIn"`
	RefreshExpiresIn int64  `json:"refreshExpiresIn"`
}

type tokenResponse struct {
	Message string     `json:"message"`
	Data    authTokens `json:"data"`
}

type messageResponse struct {
	Message string `json:"message"`
}

type meData struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type meResponse struct {
	Message string `json:"message"`
	Data    meData `json:"data"`
}

type profileQuestion struct {
	Index    int    `json:"index"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type profileData struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Questions []profileQuestion `json:"questions"`
	UserID    string            `json:"userId"`
}

type profileResponse struct {
	Message string      `json:"message"`
	Data    profileData `json:"data"`
}

type questionAnswerUpdate struct {
	Index  int    `json:"index"`
	Answer string `json:"answer"`
}

type updateProfileRequest struct {
	Name      *string                `json:"name,omitempty"`
	Questions []questionAnswerUpdate `json:"questions,omitempty"`
}

type sessionData struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UserID    string `json:"userId"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type sessionResponse struct {
	Message string      `json:"message"`
	Data    sessionData `json:"data"`
}

type sessionListData struct {
	UserID   string        `json:"userId"`
	Sessions []sessionData `json:"sessions"`
	Page     int           `json:"page"`
	Limit    int           `json:"limit"`
	Total    int           `json:"total"`
	HasMore  bool          `json:"hasMore"`
}

type sessionListResponse struct {
	Message string          `json:"message"`
	Data    sessionListData `json:"data"`
}

type chatData struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	UserID    string `json:"userId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type chatResponse struct {
	Message string   `json:"message"`
	Data    chatData `json:"data"`
}

type chatListData struct {
	SessionID string     `json:"sessionId"`
	UserID    string     `json:"userId"`
	Chats     []chatData `json:"chats"`
	Page      int        `json:"page"`
	Limit     int        `json:"limit"`
	Total     int        `json:"total"`
	HasMore   bool       `json:"hasMore"`
}

type chatListResponse struct {
	Message string       `json:"message"`
	Data    chatListData `json:"data"`
}

// API methods.

func (c *client) signup(ctx context.Context, email, password string) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/signup", map[string]string{
		"email":    email,
		"password": password,
	}, false)
	return err
}

func (c *client) login(ctx context.Context, email, password string) (authTokens, error) {
	raw, err := c.do(ctx, http.MethodPost, "/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, false)
	if err != nil {
		return authTokens{}, err
	}

	resp, err := decodeResponse[tokenResponse](raw)
	if err != nil {
		return authTokens{}, err
	}
	return resp.Data, nil
}

func (c *client) verifyEmail(ctx context.Context, email string, code int) (authTokens, error) {
	raw, err := c.do(ctx, http.MethodPost, "/auth/verify-email", map[string]any{
		"email": email,
		"code":  code,
	}, false)
	if err != nil {
		return authTokens{}, err
	}

	resp, err := decodeResponse[tokenResponse](raw)
	if err != nil {
		return authTokens{}, err
	}
	return resp.Data, nil
}

func (c *client) logout(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/logout", nil, true)
	return err
}

func (c *client) me(ctx context.Context) (meData, error) {
	raw, err := c.do(ctx, http.MethodGet, "/auth/me", nil, true)
	if err != nil {
		return meData{}, err
	}

	resp, err := decodeResponse[meResponse](raw)
	if err != nil {
		return meData{}, err
	}
	return resp.Data, nil
}

func (c *client) forgotPassword(ctx context.Context, email string) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/forgot-password", map[string]string{
		"email": email,
	}, false)
	return err
}

func (c *client) resetPassword(ctx context.Context, email string, code int, newPassword string) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/reset-password", map[string]any{
		"email":       email,
		"code":        code,
		"newPassword": newPassword,
	}, false)
	return err
}

func (c *client) changePassword(ctx context.Context, currentPassword, newPassword string) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/change-password", map[string]string{
		"currentPassword": currentPassword,
		"newPassword":     newPassword,
	}, true)
	return err
}

func (c *client) getProfile(ctx context.Context) (profileData, error) {
	raw, err := c.do(ctx, http.MethodGet, "/profiles", nil, true)
	if err != nil {
		return profileData{}, err
	}

	resp, err := decodeResponse[profileResponse](raw)
	if err != nil {
		return profileData{}, err
	}
	return resp.Data, nil
}

func (c *client) createProfile(ctx context.Context, name, qOne, qTwo string) (profileData, error) {
	raw, err := c.do(ctx, http.MethodPost, "/profiles", map[string]string{
		"name": name,
		"qOne": qOne,
		"qTwo": qTwo,
	}, true)
	if err != nil {
		return profileData{}, err
	}

	resp, err := decodeResponse[profileResponse](raw)
	if err != nil {
		return profileData{}, err
	}
	return resp.Data, nil
}

func (c *client) updateProfile(ctx context.Context, req updateProfileRequest) (profileData, error) {
	raw, err := c.do(ctx, http.MethodPut, "/profiles", req, true)
	if err != nil {
		return profileData{}, err
	}

	resp, err := decodeResponse[profileResponse](raw)
	if err != nil {
		return profileData{}, err
	}
	return resp.Data, nil
}

func (c *client) createSession(ctx context.Context, content string) (sessionData, error) {
	raw, err := c.do(ctx, http.MethodPost, "/sessions", map[string]string{
		"content": content,
	}, true)
	if err != nil {
		return sessionData{}, err
	}

	resp, err := decodeResponse[sessionResponse](raw)
	if err != nil {
		return sessionData{}, err
	}
	return resp.Data, nil
}

func (c *client) listSessions(ctx context.Context, page, limit int) (sessionListData, error) {
	query := url.Values{}
	query.Set("page", fmt.Sprint(page))
	query.Set("limit", fmt.Sprint(limit))

	raw, err := c.do(ctx, http.MethodGet, "/sessions?"+query.Encode(), nil, true)
	if err != nil {
		return sessionListData{}, err
	}

	resp, err := decodeResponse[sessionListResponse](raw)
	if err != nil {
		return sessionListData{}, err
	}
	return resp.Data, nil
}

func (c *client) getSession(ctx context.Context, sessionID string) (sessionData, error) {
	raw, err := c.do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(sessionID), nil, true)
	if err != nil {
		return sessionData{}, err
	}

	resp, err := decodeResponse[sessionResponse](raw)
	if err != nil {
		return sessionData{}, err
	}
	return resp.Data, nil
}

func (c *client) deleteSession(ctx context.Context, sessionID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(sessionID), nil, true)
	return err
}

func (c *client) createChat(ctx context.Context, sessionID, content string) (chatData, error) {
	raw, err := c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(sessionID)+"/chats", map[string]string{
		"content": content,
	}, true)
	if err != nil {
		return chatData{}, err
	}

	resp, err := decodeResponse[chatResponse](raw)
	if err != nil {
		return chatData{}, err
	}
	return resp.Data, nil
}

func (c *client) listChats(ctx context.Context, sessionID string, page, limit int) (chatListData, error) {
	query := url.Values{}
	query.Set("page", fmt.Sprint(page))
	query.Set("limit", fmt.Sprint(limit))

	path := "/sessions/" + url.PathEscape(sessionID) + "/chats?" + query.Encode()
	raw, err := c.do(ctx, http.MethodGet, path, nil, true)
	if err != nil {
		return chatListData{}, err
	}

	resp, err := decodeResponse[chatListResponse](raw)
	if err != nil {
		return chatListData{}, err
	}
	return resp.Data, nil
}
