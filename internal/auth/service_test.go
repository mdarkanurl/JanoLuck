package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRefreshStore struct {
	calls []string

	storeUserID  string
	storeHash    string
	deleteUserID string
	deleteHash   string

	storeErr  error
	deleteErr error
}

func (f *fakeRefreshStore) StoreRefreshToken(_ context.Context, userID, refreshHash string, _ time.Duration) error {
	f.calls = append(f.calls, "store")
	f.storeUserID = userID
	f.storeHash = refreshHash
	return f.storeErr
}

func (f *fakeRefreshStore) DeleteRefreshToken(_ context.Context, userID, refreshHash string) error {
	f.calls = append(f.calls, "delete")
	f.deleteUserID = userID
	f.deleteHash = refreshHash
	return f.deleteErr
}

func newTokenTestService(store refreshTokenStore) *Service {
	return &Service{
		tokens:       newTestTokens(time.Minute, time.Hour),
		refreshStore: store,
	}
}

func TestMintTokenPairStoresNewTokenBeforeRevokingOld(t *testing.T) {
	store := &fakeRefreshStore{}
	svc := newTokenTestService(store)

	tokens, err := svc.mintTokenPair(context.Background(), "user-1", "user@example.com", "old-hash")
	if err != nil {
		t.Fatalf("mintTokenPair() error = %v", err)
	}

	if len(store.calls) != 2 || store.calls[0] != "store" || store.calls[1] != "delete" {
		t.Fatalf("calls = %v, want [store delete]", store.calls)
	}
	if store.storeUserID != "user-1" {
		t.Errorf("stored userID = %q, want user-1", store.storeUserID)
	}
	if want := HashRefreshToken(tokens.RefreshToken); store.storeHash != want {
		t.Errorf("stored hash = %q, want the hash of the returned token", store.storeHash)
	}
	if store.deleteUserID != "user-1" || store.deleteHash != "old-hash" {
		t.Errorf("deleted (%q, %q), want (user-1, old-hash)", store.deleteUserID, store.deleteHash)
	}
}

func TestMintTokenPairKeepsOldTokenWhenStoreFails(t *testing.T) {
	store := &fakeRefreshStore{storeErr: errors.New("redis down")}
	svc := newTokenTestService(store)

	if _, err := svc.mintTokenPair(context.Background(), "user-1", "user@example.com", "old-hash"); err == nil {
		t.Fatal("expected the store error to be returned")
	}

	if len(store.calls) != 1 || store.calls[0] != "store" {
		t.Fatalf("calls = %v, want only the failed store", store.calls)
	}
}

func TestMintTokenPairReturnsNewTokenWhenRevokeFails(t *testing.T) {
	store := &fakeRefreshStore{deleteErr: errors.New("redis down")}
	svc := newTokenTestService(store)

	tokens, err := svc.mintTokenPair(context.Background(), "user-1", "user@example.com", "old-hash")
	if err != nil {
		t.Fatalf("mintTokenPair() error = %v, want the stored token to be returned", err)
	}
	if tokens == nil || tokens.RefreshToken == "" {
		t.Error("expected a usable token pair")
	}
}

func TestMintTokenPairWithoutOldTokenOnlyStores(t *testing.T) {
	store := &fakeRefreshStore{}
	svc := newTokenTestService(store)

	if _, err := svc.mintTokenPair(context.Background(), "user-1", "user@example.com", ""); err != nil {
		t.Fatalf("mintTokenPair() error = %v", err)
	}

	if len(store.calls) != 1 || store.calls[0] != "store" {
		t.Fatalf("calls = %v, want [store]", store.calls)
	}
}
