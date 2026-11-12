package sessions

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	SummaryTopic      = "session.summary.requested"
	SummaryBatchSize  = 10
	summaryRecentSize = 20
)

type SummaryJob struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
}

func (j SummaryJob) Validate() (sessionID, userID uuid.UUID, err error) {
	if strings.TrimSpace(j.SessionID) == "" {
		return uuid.Nil, uuid.Nil, fmt.Errorf("session id is required")
	}

	sessionId, err := uuid.Parse(strings.TrimSpace(j.SessionID))
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid session id: %v", err)
	}

	if strings.TrimSpace(j.UserID) == "" {
		return uuid.Nil, uuid.Nil, fmt.Errorf("user id is required")
	}

	userId, err := uuid.Parse(strings.TrimSpace(j.UserID))
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid user id: %v", err)
	}
	return sessionId, userId, nil
}
