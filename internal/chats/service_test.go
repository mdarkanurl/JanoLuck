package chats

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

func sessionExistsHandler(exists bool) func(string, []driver.NamedValue) (driver.Rows, error) {
	return func(string, []driver.NamedValue) (driver.Rows, error) {
		return testutil.NewRows([]string{"exists"}, []driver.Value{exists}), nil
	}
}

func TestVectorString(t *testing.T) {
	tests := []struct {
		name string
		in   []float64
		want string
	}{
		{name: "nil", in: nil, want: "[]"},
		{name: "empty", in: []float64{}, want: "[]"},
		{name: "values", in: []float64{1.5, -2, 0}, want: "[1.5,-2,0]"},
		{name: "fraction", in: []float64{0.25}, want: "[0.25]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vectorString(tt.in); got != tt.want {
				t.Errorf("vectorString(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeMessage(t *testing.T) {
	if got, err := normalizeMessage("  hello  "); err != nil || got != "hello" {
		t.Errorf("normalizeMessage() = (%q, %v), want (hello, nil)", got, err)
	}

	for _, input := range []string{"", "   "} {
		if _, err := normalizeMessage(input); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("normalizeMessage(%q) error = %v, want ErrInvalidInput", input, err)
		}
	}

	if _, err := normalizeMessage(strings.Repeat("a", 8001)); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("long message error = %v, want ErrInvalidInput", err)
	}
	if got, err := normalizeMessage(strings.Repeat("a", 8000)); err != nil || len(got) != 8000 {
		t.Errorf("8000-char message should be accepted, got len=%d err=%v", len(got), err)
	}
}

func TestParseUUID(t *testing.T) {
	id := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if got, err := parseUUID("  "+id.String()+"  ", "id"); err != nil || got != id {
		t.Errorf("parseUUID() = (%s, %v), want (%s, nil)", got, err, id)
	}

	for _, raw := range []string{"", "nope", uuid.Nil.String()} {
		if _, err := parseUUID(raw, "id"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("parseUUID(%q) error = %v, want ErrInvalidInput", raw, err)
		}
	}
}

func TestParseIDs(t *testing.T) {
	userID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	chatID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")

	gotUser, gotChat, err := parseIDs(userID.String(), chatID.String(), "chat id")
	if err != nil || gotUser != userID || gotChat != chatID {
		t.Errorf("parseIDs() = (%s, %s, %v)", gotUser, gotChat, err)
	}

	if _, _, err := parseIDs("bad", chatID.String(), "chat id"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad user error = %v, want ErrInvalidInput", err)
	}
	if _, _, err := parseIDs(userID.String(), "bad", "chat id"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad chat error = %v, want ErrInvalidInput", err)
	}
}

func TestServiceListChatsRejectsInvalidIDs(t *testing.T) {
	svc := NewService(nil, nil)

	if _, err := svc.ListChats(context.Background(), "bad", testSessionID.String(), 1, 20); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad user id: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.ListChats(context.Background(), testUserID.String(), "bad", 1, 20); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad session id: got %v, want ErrInvalidInput", err)
	}
}

func TestServiceListChatsSessionNotFound(t *testing.T) {
	svc := NewService(testRepository(t, &testutil.DB{Query: sessionExistsHandler(false)}), nil)

	_, err := svc.ListChats(context.Background(), testUserID.String(), testSessionID.String(), 1, 20)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("got %v, want ErrSessionNotFound", err)
	}
}

func TestServiceListChatsValidatesRange(t *testing.T) {
	svc := NewService(testRepository(t, &testutil.DB{Query: sessionExistsHandler(true)}), nil)

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
		if _, err := svc.ListChats(context.Background(), testUserID.String(), testSessionID.String(), tc.page, tc.limit); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: got %v, want ErrInvalidInput", tc.name, err)
		}
	}
}

func TestServiceListChatsRepositoryErrors(t *testing.T) {
	t.Run("session lookup", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		}), nil)

		if _, err := svc.ListChats(context.Background(), testUserID.String(), testSessionID.String(), 1, 20); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})

	t.Run("count", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "SessionExistsByUserID") {
					return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
				}
				return testutil.QueryError(errors.New("boom"))
			},
		}), nil)

		if _, err := svc.ListChats(context.Background(), testUserID.String(), testSessionID.String(), 1, 20); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})

	t.Run("list", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
				switch {
				case strings.Contains(query, "SessionExistsByUserID"):
					return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
				case strings.Contains(query, "CountChatsBySessionID"):
					return testutil.NewRows([]string{"count"}, []driver.Value{int64(1)}), nil
				default:
					return testutil.QueryError(errors.New("boom"))
				}
			},
		}), nil)

		if _, err := svc.ListChats(context.Background(), testUserID.String(), testSessionID.String(), 1, 20); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestServiceListChatsSuccess(t *testing.T) {
	svc := NewService(testRepository(t, &testutil.DB{
		Query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			switch {
			case strings.Contains(query, "SessionExistsByUserID"):
				return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
			case strings.Contains(query, "CountChatsBySessionID"):
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(3)}), nil
			default:
				return testutil.NewRows(chatColumns, chatRowValues(), chatRowValues()), nil
			}
		},
	}), nil)

	result, err := svc.ListChats(context.Background(), testUserID.String(), testSessionID.String(), 1, 2)
	if err != nil {
		t.Fatalf("ListChats() error = %v", err)
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if len(result.Chats) != 2 {
		t.Errorf("got %d chats, want 2", len(result.Chats))
	}
}

