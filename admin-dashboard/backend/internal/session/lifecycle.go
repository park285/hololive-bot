package session

import (
	"context"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/json"
)

func (s *Store) Refresh(ctx context.Context, id string, idle bool) (RefreshResult, error) {
	for range 2 {
		result, retry, err := s.refreshOnce(ctx, id, idle)
		if !retry {
			return result, err
		}
	}
	return s.refreshAfterCASMiss(ctx, id, idle)
}

func (s *Store) refreshOnce(ctx context.Context, id string, idle bool) (RefreshResult, bool, error) {
	data, ok, err := s.getRaw(ctx, id)
	if err != nil || !ok {
		return RefreshResult{Kind: RefreshMissing}, false, err
	}
	var sess Session
	if err := json.Unmarshal([]byte(data), &sess); err != nil {
		return RefreshResult{}, false, err
	}
	now := time.Now().UTC()
	if isAbsolutelyExpiredAt(sess, now) {
		_ = s.Delete(ctx, id)
		return RefreshResult{Kind: RefreshAbsoluteExpired}, false, nil
	}
	if sess.RotatedTo != nil {
		result, err := s.refreshResultForRotatedTo(ctx, *sess.RotatedTo)
		return result, false, err
	}
	refreshed := sess
	refreshed.ExpiresAt = cappedExpiresAt(now, s.refreshTTL(idle), sess.AbsoluteExpiresAt)
	refreshedData, err := json.Marshal(refreshed)
	if err != nil {
		return RefreshResult{}, false, err
	}
	result, err := s.evalInt(ctx, refreshCASScript, []string{sessionKey(id)}, []string{data, string(refreshedData), fmt.Sprint(ttlSeconds(refreshed.ExpiresAt, now))})
	if err != nil {
		return RefreshResult{}, false, err
	}
	return refreshCASOutcome(result, refreshSuccessResult(idle, &refreshed))
}

func (s *Store) refreshTTL(idle bool) time.Duration {
	if idle {
		return s.cfg.IdleSessionTTL
	}
	return s.cfg.ExpiryDuration
}

func refreshSuccessResult(idle bool, refreshed *Session) RefreshResult {
	if idle {
		return RefreshResult{Kind: RefreshIdleShortened}
	}
	return RefreshResult{Kind: RefreshRefreshed, Session: refreshed}
}

func refreshCASOutcome(result int64, success RefreshResult) (RefreshResult, bool, error) {
	switch result {
	case 1:
		return success, false, nil
	case 0:
		return RefreshResult{Kind: RefreshMissing}, false, nil
	case -1:
		return RefreshResult{}, true, nil
	default:
		return RefreshResult{}, false, fmt.Errorf("unexpected session refresh CAS result: %d", result)
	}
}

func (s *Store) refreshAfterCASMiss(ctx context.Context, id string, idle bool) (RefreshResult, error) {
	current, err := s.Get(ctx, id)
	if err != nil || current == nil {
		return RefreshResult{Kind: RefreshMissing}, err
	}
	if current.RotatedTo != nil {
		return s.refreshResultForRotatedTo(ctx, *current.RotatedTo)
	}
	if idle {
		return RefreshResult{}, fmt.Errorf("idle session refresh CAS did not converge")
	}
	return RefreshResult{Kind: RefreshRefreshed, Session: current}, nil
}

func (s *Store) Rotate(ctx context.Context, oldID string) (*Session, error) {
	oldData, old, err := s.rotateSource(ctx, oldID)
	if err != nil || old == nil {
		return nil, err
	}
	now := time.Now().UTC()
	if isAbsolutelyExpiredAt(*old, now) {
		_ = s.Delete(ctx, oldID)
		return nil, nil
	}
	if old.RotatedTo != nil {
		return s.Get(ctx, *old.RotatedTo)
	}
	if now.Sub(old.LastRotatedAt) < s.cfg.RotationInterval {
		return nil, nil
	}
	newSession, oldMarker, err := s.buildRotation(*old, now)
	if err != nil {
		return nil, err
	}
	rotated, err := s.rotateExec(ctx, oldID, oldData, newSession, oldMarker, now)
	if err != nil || !rotated {
		return nil, err
	}
	return &newSession, nil
}

