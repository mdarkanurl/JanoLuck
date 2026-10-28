package sessions

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestParsePaginationDefaults(t *testing.T) {
	r := httptest.NewRequest("GET", "/sessions", nil)

	page, limit, ok := parsePagination(httptest.NewRecorder(), r)
	if !ok {
		t.Fatal("expected defaults to parse")
	}
	if page != DefaultPage || limit != DefaultLimit {
		t.Fatalf("got page=%d limit=%d, want page=%d limit=%d", page, limit, DefaultPage, DefaultLimit)
	}
}

func TestParsePaginationValues(t *testing.T) {
	r := httptest.NewRequest("GET", "/sessions?page=3&limit=5", nil)

	page, limit, ok := parsePagination(httptest.NewRecorder(), r)
	if !ok {
		t.Fatal("expected values to parse")
	}
	if page != 3 || limit != 5 {
		t.Fatalf("got page=%d limit=%d, want page=3 limit=5", page, limit)
	}
}

func TestParsePaginationRejectsNonIntegers(t *testing.T) {
	for _, raw := range []string{"/sessions?page=abc", "/sessions?limit=5.5", "/sessions?limit=two"} {
		r := httptest.NewRequest("GET", raw, nil)
		rec := httptest.NewRecorder()

		if _, _, ok := parsePagination(rec, r); ok {
			t.Errorf("%s: expected parse to fail", raw)
		}
		if rec.Code != 400 {
			t.Errorf("%s: got status %d, want 400", raw, rec.Code)
		}
	}
}

func TestListSessionsValidatesRange(t *testing.T) {
	svc := NewService(nil, nil) // range rules fail before any repo call
	userID := "a4e20000-0000-4000-8000-000000000000"
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		page  int
		limit int
	}{
		{"zero page", 0, 20},
		{"negative page", -1, 20},
		{"page above max", MaxPage + 1, 20},
		{"zero limit", 1, 0},
		{"negative limit", 1, -1},
		{"limit above max", 1, MaxLimit + 1},
	} {
		if _, err := svc.ListSessions(ctx, userID, tc.page, tc.limit); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: got %v, want ErrInvalidInput", tc.name, err)
		}
	}

	if _, err := svc.ListSessions(ctx, "not-a-uuid", 1, 20); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad user id: got %v, want ErrInvalidInput", err)
	}
}
