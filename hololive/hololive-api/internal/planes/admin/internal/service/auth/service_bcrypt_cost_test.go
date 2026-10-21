package auth

import (
	"testing"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"golang.org/x/crypto/bcrypt"
)

func storedPasswordHash(t *testing.T, service *Service) string {
	t.Helper()

	var passwordHash string

	if err := service.db.QueryRow(
		t.Context(),
		`SELECT password_hash FROM auth_users WHERE email = $1`,
		normalizeEmail("user@example.com"),
	).Scan(&passwordHash); err != nil {
		t.Fatalf("load user: %v", err)
	}

	return passwordHash
}

// DefaultConfig의 BcryptCost는 안전 기본값(>=12)이어야 한다.
func TestDefaultConfig_BcryptCostSafeDefault(t *testing.T) {
	t.Parallel()

	if got := DefaultConfig().BcryptCost; got < 12 {
		t.Fatalf("DefaultConfig().BcryptCost = %d, want >= 12", got)
	}
}

// Register는 config.BcryptCost로 비밀번호를 해시해야 한다.
func TestRegister_UsesConfiguredBcryptCost(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	cfg := DefaultConfig()

	// 운영 기본값과 다른 비용이어야 설정값 대신 기본값을 쓰는 회귀를 구분한다.
	// 낮은 비용은 race 실행에서 회당 수 초가 걸리는 비용 12 해시를 피한다.
	cfg.BcryptCost = bcrypt.MinCost + 1

	service, err := NewService(db, nil, sharedlogging.NewTestLogger(), cfg)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	if _, registerErr := service.Register(t.Context(), "user@example.com", "Password1", "User"); registerErr != nil {
		t.Fatalf("register failed: %v", registerErr)
	}

	hash := storedPasswordHash(t, service)

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}

	if cost != cfg.BcryptCost {
		t.Fatalf("register hash cost = %d, want %d", cost, cfg.BcryptCost)
	}

	// 해시/검증 일관성: 동일 비밀번호가 검증을 통과해야 한다.
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("Password1")); err != nil {
		t.Fatalf("password verification failed: %v", err)
	}
}
