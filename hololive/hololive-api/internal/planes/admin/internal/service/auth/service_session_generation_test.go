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

	return login(t, service, email)
}

func login(t *testing.T, service *Service, email string) (*User, string) {
	t.Helper()

	session, user, err := service.Login(t.Context(), email, "Password1", "127.0.0.1")
	if err != nil {
		t.Fatalf("login %s: %v", email, err)
	}

	return user, session.Token
}

// bumpSessionGeneration은 운영자가 한 사용자의 세션을 폐기할 때처럼 PG 세션 세대를 올린다.
func bumpSessionGeneration(t *testing.T, service *Service, userID string) {
	t.Helper()

	if _, err := service.db.Exec(t.Context(), `UPDATE auth_users SET session_generation = session_generation + 1 WHERE id = $1`, userID); err != nil {
		t.Fatalf("bump session generation: %v", err)
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

// 세대가 오르면 해당 사용자의 기존 세션을 Me·Refresh 모두에서 거부하고 키를 지우지만,
// 다른 사용자 세션과 그 뒤 새로 로그인한 세션은 그대로 쓸 수 있어야 한다.
func TestSessionGenerationBump_RejectsExistingSessionsOnMeAndRefresh(t *testing.T) {
	cacheClient := testutil.NewTestCacheService(t.Context(), t)
	service := newGenerationTestService(t, cacheClient)

	target, meToken := registerAndLogin(t, service, "target@example.com")
	_, refreshToken := login(t, service, "target@example.com")
	_, otherToken := registerAndLogin(t, service, "other@example.com")

	bumpSessionGeneration(t, service, target.ID)

	if got := storedSessionGeneration(t, service, target.ID); got != 1 {
		t.Fatalf("session generation after bump: got=%d want=1", got)
	}

	_, err := service.Me(t.Context(), meToken)
	assertAuthCode(t, err, CodeUnauthorized)
	assertSessionKeyGone(t, cacheClient, meToken)

	_, err = service.Refresh(t.Context(), refreshToken)
	assertAuthCode(t, err, CodeUnauthorized)
	assertSessionKeyGone(t, cacheClient, refreshToken)

	if _, meErr := service.Me(t.Context(), otherToken); meErr != nil {
		t.Fatalf("other user's session must survive bump (Me): %v", meErr)
	}

	if _, refreshErr := service.Refresh(t.Context(), otherToken); refreshErr != nil {
		t.Fatalf("other user's session must survive bump (Refresh): %v", refreshErr)
	}

	_, freshToken := login(t, service, "target@example.com")

	refreshed, err := service.Refresh(t.Context(), freshToken)
	if err != nil {
		t.Fatalf("refresh of post-bump session: %v", err)
	}

	if _, err := service.Me(t.Context(), refreshed.Token); err != nil {
		t.Fatalf("me of refreshed post-bump session: %v", err)
	}
}

// raceRefreshWithGenerationBump은 Refresh가 PG 세대를 확인한 뒤 claim하기 직전에 세대를 올린다.
func raceRefreshWithGenerationBump(t *testing.T, service *Service, racingCache *failingCacheClient, token, userID string) *Session {
	t.Helper()

	racingCache.beforeCompareAndDelete = func() {
		if _, err := service.db.Exec(t.Context(), `UPDATE auth_users SET session_generation = session_generation + 1 WHERE id = $1`, userID); err != nil {
			t.Errorf("bump session generation during refresh: %v", err)
		}
	}

	raced, err := service.Refresh(t.Context(), token)
	if err != nil {
		t.Fatalf("refresh racing generation bump: %v", err)
	}

	if racingCache.beforeCompareAndDelete != nil {
		t.Fatal("expected generation bump to run between generation check and claim")
	}

	return raced
}

// Refresh의 세대 확인과 claim 사이에 세대가 오르면 새 세션은 이전 세대를 담는다.
// 그 세션도 다음 Me·Refresh에서 거부되어야 한다.
func TestRefresh_SessionIssuedAcrossConcurrentGenerationBumpIsRejected(t *testing.T) {
	baseCache := testutil.NewTestCacheService(t.Context(), t)
	racingCache := &failingCacheClient{Client: baseCache}
	service := newGenerationTestService(t, racingCache)

	user, token := registerAndLogin(t, service, "user@example.com")

	racedForMe := raceRefreshWithGenerationBump(t, service, racingCache, token, user.ID)

	_, err := service.Me(t.Context(), racedForMe.Token)
	assertAuthCode(t, err, CodeUnauthorized)
	assertSessionKeyGone(t, baseCache, racedForMe.Token)

	_, token = login(t, service, "user@example.com")

	racedForRefresh := raceRefreshWithGenerationBump(t, service, racingCache, token, user.ID)

	_, err = service.Refresh(t.Context(), racedForRefresh.Token)
	assertAuthCode(t, err, CodeUnauthorized)
	assertSessionKeyGone(t, baseCache, racedForRefresh.Token)

	if got := storedSessionGeneration(t, service, user.ID); got != 2 {
		t.Fatalf("session generation after two bumps: got=%d want=2", got)
	}
}

// migration 231 이전에 발급된 payload에는 세대 필드가 없다. 0으로 해석되어 세대가 처음 오르기 전까지 유효하고 그 뒤 거부된다.
func TestSessionWithoutGenerationField_ValidUntilFirstGenerationBump(t *testing.T) {
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
		t.Fatalf("legacy session must be valid before generation bump: %v", err)
	}

	bumpSessionGeneration(t, service, user.ID)

	_, err := service.Me(t.Context(), "sess_legacy_me")
	assertAuthCode(t, err, CodeUnauthorized)

	_, err = service.Refresh(t.Context(), "sess_legacy_refresh")
	assertAuthCode(t, err, CodeUnauthorized)
}
