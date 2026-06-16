package auth

import (
	"context"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/hololive-shared/pkg/testutil"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

type comparePasswordSpy struct {
	mu    sync.Mutex
	calls [][2][]byte
}

func (s *comparePasswordSpy) wrap(next func(hashedPassword, password []byte) error) func(hashedPassword, password []byte) error {
	return func(hashedPassword, password []byte) error {
		s.mu.Lock()
		s.calls = append(s.calls, [2][]byte{append([]byte(nil), hashedPassword...), append([]byte(nil), password...)})
		s.mu.Unlock()
		return next(hashedPassword, password)
	}
}

func (s *comparePasswordSpy) snapshot() [][2][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2][]byte(nil), s.calls...)
}

func TestLogin_UnknownEmailRunsBcryptComparison(t *testing.T) {
	db := newTestDB(t)
	cacheClient := testutil.NewTestCacheService(t, context.Background())

	cfg := DefaultConfig()
	cfg.BcryptCost = 12

	service, err := NewService(context.Background(), db, cacheClient, sharedlogging.NewTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	spy := &comparePasswordSpy{}
	original := comparePassword
	comparePassword = spy.wrap(original)
	t.Cleanup(func() { comparePassword = original })

	_, _, err = service.Login(context.Background(), "missing@example.com", "Password1", "127.0.0.1")
	if err == nil {
		t.Fatalf("expected invalid credentials for unknown email")
	}
	assertAuthCode(t, err, CodeInvalidCredentials)

	calls := spy.snapshot()
	if len(calls) != 1 {
		t.Fatalf("unknown-email login bcrypt comparisons = %d, want 1", len(calls))
	}

	gotHash, gotPassword := calls[0][0], calls[0][1]
	if string(gotPassword) != "Password1" {
		t.Fatalf("compared password = %q, want %q", gotPassword, "Password1")
	}
	if cost, costErr := bcrypt.Cost(gotHash); costErr != nil || cost != cfg.BcryptCost {
		t.Fatalf("dummy hash cost = %d (err=%v), want %d", cost, costErr, cfg.BcryptCost)
	}
	if string(gotHash) != string(service.loginDummyHash) {
		t.Fatalf("unknown-email path must compare against the precomputed dummy hash")
	}
}
