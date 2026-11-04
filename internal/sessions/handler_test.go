package sessions

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/mdarkanurl/JanoLuck/internal/auth"
	"github.com/mdarkanurl/JanoLuck/internal/database"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

func doSessionRequest(t *testing.T, fn http.HandlerFunc, method, target, body, sessionID, userID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rctx := chi.NewRouteContext()
	if sessionID != "" {
		rctx.URLParams.Add("sessionID", sessionID)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if userID != "" {
		ctx = context.WithValue(ctx, auth.UserIDKey, userID)
		ctx = context.WithValue(ctx, auth.EmailKey, "user@example.com")
	}
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	fn(rr, req)
	return rr
}

func decodeSessionError(t *testing.T, rr *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var e ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return e
}

func decodeSessionResponse(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rr.Body).Decode(v); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func TestSessionWriteJSONAndError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusCreated, map[string]string{"ok": "yes"})

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	decodeSessionResponse(t, rr, &body)
	if body["ok"] != "yes" {
		t.Errorf("body = %v, want ok=yes", body)
	}

	cases := []struct {
		name string
		msg  any
		want string
	}{
		{name: "string", msg: "boom", want: "boom"},
		{name: "error", msg: ErrNotFound, want: ErrNotFound.Error()},
		{name: "struct", msg: ErrorResponse{Error: "custom"}, want: "custom"},
		{name: "other", msg: 42, want: "internal server error"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeError(rr, http.StatusBadRequest, tt.msg)
			if got := decodeSessionError(t, rr).Error; got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionValidationMessage(t *testing.T) {
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
			name: "min string",
			value: struct {
				Short string `validate:"min=3"`
			}{Short: "ab"},
			want: "must be at least 3 characters",
		},
		{
			name: "max string",
			value: struct {
				Long string `validate:"max=3"`
			}{Long: "abcd"},
			want: "must be at most 3 characters",
		},
		{
			name: "min slice",
			value: struct {
				Items []string `validate:"min=2"`
			}{Items: []string{"one"}},
			want: "must contain at least 2 item(s)",
		},
		{
			name: "max slice",
			value: struct {
				Items []string `validate:"max=2"`
			}{Items: []string{"one", "two", "three"}},
			want: "must contain at most 2 item(s)",
		},
		{
			name: "min integer",
			value: struct {
				Count int `validate:"min=2"`
			}{Count: 1},
			want: "must be at least 2",
		},
		{
			name: "max integer",
			value: struct {
				Count int `validate:"max=2"`
			}{Count: 3},
			want: "must be at most 2",
		},
		{
			name: "default",
			value: struct {
				Email string `validate:"email"`
			}{Email: "bad"},
			want: "failed on 'email' validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.Struct(tt.value)
			if err == nil {
				t.Fatal("expected validation error")
			}
			fields, ok := err.(validator.ValidationErrors)
			if !ok || len(fields) != 1 {
				t.Fatalf("unexpected errors: %v", err)
			}
			if got := validationMessage(fields[0]); got != tt.want {
				t.Errorf("validationMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionDecodeJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"hi"}`))
	got, ok := decodeJSON[CreateSessionRequest](rr, req)
	if !ok || got.Content != "hi" {
		t.Errorf("decodeJSON() = (%+v, %v), want content hi", got, ok)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":`))
	if _, ok := decodeJSON[CreateSessionRequest](rr, req); ok {
		t.Error("expected malformed body to fail")
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestParsePaginationTrimsWhitespace(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/sessions?page=%203%20&limit=%205%20", nil)
	page, limit, ok := parsePagination(httptest.NewRecorder(), r)
	if !ok || page != 3 || limit != 5 {
		t.Errorf("got (%d, %d, %v), want (3, 5, true)", page, limit, ok)
	}
}

func TestSessionIDFromRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", "  abc  ")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	got, ok := sessionIDFromRequest(rr, req)
	if !ok || got != "abc" {
		t.Errorf("sessionIDFromRequest() = (%q, %v), want (abc, true)", got, ok)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chi.NewRouteContext()))
	rr = httptest.NewRecorder()
	if _, ok := sessionIDFromRequest(rr, req); ok {
		t.Error("expected empty session id to fail")
	}
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestSessionWriteServiceError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "invalid input", err: ErrInvalidInput, status: http.StatusBadRequest, body: ErrInvalidInput.Error()},
		{name: "not found", err: ErrNotFound, status: http.StatusNotFound, body: ErrNotFound.Error()},
		{name: "internal", err: errors.New("boom"), status: http.StatusInternalServerError, body: ErrInternal.Error()},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeServiceError(rr, tt.err)
			if rr.Code != tt.status {
				t.Errorf("status = %d, want %d", rr.Code, tt.status)
			}
			if got := decodeSessionError(t, rr).Error; got != tt.body {
				t.Errorf("error = %q, want %q", got, tt.body)
			}
		})
	}
}

func TestToSessionDataUsesUTC(t *testing.T) {
	loc := time.FixedZone("test", 5*60*60)
	session := database.Session{
		ID:       sessionID,
		Name:     "My Session",
		Userid:   sessionUserID,
		CreateAt: time.Date(2026, 1, 2, 8, 4, 5, 0, loc),
		UpdateAt: time.Date(2026, 1, 2, 8, 4, 5, 0, loc),
	}

	data := toSessionData(session)
	if data.ID != sessionID.String() || data.UserID != sessionUserID.String() || data.Name != "My Session" {
		t.Errorf("unexpected data: %+v", data)
	}
	if data.CreatedAt != "2026-01-02T03:04:05Z" || data.UpdatedAt != "2026-01-02T03:04:05Z" {
		t.Errorf("timestamps = (%q, %q), want UTC RFC3339", data.CreatedAt, data.UpdatedAt)
	}
}

func TestCreateSessionHandlerValidation(t *testing.T) {
	h := NewHandler(nil)

	cases := []struct {
		name    string
		body    string
		userID  string
		status  int
		errText string
	}{
		{name: "malformed body", body: `{`, userID: sessionUserID.String(), status: http.StatusBadRequest, errText: "invalid request body"},
		{name: "empty content", body: `{"content":" "}`, userID: sessionUserID.String(), status: http.StatusBadRequest, errText: "invalid_input"},
		{name: "long content", body: `{"content":"` + strings.Repeat("a", 1001) + `"}`, userID: sessionUserID.String(), status: http.StatusBadRequest, errText: "invalid_input"},
		{name: "unauthenticated", body: `{"content":"hello"}`, userID: "", status: http.StatusUnauthorized, errText: "invalid or expired token"},
		{name: "invalid user", body: `{"content":"hello"}`, userID: "bad", status: http.StatusBadRequest, errText: "invalid input"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := doSessionRequest(t, h.CreateSession, http.MethodPost, "/", tt.body, "", tt.userID)
			if rr.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tt.status, rr.Body.String())
			}
			if !strings.Contains(decodeSessionError(t, rr).Error, tt.errText) {
				t.Errorf("error does not contain %q", tt.errText)
			}
		})
	}
}

