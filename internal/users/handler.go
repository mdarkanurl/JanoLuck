package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
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

func (h *Handler) CreateProfile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[CreateProfileRequest](w, r)
	if !ok {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.QOne = strings.TrimSpace(req.QOne)
	req.QTwo = strings.TrimSpace(req.QTwo)

	if !validateRequest(req, w) {
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.CreateProfile(r.Context(), userID, req.Name, req.QOne, req.QTwo)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrAlreadyExists):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	var questions []Question
	if err := json.Unmarshal(result.Questions, &questions); err != nil {
		writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	writeJSON(w, http.StatusCreated, ProfileResponse{
		Message: "profile created",
		Data: ProfileData{
			ID:        result.ID.String(),
			Name:      result.Name,
			Questions: questions,
			UserID:    result.Userid.String(),
		},
	})
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.GetProfile(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	var questions []Question
	if err := json.Unmarshal(result.Questions, &questions); err != nil {
		writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	writeJSON(w, http.StatusOK, ProfileResponse{
		Message: "profile retrieved",
		Data: ProfileData{
			ID:        result.ID.String(),
			Name:      result.Name,
			Questions: questions,
			UserID:    result.Userid.String(),
		},
	})
}

func (h *Handler) ProfileExists(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	exists, err := h.service.ProfileExists(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, ProfileExistsResponse{
		Message: "profile status retrieved",
		Data: ProfileExistsData{
			Exists: exists,
			UserID: userID,
		},
	})
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[UpdateProfileRequest](w, r)
	if !ok {
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		req.Name = &trimmed
	}
	for i := range req.Questions {
		if req.Questions[i].Answer != nil {
			trimmed := strings.TrimSpace(*req.Questions[i].Answer)
			req.Questions[i].Answer = &trimmed
		}
	}

	if !validateRequest(req, w) {
		return
	}

	if req.Name == nil && len(req.Questions) == 0 {
		writeError(w, http.StatusBadRequest, "provide name or questions to update")
		return
	}

	userID, _, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	result, err := h.service.UpdateProfile(r.Context(), userID, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		}
		return
	}

	var questions []Question
	if err := json.Unmarshal(result.Questions, &questions); err != nil {
		writeError(w, http.StatusInternalServerError, ErrInternal.Error())
		return
	}

	writeJSON(w, http.StatusOK, ProfileResponse{
		Message: "profile updated",
		Data: ProfileData{
			ID:        result.ID.String(),
			Name:      result.Name,
			Questions: questions,
			UserID:    result.Userid.String(),
		},
	})
}
