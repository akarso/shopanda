package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type failingFailuresStore struct {
	auth.AttemptStore
	failCheck bool
}

func (s *failingFailuresStore) Failures(ctx context.Context, key string) (int, error) {
	if s.failCheck {
		return 0, errors.New("cache unavailable")
	}
	return s.AttemptStore.Failures(ctx, key)
}

func TestLogin_LockoutCheckFailsOpenOnStoreError(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	inner := auth.NewMemoryAttemptStore(100)
	store := &failingFailuresStore{AttemptStore: inner, failCheck: true}
	svc.SetLockout(auth.LockoutSettings{
		Enabled:     true,
		MaxFailures: 3,
		Window:      time.Minute,
	}, store)

	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "open@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	out, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "open@example.com", Password: "password123", ClientIP: "3.3.3.3",
	})
	if err != nil {
		t.Fatalf("login should fail open on lockout check error, got %v", err)
	}
	if out.Token == "" {
		t.Fatal("expected token when lockout store is unavailable")
	}
}

type failingResetStore struct {
	auth.AttemptStore
}

func (s *failingResetStore) Reset(ctx context.Context, key string, window time.Duration) error {
	return errors.New("cache delete failed")
}

func TestLogin_LockoutResetFailureFailsOpen(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	inner := auth.NewMemoryAttemptStore(100)
	store := &failingResetStore{AttemptStore: inner}
	svc.SetLockout(auth.LockoutSettings{
		Enabled:     true,
		MaxFailures: 5,
		Window:      time.Minute,
	}, store)

	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "resetfail@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, _ = inner.Increment(context.Background(), auth.LockoutKey("4.4.4.4", "resetfail@example.com"), time.Minute)

	out, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "resetfail@example.com", Password: "password123", ClientIP: "4.4.4.4",
	})
	if err != nil {
		t.Fatalf("login should fail open when lockout reset fails, got %v", err)
	}
	if out.Token == "" {
		t.Fatal("expected token despite reset failure")
	}
}


func TestLockoutKey_IPAndAccount(t *testing.T) {
	got := auth.LockoutKey("1.2.3.4", "a@example.com")
	want := "1.2.3.4|a@example.com"
	if got != want {
		t.Fatalf("LockoutKey = %q, want %q", got, want)
	}
	if auth.LockoutKey("", "a@example.com") != "unknown|a@example.com" {
		t.Fatalf("empty IP should use unknown, got %q", auth.LockoutKey("", "a@example.com"))
	}
}

func TestMemoryAttemptStore_IncrementTTLAndReset(t *testing.T) {
	store := auth.NewMemoryAttemptStore(100)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store.SetNowFunc(func() time.Time { return now })
	ctx := context.Background()
	key := "10.0.0.1|user@example.com"
	window := 50 * time.Millisecond

	n, err := store.Increment(ctx, key, window)
	if err != nil || n != 1 {
		t.Fatalf("Increment #1 = (%d, %v), want (1, nil)", n, err)
	}
	n, err = store.Increment(ctx, key, window)
	if err != nil || n != 2 {
		t.Fatalf("Increment #2 = (%d, %v), want (2, nil)", n, err)
	}
	got, err := store.Failures(ctx, key)
	if err != nil || got != 2 {
		t.Fatalf("Failures = (%d, %v), want (2, nil)", got, err)
	}

	now = now.Add(window + time.Millisecond)
	got, err = store.Failures(ctx, key)
	if err != nil || got != 0 {
		t.Fatalf("after TTL Failures = (%d, %v), want (0, nil)", got, err)
	}

	_, _ = store.Increment(ctx, key, time.Minute)
	if err := store.Reset(ctx, key, time.Minute); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	got, err = store.Failures(ctx, key)
	if err != nil || got != 0 {
		t.Fatalf("after Reset Failures = (%d, %v), want (0, nil)", got, err)
	}
}