func TestListSessionsHandlerValidation(t *testing.T) {
	h := NewHandler(nil)

	cases := []struct {
		name   string
		target string
		userID string
		status int
	}{
		{name: "bad page", target: "/?page=x", userID: sessionUserID.String(), status: http.StatusBadRequest},
		{name: "bad limit", target: "/?limit=x", userID: sessionUserID.String(), status: http.StatusBadRequest},
		{name: "zero page", target: "/?page=0", userID: sessionUserID.String(), status: http.StatusBadRequest},
		{name: "zero limit", target: "/?limit=0", userID: sessionUserID.String(), status: http.StatusBadRequest},
		{name: "unauthenticated", target: "/", userID: "", status: http.StatusUnauthorized},
		{name: "invalid user", target: "/", userID: "bad", status: http.StatusBadRequest},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := doSessionRequest(t, h.ListSessions, http.MethodGet, tt.target, "", "", tt.userID)
			if rr.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tt.status, rr.Body.String())
			}
		})
	}
}

func TestListSessionsHandlerRepositoryError(t *testing.T) {
	h := NewHandler(NewService(testRepository(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.QueryError(errors.New("boom"))
		},
	}), nil))

	rr := doSessionRequest(t, h.ListSessions, http.MethodGet, "/", "", "", sessionUserID.String())
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rr.Code, rr.Body.String())
	}
	if got := decodeSessionError(t, rr).Error; got != ErrInternal.Error() {
		t.Errorf("error = %q, want %q", got, ErrInternal.Error())
	}
}

