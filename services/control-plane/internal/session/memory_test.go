package session

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreLifecycle(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	sess := Session{
		ID:        "abc",
		Subject:   "u1",
		Email:     "u1@x.io",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := s.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, ok := s.Get(ctx, "abc")
	if !ok || got.Email != "u1@x.io" {
		t.Fatalf("Get: got=%+v ok=%v", got, ok)
	}
	_ = s.Delete(ctx, "abc")
	if _, ok := s.Get(ctx, "abc"); ok {
		t.Fatalf("Get after Delete: expected not found")
	}
}

func TestMemoryStoreExpires(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	_ = s.Create(ctx, Session{
		ID:        "exp",
		Subject:   "u2",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Hour),
	})
	if _, ok := s.Get(ctx, "exp"); ok {
		t.Fatalf("expired session should not be returned")
	}
}
