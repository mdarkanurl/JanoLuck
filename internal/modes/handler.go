package modes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/database"
)

type Handler struct {
	service *Service
}

var validate = validator.New()

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg any) {
	switch v := msg.(type) {
	case ErrorResponse:
		writeJSON(w, status, v)
	case string:
		writeJSON(w, status, ErrorResponse{Error: v})
	case error:
		writeJSON(w, status, ErrorResponse{Error: v.Error()})
	default:
		writeJSON(w, status, ErrorResponse{Error: "internal server error"})
	}
}

func validationMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", e.Field())
	case "min", "max":
		switch e.Type().Kind() {
		case reflect.Slice, reflect.Array:
			if e.Tag() == "min" {
				return fmt.Sprintf("must contain at least %s item(s)", e.Param())
			}
			return fmt.Sprintf("must contain at most %s item(s)", e.Param())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			if e.Tag() == "min" {
				return fmt.Sprintf("must be at least %s", e.Param())
			}
			return fmt.Sprintf("must be at most %s", e.Param())
		}
		if e.Tag() == "min" {
			return fmt.Sprintf("must be at least %s characters", e.Param())
		}
		return fmt.Sprintf("must be at most %s characters", e.Param())
	case "dive":
		return "contains an invalid item"
	default:
		return fmt.Sprintf("failed on '%s' validation", e.Tag())
	}
}

func validateRequest[T any](req T, w http.ResponseWriter) bool {
	if err := validate.Struct(req); err != nil {
		var fields []ValidationError

		for _, e := range err.(validator.ValidationErrors) {
			field := strings.ToLower(e.StructField())
			if ns := e.Namespace(); strings.Contains(ns, "[") {
				field = strings.ToLower(ns[strings.Index(ns, ".")+1:])
			}
			fields = append(fields, ValidationError{
				Field:   field,
				Message: validationMessage(e),
			})
		}

		writeError(w, http.StatusBadRequest, ErrorResponse{
			Error:  "invalid_input",
			Fields: fields,
		})
		return false
	}
	return true
}

func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
		})
		return v, false
	}
	return v, true
}

func pathParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	value := strings.TrimSpace(chi.URLParam(r, name))
	if value == "" {
		writeError(w, http.StatusNotFound, "not found")
		return "", false
	}
	return value, true
}

func userIDFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return "", false
	}
	return userID, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, ErrInternal.Error())
	}
}

func toModeData(m database.Mode) ModeData {
	var userID string
	if m.Userid.Valid {
		userID = m.Userid.UUID.String()
	}

	return ModeData{
		ID:        m.ID.String(),
		UserID:    userID,
		Name:      m.Name,
		Prompt:    m.Prompt,
		IsBuiltIn: m.Isbuiltin,
		CreatedAt: m.CreateAt.UTC().Format(time.RFC3339),
		UpdatedAt: m.UpdateAt.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) CreateMode(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[CreateModeRequest](w, r)
	if !ok {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Prompt = strings.TrimSpace(req.Prompt)

	if !validateRequest(req, w) {
		return
	}

	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	mode, err := h.service.CreateMode(r.Context(), userID, req.Name, req.Prompt)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, ModeResponse{
		Message: "mode created",
		Data:    toModeData(mode),
	})
}

func (h *Handler) ListModes(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := h.service.ListModes(r.Context(), userID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	items := make([]ModeData, 0, len(result.Modes))
	for _, mode := range result.Modes {
		items = append(items, toModeData(mode))
	}

	var activeModeID string
	if result.ActiveModeID != uuid.Nil {
		activeModeID = result.ActiveModeID.String()
	}

	writeJSON(w, http.StatusOK, ModeListResponse{
		Message: "modes retrieved",
		Data: ModeListData{
			Modes:        items,
			Total:        result.Total,
			ActiveModeID: activeModeID,
		},
	})
}

func (h *Handler) GetMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	modeID, ok := pathParam(w, r, "modeID")
	if !ok {
		return
	}

	mode, err := h.service.GetMode(r.Context(), userID, modeID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ModeResponse{
		Message: "mode retrieved",
		Data:    toModeData(mode),
	})
}

func (h *Handler) UpdateMode(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[UpdateModeRequest](w, r)
	if !ok {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Prompt = strings.TrimSpace(req.Prompt)

	if !validateRequest(req, w) {
		return
	}

	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	modeID, ok := pathParam(w, r, "modeID")
	if !ok {
		return
	}

	mode, err := h.service.UpdateMode(r.Context(), userID, modeID, req.Name, req.Prompt)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ModeResponse{
		Message: "mode updated",
		Data:    toModeData(mode),
	})
}

func (h *Handler) DeleteMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	modeID, ok := pathParam(w, r, "modeID")
	if !ok {
		return
	}

	if err := h.service.DeleteMode(r.Context(), userID, modeID); err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ModeActionResponse{
		Message: "mode deleted",
		Data: ModeActionData{
			ModeID: modeID,
			UserID: userID,
		},
	})
}

func (h *Handler) GetActiveMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	mode, err := h.service.GetActiveMode(r.Context(), userID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ModeResponse{
		Message: "active mode retrieved",
		Data:    toModeData(mode),
	})
}

func (h *Handler) SetActiveMode(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[SetActiveModeRequest](w, r)
	if !ok {
		return
	}

	req.ModeID = strings.TrimSpace(req.ModeID)

	if !validateRequest(req, w) {
		return
	}

	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	modeID, err := h.service.SetActiveMode(r.Context(), userID, req.ModeID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ModeActionResponse{
		Message: "active mode updated",
		Data: ModeActionData{
			ModeID: modeID.String(),
			UserID: userID,
		},
	})
}

func (h *Handler) ClearActiveMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}

	if err := h.service.ClearActiveMode(r.Context(), userID); err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, ModeActionResponse{
		Message: "active mode cleared",
		Data: ModeActionData{
			UserID: userID,
		},
	})
}