func TestListSessionsHandlerSuccess(t *testing.T) {
	h := NewHandler(NewService(testRepository(t, &testutil.DB{
		Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "CountSessionsByUserID") {
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(5)}), nil
			}
			return testutil.NewRows(sessionColumns, sessionRowValues(), sessionRowValues()), nil
		},
	}), nil))

	rr := doSessionRequest(t, h.ListSessions, http.MethodGet, "/?page=2&limit=2", "", "", sessionUserID.String())
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}

	var resp SessionListResponse
	decodeSessionResponse(t, rr, &resp)
	if resp.Message != "sessions retrieved" {
		t.Errorf("message = %q", resp.Message)
	}
	if resp.Data.UserID != sessionUserID.String() || resp.Data.Page != 2 || resp.Data.Limit != 2 {
		t.Errorf("unexpected data: %+v", resp.Data)
	}
	if resp.Data.Total != 5 || !resp.Data.HasMore {
		t.Errorf("total/hasMore = (%d, %v), want (5, true)", resp.Data.Total, resp.Data.HasMore)
	}
	if len(resp.Data.Sessions) != 2 {
		t.Errorf("got %d sessions, want 2", len(resp.Data.Sessions))
	}
}

func TestGetSessionHandler(t *testing.T) {
	cases := []struct {
		name      string
		sessionID string
		userID    string
		status    int
	}{
		{name: "missing session", sessionID: "", userID: sessionUserID.String(), status: http.StatusNotFound},
		{name: "unauthenticated", sessionID: sessionID.String(), userID: "", status: http.StatusUnauthorized},
		{name: "invalid user", sessionID: sessionID.String(), userID: "bad", status: http.StatusBadRequest},
		{name: "invalid session", sessionID: "bad", userID: sessionUserID.String(), status: http.StatusBadRequest},
	}

	h := NewHandler(nil)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := doSessionRequest(t, h.GetSession, http.MethodGet, "/", "", tt.sessionID, tt.userID)
			if rr.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tt.status, rr.Body.String())
			}
		})
	}

	t.Run("not found", func(t *testing.T) {
		h := NewHandler(NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		}), nil))

		rr := doSessionRequest(t, h.GetSession, http.MethodGet, "/", "", sessionID.String(), sessionUserID.String())
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rr.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := NewHandler(NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(sessionColumns, sessionRowValues()), nil
			},
		}), nil))

		rr := doSessionRequest(t, h.GetSession, http.MethodGet, "/", "", sessionID.String(), sessionUserID.String())
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
		}
		var resp SessionResponse
		decodeSessionResponse(t, rr, &resp)
		if resp.Data.ID != sessionID.String() {
			t.Errorf("got id %q, want %q", resp.Data.ID, sessionID.String())
		}
	})
}

func TestDeleteSessionHandler(t *testing.T) {
	cases := []struct {
		name      string
		sessionID string
		userID    string
		status    int
	}{
		{name: "missing session", sessionID: "", userID: sessionUserID.String(), status: http.StatusNotFound},
		{name: "unauthenticated", sessionID: sessionID.String(), userID: "", status: http.StatusUnauthorized},
		{name: "invalid user", sessionID: sessionID.String(), userID: "bad", status: http.StatusBadRequest},
		{name: "invalid session", sessionID: "bad", userID: sessionUserID.String(), status: http.StatusBadRequest},
	}

	h := NewHandler(nil)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := doSessionRequest(t, h.DeleteSession, http.MethodDelete, "/", "", tt.sessionID, tt.userID)
			if rr.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tt.status, rr.Body.String())
			}
		})
	}

	t.Run("not found", func(t *testing.T) {
		h := NewHandler(NewService(testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(0)
			},
		}), nil))

		rr := doSessionRequest(t, h.DeleteSession, http.MethodDelete, "/", "", sessionID.String(), sessionUserID.String())
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rr.Code)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := NewHandler(NewService(testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(1)
			},
		}), nil))

		rr := doSessionRequest(t, h.DeleteSession, http.MethodDelete, "/", "", sessionID.String(), sessionUserID.String())
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
		}
		var resp SessionActionResponse
		decodeSessionResponse(t, rr, &resp)
		if resp.Data.ID != sessionID.String() || resp.Data.UserID != sessionUserID.String() {
			t.Errorf("unexpected action data: %+v", resp.Data)
		}
	})
}
