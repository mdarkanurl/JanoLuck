package modes

type CreateModeRequest struct {
	Name   string `json:"name"   validate:"required,min=1,max=100"`
	Prompt string `json:"prompt" validate:"required,min=1,max=8000"`
}

type UpdateModeRequest struct {
	Name   string `json:"name"   validate:"required,min=1,max=100"`
	Prompt string `json:"prompt" validate:"required,min=1,max=8000"`
}

type SetActiveModeRequest struct {
	ModeID string `json:"modeId" validate:"required"`
}

type ModeData struct {
	ID        string `json:"id"`
	UserID    string `json:"userId,omitempty"`
	Name      string `json:"name"`
	Prompt    string `json:"prompt"`
	IsBuiltIn bool   `json:"isBuiltIn"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type ModeResponse struct {
	Message string   `json:"message"`
	Data    ModeData `json:"data"`
}

type ModeListData struct {
	Modes        []ModeData `json:"modes"`
	Page         int        `json:"page"`
	Limit        int        `json:"limit"`
	Total        int        `json:"total"`
	HasMore      bool       `json:"hasMore"`
	ActiveModeID string     `json:"activeModeId,omitempty"`
}

type ModeListResponse struct {
	Message string       `json:"message"`
	Data    ModeListData `json:"data"`
}

type ModeActionData struct {
	ModeID string `json:"modeId"`
	UserID string `json:"userId"`
}

type ModeActionResponse struct {
	Message string         `json:"message"`
	Data    ModeActionData `json:"data"`
}

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields []ValidationError `json:"fields,omitempty"`
}
