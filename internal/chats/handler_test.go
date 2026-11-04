package chats

import (
	"context"
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
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

func doChatRequest(t *testing.T, fn http.HandlerFunc, method, body, sessionID, userID string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/sessions/"+sessionID+"/chats", reader)

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

func decodeChatError(t *testing.T, rr *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()
	var e ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return e
}

func decodeChatResponse(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rr.Body).Decode(v); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func TestWriteJSONAndWriteError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, http.StatusCreated, map[string]string{"ok": "yes"})

	if rr.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	decodeChatResponse(t, rr, &body)
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
			if got := decodeChatError(t, rr).Error; got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
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
			name: "oneof",
			value: struct {
				Choice string `validate:"oneof=a b"`
			}{Choice: "c"},
			want: "Choice must be one of: a b",
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
			if !ok {
				t.Fatalf("unexpected error type %T", err)
			}
			if len(fields) != 1 {
				t.Fatalf("got %d validation errors, want 1", len(fields))
			}
			if got := validationMessage(fields[0]); got != tt.want {
				t.Errorf("validationMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateRequestWritesStructuredErrors(t *testing.T) {
	rr := httptest.NewRecorder()
	if validateRequest(CreateChatRequest{}, rr) {
		t.Fatal("expected empty request to be invalid")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}

	e := decodeChatError(t, rr)
	if e.Error != "invalid_input" {
		t.Errorf("error = %q, want invalid_input", e.Error)
	}
	if len(e.Fields) != 1 || e.Fields[0].Field != "content" {
		t.Errorf("fields = %+v, want one content error", e.Fields)
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":"hi"}`))
		got, ok := decodeJSON[CreateChatRequest](rr, req)
		if !ok || got.Content != "hi" {
			t.Errorf("decodeJSON() = (%+v, %v), want content hi", got, ok)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"content":`))
		if _, ok := decodeJSON[CreateChatRequest](rr, req); ok {
			t.Error("expected decode to fail")
		}
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("too large", func(t *testing.T) {
		rr := httptest.NewRecorder()
		body := `{"content":"` + strings.Repeat("a", 1<<20) + `"}`
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		if _, ok := decodeJSON[CreateChatRequest](rr, req); ok {
			t.Error("expected oversized body to fail")
		}
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})
}

func TestPathParam(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", "  abc  ")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	got, ok := pathParam(rr, req, "sessionID")
	if !ok || got != "abc" {
		t.Errorf("pathParam() = (%q, %v), want (abc, true)", got, ok)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chi.NewRouteContext()))
	rr = httptest.NewRecorder()
	if _, ok := pathParam(rr, req, "sessionID"); ok {
		t.Error("expected empty param to fail")
	}
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestParsePagination(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/chats", nil)
	page, limit, ok := parsePagination(httptest.NewRecorder(), r)
	if !ok || page != DefaultPage || limit != DefaultLimit {
		t.Errorf("defaults = (%d, %d, %v), want (%d, %d, true)", page, limit, ok, DefaultPage, DefaultLimit)
	}

	r = httptest.NewRequest(http.MethodGet, "/chats?page=3&limit=5", nil)
	page, limit, ok = parsePagination(httptest.NewRecorder(), r)
	if !ok || page != 3 || limit != 5 {
		t.Errorf("values = (%d, %d, %v), want (3, 5, true)", page, limit, ok)
	}

	for _, target := range []string{"/chats?page=x", "/chats?limit=1.5"} {
		rr := httptest.NewRecorder()
		if _, _, ok := parsePagination(rr, httptest.NewRequest(http.MethodGet, target, nil)); ok {
			t.Errorf("%s: expected parse failure", target)
		}
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rr.Code)
		}
	}
}

func TestWriteServiceError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "invalid input", err: ErrInvalidInput, status: http.StatusBadRequest, body: ErrInvalidInput.Error()},
		{name: "not found", err: ErrNotFound, status: http.StatusNotFound, body: ErrNotFound.Error()},
		{name: "session not found", err: ErrSessionNotFound, status: http.StatusNotFound, body: ErrSessionNotFound.Error()},
		{name: "internal", err: errors.New("boom"), status: http.StatusInternalServerError, body: ErrInternal.Error()},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeServiceError(rr, tt.err)
			if rr.Code != tt.status {
				t.Errorf("status = %d, want %d", rr.Code, tt.status)
			}
			if got := decodeChatError(t, rr).Error; got != tt.body {
				t.Errorf("error = %q, want %q", got, tt.body)
			}
		})
	}
}

func TestToChatDataUsesUTC(t *testing.T) {
	loc := time.FixedZone("test", 5*60*60)
	chat := Chat{
		ID:        testChatID,
		SessionID: testSessionID,
		UserID:    testUserID,
		Role:      RoleUser,
		Content:   "hello",
		CreatedAt: time.Date(2026, 1, 2, 8, 4, 5, 0, loc),
		UpdatedAt: time.Date(2026, 1, 2, 8, 4, 5, 0, loc),
	}

	data := toChatData(chat)
	if data.ID != testChatID.String() || data.SessionID != testSessionID.String() || data.UserID != testUserID.String() {
		t.Errorf("unexpected ids: %+v", data)
	}
	if data.CreatedAt != "2026-01-02T03:04:05Z" || data.UpdatedAt != "2026-01-02T03:04:05Z" {
		t.Errorf("timestamps = (%q, %q), want UTC RFC3339", data.CreatedAt, data.UpdatedAt)
	}
}

func TestCreateChatHandlerValidation(t *testing.T) {
	h := NewHandler(nil)

	cases := []struct {
		name      string
		body      string
		sessionID string
		userID    string
		status    int
		errText   string
	}{
		{name: "malformed body", body: `{`, sessionID: testSessionID.String(), userID: testUserID.String(), status: http.StatusBadRequest, errText: "invalid request body"},
		{name: "empty content", body: `{"content":"  "}`, sessionID: testSessionID.String(), userID: testUserID.String(), status: http.StatusBadRequest, errText: "invalid_input"},
		{name: "long content", body: `{"content":"` + strings.Repeat("a", 8001) + `"}`, sessionID: testSessionID.String(), userID: testUserID.String(), status: http.StatusBadRequest, errText: "invalid_input"},
		{name: "missing session", body: `{"content":"hello"}`, sessionID: "", userID: testUserID.String(), status: http.StatusNotFound, errText: "not found"},
		{name: "unauthenticated", body: `{"content":"hello"}`, sessionID: testSessionID.String(), userID: "", status: http.StatusUnauthorized, errText: "invalid or expired token"},
		{name: "invalid user", body: `{"content":"hello"}`, sessionID: testSessionID.String(), userID: "bad", status: http.StatusBadRequest, errText: "invalid input"},
		{name: "invalid session", body: `{"content":"hello"}`, sessionID: "bad", userID: testUserID.String(), status: http.StatusBadRequest, errText: "invalid input"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rr := doChatRequest(t, h.CreateChat, http.MethodPost, tt.body, tt.sessionID, tt.userID)
			if rr.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tt.status, rr.Body.String())
			}
			if !strings.Contains(decodeChatError(t, rr).Error, tt.errText) {
				t.Errorf("error does not contain %q", tt.errText)
			}
		})
	}
}

func TestCreateChatHandlerServiceErrors(t *testing.T) {
	sessionMissing := newTestChatHandler(t, &testutil.DB{Query: sessionExistsHandler(false)})
	rr := doChatRequest(t, sessionMissing.CreateChat, http.MethodPost, `{"content":"hello"}`, testSessionID.String(), testUserID.String())
	if rr.Code != http.StatusNotFound {
		t.Errorf("session missing: status = %d, want 404", rr.Code)
	}

	lookupFailure := newTestChatHandler(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.QueryError(errors.New("boom"))
		},
	})
	rr = doChatRequest(t, lookupFailure.CreateChat, http.MethodPost, `{"content":"hello"}`, testSessionID.String(), testUserID.String())
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("lookup failure: status = %d, want 500", rr.Code)
	}
}

func newTestChatHandler(t *testing.T, db *testutil.DB) *Handler {
	t.Helper()
	return NewHandler(NewService(testRepository(t, db), nil))
}

func TestListChatsHandlerValidation(t *testing.T) {
	h := NewHandler(nil)

	cases := []struct {
		name      string
		target    string
		sessionID string
		userID    string
		status    int
	}{
		{name: "bad page", target: "/?page=x", sessionID: testSessionID.String(), userID: testUserID.String(), status: http.StatusBadRequest},
		{name: "bad limit", target: "/?limit=x", sessionID: testSessionID.String(), userID: testUserID.String(), status: http.StatusBadRequest},
		{name: "missing session", target: "/", sessionID: "", userID: testUserID.String(), status: http.StatusNotFound},
		{name: "unauthenticated", target: "/", sessionID: testSessionID.String(), userID: "", status: http.StatusUnauthorized},
		{name: "invalid user", target: "/", sessionID: testSessionID.String(), userID: "bad", status: http.StatusBadRequest},
		{name: "invalid session", target: "/", sessionID: "bad", userID: testUserID.String(), status: http.StatusBadRequest},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			rctx := chi.NewRouteContext()
			if tt.sessionID != "" {
				rctx.URLParams.Add("sessionID", tt.sessionID)
			}
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			if tt.userID != "" {
				ctx = context.WithValue(ctx, auth.UserIDKey, tt.userID)
				ctx = context.WithValue(ctx, auth.EmailKey, "user@example.com")
			}
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			h.ListChats(rr, req)
			if rr.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tt.status, rr.Body.String())
			}
		})
	}
}

func TestListChatsHandlerServiceErrors(t *testing.T) {
	sessionMissing := newTestChatHandler(t, &testutil.DB{Query: sessionExistsHandler(false)})
	rr := doChatRequest(t, sessionMissing.ListChats, http.MethodGet, "", testSessionID.String(), testUserID.String())
	if rr.Code != http.StatusNotFound {
		t.Errorf("session missing: status = %d, want 404", rr.Code)
	}

	lookupFailure := newTestChatHandler(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return testutil.QueryError(errors.New("boom"))
		},
	})
	rr = doChatRequest(t, lookupFailure.ListChats, http.MethodGet, "", testSessionID.String(), testUserID.String())
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("lookup failure: status = %d, want 500", rr.Code)
	}
}

func TestListChatsHandlerSuccess(t *testing.T) {
	h := newTestChatHandler(t, &testutil.DB{
		Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "SessionExistsByUserID"):
				return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
			case strings.Contains(query, "CountChatsBySessionID"):
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(2)}), nil
			default:
				return testutil.NewRows(chatColumns, chatRowValues(), chatRowValues()), nil
			}
		},
	})

	rr := doChatRequest(t, h.ListChats, http.MethodGet, "", testSessionID.String(), testUserID.String())
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}

	var resp ChatListResponse
	decodeChatResponse(t, rr, &resp)
	if resp.Message != "messages retrieved" {
		t.Errorf("message = %q", resp.Message)
	}
	if resp.Data.SessionID != testSessionID.String() || resp.Data.UserID != testUserID.String() {
		t.Errorf("unexpected ids: %+v", resp.Data)
	}
	if len(resp.Data.Chats) != 2 || resp.Data.Total != 2 || resp.Data.HasMore {
		t.Errorf("unexpected list data: %+v", resp.Data)
	}
}
