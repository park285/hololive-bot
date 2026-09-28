package auth

import (
	"testing"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

func newGenerationTestService(t *testing.T, cacheClient cache.Client) *Service {
	t.Helper()

	config := DefaultConfig()

	config.BcryptCost = bcrypt.MinCost
	config.LoginRateLimitPerMinute = 1000

	service, err := NewService(newTestDB(t), cacheClient, sharedlogging.NewTestLogger(), config)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	return service
}

func registerAndLogin(t *testing.T, service *Service, email string) (*User, string) {
	t.Helper()

	if _, err := service.Register(t.Context(), email, "Password1", "User"); err != nil {
		t.Fatalf("register %s: %v", email, err)
	}

	return login(t, service, email, "Password1")
}

func login(t *testing.T, service *Service, email, password string) (*User, string) {
	t.Helper()

	session, user, err := service.Login(t.Context(), email, password, "127.0.0.1")
	if err != nil {
		t.Fatalf("login %s: %v", email, err)
	}

	return user, session.Token
}

func resetPassword(t *testing.T, service *Service, email, newPassword string) {
	t.Helper()

	resetToken, err := service.RequestPasswordReset(t.Context(), email, "127.0.0.1")
	if err != nil {
		t.Fatalf("request password reset: %v", err)
	}

	if err := service.ResetPassword(t.Context(), resetToken, newPassword); err != nil {
		t.Fatalf("reset password: %v", err)
	}
}

func storedSessionGeneration(t *testing.T, service *Service, userID string) int64 {
	t.Helper()

	var generation int64

	if err := service.db.QueryRow(t.Context(), `SELECT session_generation FROM auth_users WHERE id = $1`, userID).Scan(&generation); err != nil {
		t.Fatalf("load session generation: %v", err)
	}

	return generation
}

func assertSessionKeyGone(t *testing.T, cacheClient cache.Client, token string) {
	t.Helper()

	exists, err := cacheClient.Exists(t.Context(), sessionKeyPrefix+sha256Hex(token))
	if err != nil {
		t.Fatalf("check session key: %v", err)
	}

	if exists {
		t.Fatal("expected rejected session key to be deleted")
	}
}

// reset은 해당 사용자의 기존 세션을 Me·Refresh 모두에서 거부하고 키를 지우지만,
// 다른 사용자 세션과 reset 이후 새로 로그인한 세션은 그대로 쓸 수 있어야 한다.
func TestPasswordReset_RejectsExistingSessionsOnMeAndRefresh(t *testing.T) {
	cacheClient := testutil.NewTestCacheService(t.Context(), t)
	service := newGenerationTestService(t, cacheClient)

	target, meToken := registerAndLogin(t, service, "target@example.com")
	_, refreshToken := login(t, service, "target@example.com", "Password1")
	_, otherToken := registerAndLogin(t, service, "other@example.com")

	resetPassword(t, service, "target@example.com", "NewPassw0rd1")

	if got := storedSessionGeneration(t, service, target.ID); got != 1 {
		t.Fatalf("session generation after reset: got=%d want=1", got)
	}

	_, err := service.Me(t.Context(), meToken)
	assertAuthCode(t, err, CodeUnauthorized)
	assertSessionKeyGone(t, cacheClient, meToken)

	_, err = service.Refresh(t.Context(), refreshToken)
	assertAuthCode(t, err, CodeUnauthorized)
	assertSessionKeyGone(t, cacheClient, refreshToken)

	if _, meErr := service.Me(t.Context(), otherToken); meErr != nil {
		t.Fatalf("other user's session must survive reset (Me): %v", meErr)
	}

	if _, refreshErr := service.Refresh(t.Context(), otherToken); refreshErr != nil {
		t.Fatalf("other user's session must survive reset (Refresh): %v", refreshErr)
	}

	_, freshToken := login(t, service, "target@example.com", "NewPassw0rd1")

	refreshed, err := service.Refresh(t.Context(), freshToken)
	if err != nil {
		t.Fatalf("refresh of post-reset session: %v", err)
	}

	if _, err := service.Me(t.Context(), refreshed.Token); err != nil {
		t.Fatalf("me of refreshed post-reset session: %v", err)
	}
}

// raceRefreshWithReset은 Refresh가 PG 세대를 확인한 뒤 claim하기 직전에 비밀번호 reset을 커밋시킨다.
func raceRefreshWithReset(t *testing.T, service *Service, racingCache *failingCacheClient, token, email, newPassword string) *Session {
	t.Helper()

	resetToken, err := service.RequestPasswordReset(t.Context(), email, "127.0.0.1")
	if err != nil {
		t.Fatalf("request password reset: %v", err)
	}

	racingCache.beforeCompareAndDelete = func() {
		if resetErr := service.ResetPassword(t.Context(), resetToken, newPassword); resetErr != nil {
			t.Errorf("reset password during refresh: %v", resetErr)
		}
	}

	raced, err := service.Refresh(t.Context(), token)
	if err != nil {
		t.Fatalf("refresh racing reset: %v", err)
	}

	if racingCache.beforeCompareAndDelete != nil {
		t.Fatal("expected reset to run between generation check and claim")
	}

	return raced
}

// Refresh의 세대 확인과 claim 사이에 reset이 커밋되면 새 세션은 이전 세대를 담는다.
// 그 세션도 다음 Me·Refresh에서 거부되어야 한다.
func TestRefresh_SessionIssuedAcrossConcurrentResetIsRejected(t *testing.T) {
	baseCache := testutil.NewTestCacheService(t.Context(), t)
	racingCache := &failingCacheClient{Client: baseCache}
	service := newGenerationTestService(t, racingCache)

	_, token := registerAndLogin(t, service, "user@example.com")

	racedForMe := raceRefreshWithReset(t, service, racingCache, token, "user@example.com", "NewPassw0rd1")

	_, err := service.Me(t.Context(), racedForMe.Token)
	assertAuthCode(t, err, CodeUnauthorized)
	assertSessionKeyGone(t, baseCache, racedForMe.Token)

	_, token = login(t, service, "user@example.com", "NewPassw0rd1")

	racedForRefresh := raceRefreshWithReset(t, service, racingCache, token, "user@example.com", "NewPassw0rd2")

	_, err = service.Refresh(t.Context(), racedForRefresh.Token)
	assertAuthCode(t, err, CodeUnauthorized)
	assertSessionKeyGone(t, baseCache, racedForRefresh.Token)

	if _, _, err := service.Login(t.Context(), "user@example.com", "NewPassw0rd2", "127.0.0.1"); err != nil {
		t.Fatalf("login after second reset: %v", err)
	}
}

// migration 231 이전에 발급된 payload에는 세대 필드가 없다. 0으로 해석되어 reset 전까지 유효하고 reset 후 거부된다.
func TestSessionWithoutGenerationField_ValidUntilFirstReset(t *testing.T) {
	cacheClient := testutil.NewTestCacheService(t.Context(), t)
	service := newGenerationTestService(t, cacheClient)

	user, _ := registerAndLogin(t, service, "user@example.com")

	storeLegacySession := func(token string) {
		t.Helper()

		now := time.Now().UTC()
		payload := `{"userId":"` + user.ID + `","expiresAt":"` + now.Add(time.Hour).Format(time.RFC3339Nano) +
			`","createdAt":"` + now.Format(time.RFC3339Nano) + `"}`

		stored, err := cacheClient.SetNX(t.Context(), sessionKeyPrefix+sha256Hex(token), payload, time.Hour)
		if err != nil || !stored {
			t.Fatalf("store legacy session: stored=%v err=%v", stored, err)
		}
	}

	storeLegacySession("sess_legacy_me")
	storeLegacySession("sess_legacy_refresh")

	if _, err := service.Me(t.Context(), "sess_legacy_me"); err != nil {
		t.Fatalf("legacy session must be valid before reset: %v", err)
	}

	resetPassword(t, service, "user@example.com", "NewPassw0rd1")

	_, err := service.Me(t.Context(), "sess_legacy_me")
	assertAuthCode(t, err, CodeUnauthorized)

	_, err = service.Refresh(t.Context(), "sess_legacy_refresh")
	assertAuthCode(t, err, CodeUnauthorized)
}