func TestLogin_LockoutAfterFailuresAndResetOnSuccess(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	store := auth.NewMemoryAttemptStore(100)
	svc.SetLockout(auth.LockoutSettings{
		Enabled:     true,
		MaxFailures: 3,
		Window:      time.Minute,
	}, store)

	_, err := svc.Register(context.Background(), auth.RegisterInput{
		Email: "lock@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := 0; i < 2; i++ {
		_, err := svc.Login(context.Background(), auth.LoginInput{
			Email: "lock@example.com", Password: "wrong", ClientIP: "9.9.9.9",
		})
		if !apperror.Is(err, apperror.CodeUnauthorized) {
			t.Fatalf("failure %d: want unauthorized, got %v", i+1, err)
		}
	}

	_, err = svc.Login(context.Background(), auth.LoginInput{
		Email: "lock@example.com", Password: "wrong", ClientIP: "9.9.9.9",
	})
	if !apperror.Is(err, apperror.CodeRateLimited) {
		t.Fatalf("3rd failure: want rate_limited, got %v", err)
	}

	_, err = svc.Login(context.Background(), auth.LoginInput{
		Email: "lock@example.com", Password: "password123", ClientIP: "9.9.9.9",
	})
	if !apperror.Is(err, apperror.CodeRateLimited) {
		t.Fatalf("while locked: want rate_limited, got %v", err)
	}

	// Different IP must not share the lockout key (no account-only DoS).
	out, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "lock@example.com", Password: "password123", ClientIP: "8.8.8.8",
	})
	if err != nil {
		t.Fatalf("other IP login: %v", err)
	}
	if out.Token == "" {
		t.Fatal("expected token for other IP")
	}

	_ = store.Reset(context.Background(), auth.LockoutKey("9.9.9.9", "lock@example.com"), time.Minute)
	out, err = svc.Login(context.Background(), auth.LoginInput{
		Email: "lock@example.com", Password: "password123", ClientIP: "9.9.9.9",
	})
	if err != nil {
		t.Fatalf("after reset login: %v", err)
	}
	if out.Token == "" {
		t.Fatal("expected token after reset")
	}
}

func TestLogin_LockoutResetOnSuccess(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	store := auth.NewMemoryAttemptStore(100)
	svc.SetLockout(auth.LockoutSettings{
		Enabled:     true,
		MaxFailures: 5,
		Window:      time.Minute,
	}, store)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "ok@example.com", Password: "password123",
	})

	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "ok@example.com", Password: "wrong", ClientIP: "1.1.1.1",
	})
	if !apperror.Is(err, apperror.CodeUnauthorized) {
		t.Fatalf("want unauthorized, got %v", err)
	}

	_, err = svc.Login(context.Background(), auth.LoginInput{
		Email: "ok@example.com", Password: "password123", ClientIP: "1.1.1.1",
	})
	if err != nil {
		t.Fatalf("success login: %v", err)
	}

	got, err := store.Failures(context.Background(), auth.LockoutKey("1.1.1.1", "ok@example.com"))
	if err != nil || got != 0 {
		t.Fatalf("Failures after success = (%d, %v), want (0, nil)", got, err)
	}
}

func TestLogin_LockoutTTLExpiry(t *testing.T) {
	repo := newMockRepo()
	svc := newTestService(repo)
	store := auth.NewMemoryAttemptStore(100)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store.SetNowFunc(func() time.Time { return now })
	window := time.Minute
	svc.SetLockout(auth.LockoutSettings{
		Enabled:     true,
		MaxFailures: 2,
		Window:      window,
	}, store)

	_, _ = svc.Register(context.Background(), auth.RegisterInput{
		Email: "ttl@example.com", Password: "password123",
	})

	for i := 0; i < 2; i++ {
		_, _ = svc.Login(context.Background(), auth.LoginInput{
			Email: "ttl@example.com", Password: "wrong", ClientIP: "2.2.2.2",
		})
	}
	_, err := svc.Login(context.Background(), auth.LoginInput{
		Email: "ttl@example.com", Password: "password123", ClientIP: "2.2.2.2",
	})
	if !apperror.Is(err, apperror.CodeRateLimited) {
		t.Fatalf("want locked, got %v", err)
	}

	now = now.Add(window + time.Second)
	_, err = svc.Login(context.Background(), auth.LoginInput{
		Email: "ttl@example.com", Password: "password123", ClientIP: "2.2.2.2",
	})
	if err != nil {
		t.Fatalf("after TTL: %v", err)
	}
}

func TestNewAttemptStore_Modes(t *testing.T) {
	if _, err := auth.NewAttemptStore("memory", nil, nil); err != nil {
		t.Fatalf("memory: %v", err)
	}
	if _, err := auth.NewAttemptStore("cache", nil, nil); err == nil {
		t.Fatal("cache without backend should error")
	}
	if _, err := auth.NewAttemptStore("weird", nil, nil); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("want unsupported error, got %v", err)
	}
}