func (s *Store) rotateSource(ctx context.Context, oldID string) (string, *Session, error) {
	oldData, ok, err := s.getRaw(ctx, oldID)
	if err != nil || !ok {
		return "", nil, err
	}
	var old Session
	if err := json.Unmarshal([]byte(oldData), &old); err != nil {
		return "", nil, err
	}
	return oldData, &old, nil
}

func (s *Store) buildRotation(old Session, now time.Time) (Session, Session, error) {
	newID, err := auth.GenerateSessionID()
	if err != nil {
		return Session{}, Session{}, err
	}
	newSession := Session{
		ID:                newID,
		CreatedAt:         old.CreatedAt,
		ExpiresAt:         cappedExpiresAt(now, s.cfg.ExpiryDuration, old.AbsoluteExpiresAt),
		AbsoluteExpiresAt: old.AbsoluteExpiresAt,
		LastRotatedAt:     now,
	}
	oldMarker := old
	oldMarker.ExpiresAt = cappedExpiresAt(now, s.cfg.GracePeriod, old.AbsoluteExpiresAt)
	oldMarker.LastRotatedAt = now
	oldMarker.RotatedTo = &newID
	return newSession, oldMarker, nil
}

func (s *Store) rotateExec(ctx context.Context, oldID, oldData string, newSession, oldMarker Session, now time.Time) (bool, error) {
	newData, err := json.Marshal(newSession)
	if err != nil {
		return false, err
	}
	markerData, err := json.Marshal(oldMarker)
	if err != nil {
		return false, err
	}
	resp := s.client.Do(ctx, s.client.B().Eval().Script(rotateScript).Numkeys(2).
		Key(sessionKey(oldID), sessionKey(newSession.ID)).
		Arg(string(newData), string(markerData), fmt.Sprint(ttlSeconds(newSession.ExpiresAt, now)), fmt.Sprint(ttlSeconds(oldMarker.ExpiresAt, now)), oldData).
		Build())
	result, ok, err := intResultAllowingNil(resp)
	if err != nil {
		return false, err
	}
	return ok && result == 1, nil
}

func intResultAllowingNil(resp valkey.ValkeyResult) (int64, bool, error) {
	if err := resp.Error(); err != nil {
		if util.IsValkeyNil(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	value, err := resp.AsInt64()
	if err != nil {
		if util.IsValkeyNil(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return value, true, nil
}

func (s *Store) refreshResultForRotatedTo(ctx context.Context, rotatedTo string) (RefreshResult, error) {
	replacement, err := s.Get(ctx, rotatedTo)
	if err != nil {
		return RefreshResult{}, err
	}
	if replacement == nil {
		return RefreshResult{Kind: RefreshNotRefreshable}, nil
	}
	return RefreshResult{Kind: RefreshRotated, Session: replacement}, nil
}

const refreshCASScript = `
local key = KEYS[1]
local expected_data = ARGV[1]
local refreshed_data = ARGV[2]
local ttl = tonumber(ARGV[3])
local current_data = redis.call('GET', key)
if not current_data then return 0 end
if current_data ~= expected_data then return -1 end
redis.call('SET', key, refreshed_data, 'EX', ttl)
return 1
`

const rotateScript = `
local old_key = KEYS[1]
local new_key = KEYS[2]
local new_data = ARGV[1]
local old_marker_data = ARGV[2]
local new_ttl = tonumber(ARGV[3])
local grace_ttl = tonumber(ARGV[4])
local expected_old_data = ARGV[5]
local old_data = redis.call('GET', old_key)
if not old_data then return nil end
if old_data ~= expected_old_data then return nil end
redis.call('SET', new_key, new_data, 'EX', new_ttl)
redis.call('SET', old_key, old_marker_data, 'EX', grace_ttl)
return 1
`
