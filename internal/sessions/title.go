package sessions

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const TitleTopic = "session.title.requested"

type TitleJob struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Content   string `json:"content"`
}

func (j TitleJob) Validate() error {
	if strings.TrimSpace(j.SessionID) == "" {
		return fmt.Errorf("session id is required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(j.SessionID)); err != nil {
		return fmt.Errorf("invalid session id: %v", err)
	}
	if strings.TrimSpace(j.UserID) == "" {
		return fmt.Errorf("user id is required")
	}
	if _, err := uuid.Parse(strings.TrimSpace(j.UserID)); err != nil {
		return fmt.Errorf("invalid user id: %v", err)
	}
	if strings.TrimSpace(j.Content) == "" {
		return fmt.Errorf("content is required")
	}
	return nil
}
