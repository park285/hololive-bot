package session

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/admin-dashboard/internal/auth"
)

func prepareTestAccount(t *testing.T) TestAccount {
	t.Helper()

	account, credentials, err := NewTestAccount(time.Minute, 10)
	require.NoError(t, err)
	require.True(t, validTestAccountName(account.Username))
	require.Equal(t, account.Username, credentials.Username)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(credentials.Password)))

	return account
}

func TestTestAccountCreationBoundsAndSingleLease(t *testing.T) {
	for _, ttl := range []time.Duration{0, 59 * time.Second, time.Minute + time.Millisecond, MaxTestAccountTTL + time.Second} {
		_, _, err := NewTestAccount(ttl, 10)
		require.Error(t, err)
	}

	_, _, err := NewTestAccount(time.Minute, 9)
	require.Error(t, err)

	store, mr := newTestStore(t)
	account := prepareTestAccount(t)

	var (
		wins atomic.Int32
		wg   sync.WaitGroup
	)

	errs := make(chan error, 8)

	for range 8 {
		wg.Go(func() {
			issueErr := store.IssueTestAccount(t.Context(), account)
			if issueErr == nil {
				wins.Add(1)
			} else if !errors.Is(issueErr, ErrTestAccountExists) {
				errs <- issueErr
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	require.EqualValues(t, 1, wins.Load())

	current, found, err := store.CurrentTestAccount(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, account.Username, current.Username)
	require.Positive(t, mr.TTL(testAccountKey))
	require.LessOrEqual(t, mr.TTL(testAccountKey), time.Minute)

	changed, err := store.RevokeTestAccount(t.Context(), "test-00000000000000000000000000000000")
	require.ErrorIs(t, err, ErrTestAccountMismatch)
	require.False(t, changed)
	require.True(t, mr.Exists(testAccountKey))
}

func TestTestAccountRevocationClosesEveryFamilyAndPreservesAdministrator(t *testing.T) {
	store, _ := newTestStore(t)

	store.cfg.RotationInterval = 0

	account := prepareTestAccount(t)
	require.NoError(t, store.IssueTestAccount(t.Context(), account))

	primary, err := store.Create(t.Context())
	require.NoError(t, err)

	first, found, err := store.CreateTestSession(t.Context(), account)
	require.NoError(t, err)
	require.True(t, found)

	second, found, err := store.CreateTestSession(t.Context(), account)
	require.NoError(t, err)
	require.True(t, found)

	next, rotated, err := store.Rotate(t.Context(), first.ID)
	require.NoError(t, err)
	require.True(t, rotated)
	require.Equal(t, account.Username, next.TestAccount)
	require.Contains(t, next.ID, auth.TestSessionPrefix)
	require.Equal(t, time.Unix(account.ExpiresAtUnix, 0), next.AbsoluteExpiresAt)

	changed, err := store.RevokeTestAccount(t.Context(), account.Username)
	require.NoError(t, err)
	require.True(t, changed)

	for _, sess := range []Session{first, second, next} {
		_, sessionFound, getErr := store.Get(t.Context(), sess.ID)
		require.NoError(t, getErr)
		require.False(t, sessionFound)

		active, familyErr := store.FamilyActive(t.Context(), sess.FamilyID)
		require.NoError(t, familyErr)
		require.False(t, active)

		refresh, refreshErr := store.Refresh(t.Context(), sess.ID, false)
		require.NoError(t, refreshErr)
		require.Equal(t, RefreshMissing, refresh.Kind)

		_, wasRotated, rotateErr := store.Rotate(t.Context(), sess.ID)
		require.NoError(t, rotateErr)
		require.False(t, wasRotated)
	}

	_, found, err = store.Get(t.Context(), primary.ID)
	require.NoError(t, err)
	require.True(t, found)

	changed, err = store.RevokeTestAccount(t.Context(), account.Username)
	require.NoError(t, err)
	require.False(t, changed)

	newAccount := prepareTestAccount(t)
	require.NoError(t, store.IssueTestAccount(t.Context(), newAccount))

	_, found, err = store.Get(t.Context(), next.ID)
	require.NoError(t, err)
	require.False(t, found, "재발급으로 이전 계정의 세션을 되살리지 않습니다")

	_, found, err = store.CreateTestSession(t.Context(), account)
	require.NoError(t, err)
	require.False(t, found, "bcrypt 검사 이후 계정이 교체되면 세션을 만들지 않습니다")
}

func TestTestAccountExpiryAndInvalidRecordsFailClosed(t *testing.T) {
	store, mr := newTestStore(t)
	account := prepareTestAccount(t)
	require.NoError(t, store.IssueTestAccount(t.Context(), account))

	sess, found, err := store.CreateTestSession(t.Context(), account)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, sess.ExpiresAt.After(time.Unix(account.ExpiresAtUnix, 0)))

	for _, malformed := range []string{`{`, `{}`, `{"username":3}`, `{"username":"test-00000000000000000000000000000000","password_hash":"invalid","expires_at_unix":2000000000}`} {
		require.NoError(t, mr.Set(testAccountKey, malformed))

		_, _, recordErr := store.CurrentTestAccount(t.Context())
		require.Error(t, recordErr)

		_, _, recordErr = store.Get(t.Context(), sess.ID)
		require.Error(t, recordErr)
	}

	data, err := jsonv2.Marshal(account)
	require.NoError(t, err)
	require.NoError(t, mr.Set(testAccountKey, string(data)))
	mr.SetTTL(testAccountKey, time.Second)
	mr.FastForward(2 * time.Second)

	_, found, err = store.CurrentTestAccount(t.Context())
	require.NoError(t, err)
	require.False(t, found)

	_, found, err = store.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.False(t, found)
}

func TestTestAccountRevocationWinsPreparedRefreshAndRotation(t *testing.T) {
	store, _ := newTestStore(t)
	account := prepareTestAccount(t)
	require.NoError(t, store.IssueTestAccount(t.Context(), account))

	sess, found, err := store.CreateTestSession(t.Context(), account)
	require.NoError(t, err)
	require.True(t, found)

	raw, found, err := store.getRaw(t.Context(), sess.ID)
	require.NoError(t, err)
	require.True(t, found)

	next, marker, err := store.buildRotation(&sess, time.Now())
	require.NoError(t, err)

	_, err = store.RevokeTestAccount(t.Context(), account.Username)
	require.NoError(t, err)

	result, retry, err := store.refreshCAS(t.Context(), sess.ID, false, raw, &sess, time.Now())
	require.NoError(t, err)
	require.False(t, retry)
	require.Equal(t, RefreshMissing, result.Kind)

	rotated, err := store.rotateExec(t.Context(), sess.ID, raw, &next, &marker, time.Now())
	require.NoError(t, err)
	require.Zero(t, rotated)
}

func TestTestAccountStoreFailuresRemainErrors(t *testing.T) {
	store, mr := newTestStore(t)
	account := prepareTestAccount(t)

	mr.SetError("synthetic unavailable")
	require.Error(t, store.IssueTestAccount(t.Context(), account))

	_, _, err := store.CurrentTestAccount(t.Context())
	require.Error(t, err)

	_, _, err = store.CreateTestSession(t.Context(), account)
	require.Error(t, err)

	_, err = store.RevokeTestAccount(t.Context(), account.Username)
	require.Error(t, err)
}
