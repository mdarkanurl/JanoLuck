package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

const (
	MaxBodyBytes = 1 << 20

	DefaultPage  = 1
	DefaultLimit = 20
)

var Validator = validator.New()

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error  string            `json:"error"`
	Fields []ValidationError `json:"fields,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, msg any) {
	switch v := msg.(type) {
	case ErrorResponse:
		WriteJSON(w, status, v)
	case string:
		WriteJSON(w, status, ErrorResponse{Error: v})
	case error:
		WriteJSON(w, status, ErrorResponse{Error: v.Error()})
	default:
		WriteJSON(w, status, ErrorResponse{Error: "internal server error"})
	}
}

func ValidationMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return e.Field() + " is required"
	case "email":
		return "must be a valid email address"
	case "oneof":
		return e.Field() + " must be one of: " + e.Param()
	case "min", "max":
		if marker, ok := e.Value().(interface{ IsVerificationCode() bool }); ok && marker.IsVerificationCode() {
			return "must be a 6-digit code"
		}

		switch e.Type().Kind() {
		case reflect.Slice, reflect.Array:
			if e.Tag() == "min" {
				return "must contain at least " + e.Param() + " item(s)"
			}
			return "must contain at most " + e.Param() + " item(s)"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			if e.Tag() == "min" {
				return "must be at least " + e.Param()
			}
			return "must be at most " + e.Param()
		}

		if e.Tag() == "min" {
			return "must be at least " + e.Param() + " characters"
		}
		return "must be at most " + e.Param() + " characters"
	case "dive":
		return "contains an invalid item"
	case "strong_password":
		return "password must contain at least one letter and one number"
	default:
		return "failed on '" + e.Tag() + "' validation"
	}
}

func ValidateRequest[T any](req T, w http.ResponseWriter) bool {
	err := Validator.Struct(req)
	if err == nil {
		return true
	}

	errs, ok := err.(validator.ValidationErrors)
	if !ok {
		WriteError(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return false
	}

	fields := make([]ValidationError, 0, len(errs))
	for _, e := range errs {
		field := strings.ToLower(e.StructField())
		if ns := e.Namespace(); strings.Contains(ns, "[") {
			field = strings.ToLower(ns[strings.Index(ns, ".")+1:])
		}
		fields = append(fields, ValidationError{
			Field:   field,
			Message: ValidationMessage(e),
		})
	}

	WriteError(w, http.StatusBadRequest, ErrorResponse{
		Error:  "invalid_input",
		Fields: fields,
	})
	return false
}

func DecodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T

	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			WriteError(w, http.StatusUnsupportedMediaType, ErrorResponse{
				Error: "Content-Type must be application/json",
			})
			return v, false
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&v); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			WriteError(w, http.StatusRequestEntityTooLarge, ErrorResponse{
				Error: "request body too large",
			})
			return v, false
		}

		WriteError(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
		})
		return v, false
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		WriteError(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid request body",
		})
		return v, false
	}

	return v, true
}

func ParsePagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	q := r.URL.Query()

	page := DefaultPage
	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			WriteError(w, http.StatusBadRequest, ErrorResponse{Error: "page must be an integer"})
			return 0, 0, false
		}
		page = v
	}

	limit := DefaultLimit
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			WriteError(w, http.StatusBadRequest, ErrorResponse{Error: "limit must be an integer"})
			return 0, 0, false
		}
		limit = v
	}

	return page, limit, true
}
