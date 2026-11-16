package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
)

type decodeTarget struct {
	Content string `json:"content" validate:"required"`
}

func TestWriteJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteJSON(rr, http.StatusCreated, map[string]string{"ok": "yes"})

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != `{"ok":"yes"}` {
		t.Errorf("body = %q", got)
	}
}

func TestWriteError(t *testing.T) {
	cases := []struct {
		name string
		msg  any
		want string
	}{
		{name: "string", msg: "boom", want: "boom"},
		{name: "error", msg: errors.New("boom"), want: "boom"},
		{name: "struct", msg: ErrorResponse{Error: "custom"}, want: "custom"},
		{name: "unknown", msg: 42, want: "internal server error"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			WriteError(rr, http.StatusBadRequest, tt.msg)

			var body ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Error != tt.want {
				t.Errorf("error = %q, want %q", body.Error, tt.want)
			}
		})
	}
}

func TestValidationMessage(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "required",
			value: struct {
				Name string `validate:"required"`
			}{},
			want: "Name is required",
		},
		{
			name: "email",
			value: struct {
				Email string `validate:"email"`
			}{Email: "bad"},
			want: "must be a valid email address",
		},
		{
			name: "oneof",
			value: struct {
				Choice string `validate:"oneof=a b"`
			}{Choice: "c"},
			want: "Choice must be one of: a b",
		},
		{
			name: "min string",
			value: struct {
				Short string `validate:"min=3"`
			}{Short: "ab"},
			want: "must be at least 3 characters",
		},
		{
			name: "min slice",
			value: struct {
				Items []string `validate:"min=2"`
			}{Items: []string{"one"}},
			want: "must contain at least 2 item(s)",
		},
		{
			name: "max integer",
			value: struct {
				Count int `validate:"max=2"`
			}{Count: 3},
			want: "must be at most 2",
		},
		{
			name: "dive",
			value: struct {
				Items []string `validate:"dive,required"`
			}{Items: []string{""}},
			want: "Items[0] is required",
		},
		{
			name: "unknown tag",
			value: struct {
				Name string `validate:"uppercase"`
			}{},
			want: "failed on 'uppercase' validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validator.Struct(tt.value)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			fields, ok := err.(validator.ValidationErrors)
			if !ok || len(fields) != 1 {
				t.Fatalf("unexpected errors: %v", err)
			}
			if got := ValidationMessage(fields[0]); got != tt.want {
				t.Errorf("ValidationMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateRequest(t *testing.T) {
	rr := httptest.NewRecorder()
	if ValidateRequest(decodeTarget{}, rr) {
		t.Fatal("expected an empty request to be invalid")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}

	var body ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "invalid_input" {
		t.Errorf("error = %q, want invalid_input", body.Error)
	}
	if len(body.Fields) != 1 || body.Fields[0].Field != "content" {
		t.Errorf("fields = %+v, want one content error", body.Fields)
	}

	rr = httptest.NewRecorder()
	if !ValidateRequest(decodeTarget{Content: "hi"}, rr) {
		t.Error("expected a complete request to be valid")
	}
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		wantStatus  int
	}{
		{name: "valid", body: `{"content":"hi"}`, wantStatus: http.StatusOK},
		{name: "valid with charset", body: `{"content":"hi"}`, contentType: "application/json; charset=utf-8", wantStatus: http.StatusOK},
		{name: "trailing whitespace", body: "{\"content\":\"hi\"}\n\t ", wantStatus: http.StatusOK},
		{name: "malformed", body: `{"content":`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", body: `{"content":"hi","extra":true}`, wantStatus: http.StatusBadRequest},
		{name: "trailing data", body: `{"content":"hi"}{"content":"bye"}`, wantStatus: http.StatusBadRequest},
		{name: "trailing garbage", body: `{"content":"hi"} nope`, wantStatus: http.StatusBadRequest},
		{name: "empty body", body: ``, wantStatus: http.StatusBadRequest},
		{name: "wrong content type", body: `{"content":"hi"}`, contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "invalid content type", body: `{"content":"hi"}`, contentType: "application/json; charset", wantStatus: http.StatusUnsupportedMediaType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			got, ok := DecodeJSON[decodeTarget](rr, req)
			if tt.wantStatus == http.StatusOK {
				if !ok || got.Content != "hi" {
					t.Fatalf("DecodeJSON() = (%+v, %v), want content hi", got, ok)
				}
				return
			}

			if ok {
				t.Fatal("expected decoding to fail")
			}
			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

func TestDecodeJSONOversizedBody(t *testing.T) {
	rr := httptest.NewRecorder()
	body := `{"content":"` + strings.Repeat("a", MaxBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))

	if _, ok := DecodeJSON[decodeTarget](rr, req); ok {
		t.Fatal("expected an oversized body to fail")
	}
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rr.Code)
	}
}

func TestParsePagination(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	page, limit, ok := ParsePagination(httptest.NewRecorder(), r)
	if !ok || page != DefaultPage || limit != DefaultLimit {
		t.Errorf("defaults = (%d, %d, %v), want (%d, %d, true)", page, limit, ok, DefaultPage, DefaultLimit)
	}

	r = httptest.NewRequest(http.MethodGet, "/?page=%203%20&limit=%205%20", nil)
	page, limit, ok = ParsePagination(httptest.NewRecorder(), r)
	if !ok || page != 3 || limit != 5 {
		t.Errorf("values = (%d, %d, %v), want (3, 5, true)", page, limit, ok)
	}

	for _, target := range []string{"/?page=x", "/?limit=1.5"} {
		rr := httptest.NewRecorder()
		if _, _, ok := ParsePagination(rr, httptest.NewRequest(http.MethodGet, target, nil)); ok {
			t.Errorf("%s: expected parsing to fail", target)
		}
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rr.Code)
		}
	}
}
