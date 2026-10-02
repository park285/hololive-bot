package auth

import (
	"os"
	"strings"
	"testing"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

func TestAuthenticationCacheFailuresDoNotLogEmail(t *testing.T) {
	const email = "synthetic.review@example.invalid"

	dir := t.TempDir()
	t.Chdir(dir)

	logger, closer, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
		Dir: dir, Level: "warn", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1,
	}, "auth.log", sharedlogging.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if closer != nil {
		t.Cleanup(func() {
			if closeErr := closer.Close(); closeErr != nil {
				t.Errorf("close logger: %v", closeErr)
			}
		})
	}

	cacheClient, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)
	s := &Service{cacheClient: cacheClient, logger: logger, config: DefaultConfig()}
	s.onLoginFailed(t.Context(), email)

	key := loginFailKeyPrefix + email
	if count, getErr := mini.Get(key); getErr != nil || count != "1" {
		t.Fatalf("original cache key was changed: count=%q error=%v", count, getErr)
	}

	if mini.Exists(privacylog.RedactCacheKey(key)) {
		t.Fatal("redacted diagnostic key must not replace the stored authentication key")
	}

	mini.SetError("synthetic cache failure")
	s.onLoginFailed(t.Context(), email)
	s.onLoginSucceeded(t.Context(), email)

	data, err := os.ReadFile("auth.log")
	if err != nil {
		t.Fatal(err)
	}

	logText := string(data)
	if strings.Contains(logText, email) {
		t.Fatalf("authentication failure log contains plaintext email: %s", logText)
	}

	for _, expected := range []string{
		"login_fail_increment_failed", "login_fail_counter_delete_failed", "account_lock_delete_failed",
		privacylog.RedactCacheKey(key), privacylog.RedactCacheKey(accountLockKeyPrefix + email),
	} {
		if !strings.Contains(logText, expected) {
			t.Errorf("authentication failure diagnostic missing %q", expected)
		}
	}
}

func TestAuthenticationRateLimitErrorsRedactClientIP(t *testing.T) {
	const clientIP = "2001:db8::1"

	cacheClient, mini := testutil.NewTestCacheServiceWithMini(t.Context(), t)
	mini.SetError("synthetic cache failure")

	key := loginRateLimitKeyPrefix + clientIP

	_, err := incrWithTTL(t.Context(), cacheClient, key, time.Minute)
	if err == nil {
		t.Fatal("expected cache error")
	}

	if strings.Contains(err.Error(), clientIP) || !strings.Contains(err.Error(), privacylog.RedactCacheKey(key)) {
		t.Fatalf("rate-limit error must retain only a pseudonymized key: %v", err)
	}
}