func TestServiceGetChat(t *testing.T) {
	t.Run("invalid ids", func(t *testing.T) {
		svc := NewService(nil, nil)
		if _, err := svc.GetChat(context.Background(), "bad", testChatID.String()); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("bad user: got %v, want ErrInvalidInput", err)
		}
		if _, err := svc.GetChat(context.Background(), testUserID.String(), "bad"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("bad chat: got %v, want ErrInvalidInput", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(chatColumns, chatRowValues()), nil
			},
		}), nil)

		chat, err := svc.GetChat(context.Background(), testUserID.String(), testChatID.String())
		if err != nil {
			t.Fatalf("GetChat() error = %v", err)
		}
		if chat.ID != testChatID {
			t.Errorf("got id %s, want %s", chat.ID, testChatID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		}), nil)

		if _, err := svc.GetChat(context.Background(), testUserID.String(), testChatID.String()); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})
}

func TestServiceUpdateChatValidation(t *testing.T) {
	svc := NewService(nil, nil)

	_, err := svc.UpdateChat(context.Background(), "bad", testChatID.String(), "x")
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad user: got %v, want ErrInvalidInput", err)
	}
	_, err = svc.UpdateChat(context.Background(), testUserID.String(), "bad", "x")
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad chat: got %v, want ErrInvalidInput", err)
	}

	for _, content := range []string{"", "   ", strings.Repeat("a", 8001)} {
		_, err := svc.UpdateChat(context.Background(), testUserID.String(), testChatID.String(), content)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("content len=%d: got %v, want ErrInvalidInput", len(content), err)
		}
	}
}

func TestServiceUpdateChatSuccess(t *testing.T) {
	svc := NewService(testRepository(t, &testutil.DB{
		Query: func(string, []driver.NamedValue) (driver.Rows, error) {
			values := chatRowValues()
			values[4] = "updated"
			return testutil.NewRows(chatColumns, values), nil
		},
	}), nil)

	chat, err := svc.UpdateChat(context.Background(), testUserID.String(), testChatID.String(), "  updated  ")
	if err != nil {
		t.Fatalf("UpdateChat() error = %v", err)
	}
	if chat.Content != "updated" {
		t.Errorf("got content %q, want updated", chat.Content)
	}
}

func TestServiceDeleteChat(t *testing.T) {
	t.Run("invalid ids", func(t *testing.T) {
		svc := NewService(nil, nil)
		if err := svc.DeleteChat(context.Background(), "bad", testChatID.String()); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("bad user: got %v, want ErrInvalidInput", err)
		}
		if err := svc.DeleteChat(context.Background(), testUserID.String(), "bad"); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("bad chat: got %v, want ErrInvalidInput", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(0)
			},
		}), nil)

		if err := svc.DeleteChat(context.Background(), testUserID.String(), testChatID.String()); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(1)
			},
		}), nil)

		if err := svc.DeleteChat(context.Background(), testUserID.String(), testChatID.String()); err != nil {
			t.Fatalf("DeleteChat() error = %v", err)
		}
	})
}

func TestServiceCreateChatValidation(t *testing.T) {
	svc := NewService(nil, nil)

	if _, err := svc.CreateChat(context.Background(), "bad", testSessionID.String(), "hello"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad user: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.CreateChat(context.Background(), testUserID.String(), "bad", "hello"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad session: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.CreateChat(context.Background(), testUserID.String(), testSessionID.String(), "  "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("blank content: got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.CreateChat(context.Background(), testUserID.String(), testSessionID.String(), strings.Repeat("a", 8001)); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("long content: got %v, want ErrInvalidInput", err)
	}
}

func TestServiceCreateChatSessionLookupErrors(t *testing.T) {
	t.Run("session missing", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{Query: sessionExistsHandler(false)}), nil)

		if _, err := svc.CreateChat(context.Background(), testUserID.String(), testSessionID.String(), "hello"); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("got %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("lookup failure", func(t *testing.T) {
		svc := NewService(testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		}), nil)

		if _, err := svc.CreateChat(context.Background(), testUserID.String(), testSessionID.String(), "hello"); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}
