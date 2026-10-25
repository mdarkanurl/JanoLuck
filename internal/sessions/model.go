package sessions

type CreateSessionRequest struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
}

type SessionData struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UserID    string `json:"userId"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type SessionResponse struct {
	Message string      `json:"message"`
	Data    SessionData `json:"data"`
}

type SessionListData struct {
	UserID   string        `json:"userId"`
	Sessions []SessionData `json:"sessions"`
}

type SessionListResponse struct {
	Message string          `json:"message"`
	Data    SessionListData `json:"data"`
}

type SessionActionData struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
}

type SessionActionResponse struct {
	Message string            `json:"message"`
	Data    SessionActionData `json:"data"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields []ValidationError `json:"fields,omitempty"`
}
