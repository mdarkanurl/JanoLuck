package chats

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mdarkanurl/JanoLuck/internal/testutil"
)

var (
	testUserID    = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	testSessionID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	testChatID    = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	testOtherID   = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	testTime      = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

func testRepository(t *testing.T, db *testutil.DB) *Repository {
	t.Helper()
	return NewRepository(testutil.NewQueries(t, db))
}

func chatRowValues() []driver.Value {
	return []driver.Value{
		testChatID.String(),
		testSessionID.String(),
		testUserID.String(),
		RoleUser,
		"hello",
		testTime,
		testTime,
	}
}

var chatColumns = []string{"id", "sessionId", "userId", "role", "content", "create_at", "update_at"}

func TestNewChat(t *testing.T) {
	chat := newChat(
		testChatID,
		uuid.NullUUID{UUID: testSessionID, Valid: true},
		uuid.NullUUID{UUID: testUserID, Valid: true},
		sql.NullString{String: RoleSystem, Valid: true},
		"prediction",
		testTime,
		testTime.Add(time.Minute),
	)

	if chat.ID != testChatID || chat.SessionID != testSessionID || chat.UserID != testUserID {
		t.Errorf("unexpected ids: %+v", chat)
	}
	if chat.Role != RoleSystem || chat.Content != "prediction" {
		t.Errorf("unexpected role/content: %+v", chat)
	}
	if !chat.CreatedAt.Equal(testTime) || !chat.UpdatedAt.Equal(testTime.Add(time.Minute)) {
		t.Errorf("unexpected timestamps: %+v", chat)
	}
}

func TestNewChatUnwrapsInvalidNulls(t *testing.T) {
	chat := newChat(testChatID, uuid.NullUUID{}, uuid.NullUUID{}, sql.NullString{}, "x", testTime, testTime)

	if chat.SessionID != uuid.Nil || chat.UserID != uuid.Nil || chat.Role != "" {
		t.Errorf("expected zero values for null columns, got %+v", chat)
	}
}

func TestRepositorySessionExistsByUserID(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows([]string{"exists"}, []driver.Value{true}), nil
			},
		})

		exists, err := repo.SessionExistsByUserID(context.Background(), testSessionID, testUserID)
		if err != nil {
			t.Fatalf("SessionExistsByUserID() error = %v", err)
		}
		if !exists {
			t.Error("expected session to exist")
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.SessionExistsByUserID(context.Background(), testSessionID, testUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryCreateChat(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(chatColumns, chatRowValues()), nil
			},
		})

		chat, err := repo.CreateChat(context.Background(), testSessionID, testUserID, RoleUser, "hello", "[]")
		if err != nil {
			t.Fatalf("CreateChat() error = %v", err)
		}
		if chat.ID != testChatID || chat.Content != "hello" || chat.Role != RoleUser {
			t.Errorf("unexpected chat: %+v", chat)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.CreateChat(context.Background(), testSessionID, testUserID, RoleUser, "hello", "[]"); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryListChatsBySessionID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(chatColumns, chatRowValues(), chatRowValues()), nil
			},
		})

		chats, err := repo.ListChatsBySessionID(context.Background(), testSessionID, testUserID, 10, 0)
		if err != nil {
			t.Fatalf("ListChatsBySessionID() error = %v", err)
		}
		if len(chats) != 2 {
			t.Fatalf("got %d chats, want 2", len(chats))
		}
	})

	t.Run("empty result is non-nil", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(chatColumns), nil
			},
		})

		chats, err := repo.ListChatsBySessionID(context.Background(), testSessionID, testUserID, 10, 0)
		if err != nil {
			t.Fatalf("ListChatsBySessionID() error = %v", err)
		}
		if chats == nil || len(chats) != 0 {
			t.Errorf("got %#v, want empty non-nil slice", chats)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.ListChatsBySessionID(context.Background(), testSessionID, testUserID, 10, 0); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryListSimilarChatsBySessionID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows([]string{"content"}, []driver.Value{"a"}, []driver.Value{"b"}), nil
			},
		})

		chats, err := repo.ListSimilarChatsBySessionID(context.Background(), testSessionID, testChatID, "[]", 10)
		if err != nil {
			t.Fatalf("ListSimilarChatsBySessionID() error = %v", err)
		}
		if len(chats) != 2 || chats[0] != "a" || chats[1] != "b" {
			t.Errorf("got %v, want [a b]", chats)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.ListSimilarChatsBySessionID(context.Background(), testSessionID, testChatID, "[]", 10); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryCountChatsBySessionID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows([]string{"count"}, []driver.Value{int64(7)}), nil
			},
		})

		total, err := repo.CountChatsBySessionID(context.Background(), testSessionID, testUserID)
		if err != nil {
			t.Fatalf("CountChatsBySessionID() error = %v", err)
		}
		if total != 7 {
			t.Errorf("got %d, want 7", total)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.CountChatsBySessionID(context.Background(), testSessionID, testUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryGetChatByID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.NewRows(chatColumns, chatRowValues()), nil
			},
		})

		chat, err := repo.GetChatByID(context.Background(), testChatID, testUserID)
		if err != nil {
			t.Fatalf("GetChatByID() error = %v", err)
		}
		if chat.ID != testChatID {
			t.Errorf("got id %s, want %s", chat.ID, testChatID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})

		if _, err := repo.GetChatByID(context.Background(), testChatID, testUserID); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.GetChatByID(context.Background(), testChatID, testUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryUpdateChatContent(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				values := chatRowValues()
				values[4] = "updated"
				return testutil.NewRows(chatColumns, values), nil
			},
		})

		chat, err := repo.UpdateChatContent(context.Background(), testChatID, testUserID, "updated")
		if err != nil {
			t.Fatalf("UpdateChatContent() error = %v", err)
		}
		if chat.Content != "updated" {
			t.Errorf("got content %q, want updated", chat.Content)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(sql.ErrNoRows)
			},
		})

		if _, err := repo.UpdateChatContent(context.Background(), testChatID, testUserID, "updated"); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return testutil.QueryError(errors.New("boom"))
			},
		})

		if _, err := repo.UpdateChatContent(context.Background(), testChatID, testUserID, "updated"); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}

func TestRepositoryDeleteChat(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(1)
			},
		})

		if err := repo.DeleteChat(context.Background(), testChatID, testUserID); err != nil {
			t.Fatalf("DeleteChat() error = %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.Result(0)
			},
		})

		if err := repo.DeleteChat(context.Background(), testChatID, testUserID); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		repo := testRepository(t, &testutil.DB{
			Exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return testutil.ExecError(errors.New("boom"))
			},
		})

		if err := repo.DeleteChat(context.Background(), testChatID, testUserID); !errors.Is(err, ErrInternal) {
			t.Errorf("got %v, want ErrInternal", err)
		}
	})
}
