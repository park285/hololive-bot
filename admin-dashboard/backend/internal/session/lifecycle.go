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
	normalizeLegacySession(&sess)
	now := time.Now().UTC()
	result, done, err := s.refreshExistingSession(ctx, id, &sess, now)
	if done || err != nil {
		return result, false, err
	}
	return s.refreshCAS(ctx, id, idle, data, &sess, now)
}

func (s *Store) refreshExistingSession(ctx context.Context, id string, sess *Session, now time.Time) (RefreshResult, bool, error) {
	if isAbsolutelyExpiredAt(sess, now) {
		return RefreshResult{Kind: RefreshAbsoluteExpired}, true, s.Delete(ctx, id)
	}
	if sess.RotatedTo != nil {
		result, err := s.refreshResultForRotatedTo(ctx, *sess.RotatedTo)
		return result, true, err
	}
	return RefreshResult{}, false, nil
}

func (s *Store) refreshCAS(ctx context.Context, id string, idle bool, data string, sess *Session, now time.Time) (RefreshResult, bool, error) {
	refreshed := *sess
	refreshed.ExpiresAt = cappedExpiresAt(now, s.refreshTTL(idle), sess.AbsoluteExpiresAt)
	refreshedData, err := json.Marshal(refreshed)
	if err != nil {
		return RefreshResult{}, false, err
	}
	result, err := s.evalInt(ctx, refreshCASScript,
		[]string{sessionKey(id), familyKey(refreshed.FamilyID)},
		[]string{data, string(refreshedData), fmt.Sprint(ttlSeconds(refreshed.ExpiresAt, now)), id})
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
	case -2:
		return RefreshResult{}, false, fmt.Errorf("session family lease points to another token")
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
	if rotated, done, err := s.currentRotation(ctx, oldID, old, now); done || err != nil {
		return rotated, err
	}
	newSession, oldMarker, err := s.buildRotation(old, now)
	if err != nil {
		return nil, err
	}
	result, err := s.rotateExec(ctx, oldID, oldData, &newSession, &oldMarker, now)
	if err != nil {
		return nil, err
	}
	switch result {
	case 1:
		return &newSession, nil
	case 0:
		return s.rotationWinner(ctx, oldID)
	case -1:
		return nil, fmt.Errorf("session family lease points to another token")
	default:
		return nil, fmt.Errorf("unexpected session rotate result: %d", result)
	}
}

func (s *Store) currentRotation(ctx context.Context, oldID string, old *Session, now time.Time) (*Session, bool, error) {
	if isAbsolutelyExpiredAt(old, now) {
		return nil, true, s.Delete(ctx, oldID)
	}
	if old.RotatedTo != nil {
		rotated, err := s.Get(ctx, *old.RotatedTo)
		return rotated, true, err
	}
	if now.Sub(old.LastRotatedAt) < s.cfg.RotationInterval {
		return nil, true, nil
	}
	return nil, false, nil
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
	normalizeLegacySession(&old)
	return oldData, &old, nil
}

func (s *Store) buildRotation(old *Session, now time.Time) (newSession, oldMarker Session, err error) {
	newID, err := auth.GenerateSessionID()
	if err != nil {
		return Session{}, Session{}, err
	}
	newSession = Session{
		ID:                newID,
		FamilyID:          old.FamilyID,
		CreatedAt:         old.CreatedAt,
		ExpiresAt:         cappedExpiresAt(now, s.cfg.ExpiryDuration, old.AbsoluteExpiresAt),
		AbsoluteExpiresAt: old.AbsoluteExpiresAt,
		LastRotatedAt:     now,
	}
	oldMarker = *old
	oldMarker.ExpiresAt = cappedExpiresAt(now, s.cfg.GracePeriod, old.AbsoluteExpiresAt)
	oldMarker.LastRotatedAt = now
	oldMarker.RotatedTo = &newID
	return newSession, oldMarker, nil
}

func (s *Store) rotateExec(ctx context.Context, oldID, oldData string, newSession, oldMarker *Session, now time.Time) (int64, error) {
	newData, err := json.Marshal(*newSession)
	if err != nil {
		return 0, err
	}
	markerData, err := json.Marshal(*oldMarker)
	if err != nil {
		return 0, err
	}
	return s.evalInt(ctx, rotateScript,
		[]string{sessionKey(oldID), sessionKey(newSession.ID), familyKey(newSession.FamilyID)},
		[]string{
			string(newData),
			string(markerData),
			fmt.Sprint(ttlSeconds(newSession.ExpiresAt, now)),
			fmt.Sprint(ttlSeconds(oldMarker.ExpiresAt, now)),
			oldData,
			newSession.ID,
			oldID,
		})
}

func (s *Store) rotationWinner(ctx context.Context, oldID string) (*Session, error) {
	current, err := s.Get(ctx, oldID)
	if err != nil || current == nil || current.RotatedTo == nil {
		return nil, err
	}
	winner, err := s.Get(ctx, *current.RotatedTo)
	if err != nil {
		return nil, err
	}
	if winner == nil || winner.RotatedTo != nil || winner.FamilyID != current.FamilyID {
		return nil, fmt.Errorf("session rotation winner is missing or inconsistent")
	}
	return winner, nil
}

func intResultAllowingNil(resp valkey.ValkeyResult) (value int64, ok bool, err error) {
	if err := resp.Error(); err != nil {
		if util.IsValkeyNil(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	value, err = resp.AsInt64()
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
local session_key = KEYS[1]
local family_key = KEYS[2]
local expected_data = ARGV[1]
local refreshed_data = ARGV[2]
local ttl = tonumber(ARGV[3])
local id = ARGV[4]
local current_data = redis.call('GET', session_key)
if not current_data then return 0 end
if current_data ~= expected_data then return -1 end
local family_current = redis.call('GET', family_key)
if family_current and family_current ~= id then return -2 end
redis.call('SET', session_key, refreshed_data, 'EX', ttl)
redis.call('SET', family_key, id, 'EX', ttl)
return 1
`

const rotateScript = `
local old_key = KEYS[1]
local new_key = KEYS[2]
local family_key = KEYS[3]
local new_data = ARGV[1]
local old_marker_data = ARGV[2]
local new_ttl = tonumber(ARGV[3])
local grace_ttl = tonumber(ARGV[4])
local expected_old_data = ARGV[5]
local new_id = ARGV[6]
local old_id = ARGV[7]
local old_data = redis.call('GET', old_key)
if not old_data then return 0 end
if old_data ~= expected_old_data then return 0 end
local family_current = redis.call('GET', family_key)
if family_current and family_current ~= old_id then return -1 end
redis.call('SET', new_key, new_data, 'EX', new_ttl)
redis.call('SET', old_key, old_marker_data, 'EX', grace_ttl)
redis.call('SET', family_key, new_id, 'EX', new_ttl)
return 1
`
