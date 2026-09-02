package session

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/admin-dashboard/internal/auth"
)

func (s *Store) Refresh(ctx context.Context, id string, idle bool) (RefreshResult, error) {
	for range 2 {
		result, retry, err := s.refreshOnce(ctx, id, idle)
		if retry {
			continue
		}

		if err != nil {
			return result, fmt.Errorf("refresh once: %w", err)
		}

		return result, nil
	}

	out, err := s.refreshAfterCASMiss(ctx, id, idle)
	if err != nil {
		return out, fmt.Errorf("refresh after CAS miss: %w", err)
	}

	return out, nil
}

func (s *Store) refreshOnce(ctx context.Context, id string, idle bool) (RefreshResult, bool, error) {
	data, ok, err := s.getRaw(ctx, id)
	if err != nil {
		return RefreshResult{Kind: RefreshMissing}, false, fmt.Errorf("get raw: %w", err)
	}

	if !ok {
		return RefreshResult{Kind: RefreshMissing}, false, nil
	}

	var sess Session

	if unmarshalErr := jsonv2.Unmarshal([]byte(data), &sess); unmarshalErr != nil {
		return RefreshResult{}, false, fmt.Errorf("unmarshal: %w", unmarshalErr)
	}

	normalizeLegacySession(&sess)

	now := time.Now().UTC()

	result, done, err := s.refreshExistingSession(ctx, id, &sess, now)
	if err != nil {
		return result, false, fmt.Errorf("refresh existing session: %w", err)
	}

	if done {
		return result, false, nil
	}

	out1, out2, err := s.refreshCAS(ctx, id, idle, data, &sess, now)
	if err != nil {
		return out1, out2, fmt.Errorf("refresh CAS: %w", err)
	}

	return out1, out2, nil
}

func (s *Store) refreshExistingSession(ctx context.Context, id string, sess *Session, now time.Time) (RefreshResult, bool, error) {
	if isAbsolutelyExpiredAt(sess, now) {
		if err := s.Delete(ctx, id); err != nil {
			return RefreshResult{Kind: RefreshAbsoluteExpired}, true, fmt.Errorf("delete: %w", err)
		}

		return RefreshResult{Kind: RefreshAbsoluteExpired}, true, nil
	}

	if sess.RotatedTo != nil {
		result, err := s.refreshResultForRotatedTo(ctx, *sess.RotatedTo)
		if err != nil {
			return result, true, fmt.Errorf("refresh result for rotated to: %w", err)
		}

		return result, true, nil
	}

	return RefreshResult{}, false, nil
}

func (s *Store) refreshCAS(ctx context.Context, id string, idle bool, data string, sess *Session, now time.Time) (RefreshResult, bool, error) {
	refreshed := *sess

	refreshed.ExpiresAt = cappedExpiresAt(now, s.refreshTTL(idle), sess.AbsoluteExpiresAt)

	refreshedData, err := jsonv2.Marshal(refreshed)
	if err != nil {
		return RefreshResult{}, false, fmt.Errorf("marshal: %w", err)
	}

	result, err := s.evalInt(ctx, refreshCASScript,
		[]string{sessionKey(id), familyKey(refreshed.FamilyID)},
		[]string{data, string(refreshedData), fmt.Sprint(ttlSeconds(refreshed.ExpiresAt, now)), id})
	if err != nil {
		return RefreshResult{}, false, fmt.Errorf("eval int: %w", err)
	}

	out1, out2, err := refreshCASOutcome(result, refreshSuccessResult(idle, &refreshed))
	if err != nil {
		return out1, out2, fmt.Errorf("refresh CAS outcome: %w", err)
	}

	return out1, out2, nil
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
	}

	return RefreshResult{}, false, refreshCASError(result)
}

func refreshCASError(result int64) error {
	if result == -2 {
		return errors.New("session family lease points to another token")
	}

	return fmt.Errorf("unexpected session refresh CAS result: %d", result)
}

func (s *Store) refreshAfterCASMiss(ctx context.Context, id string, idle bool) (RefreshResult, error) {
	current, ok, err := s.Get(ctx, id)
	if err != nil {
		return RefreshResult{Kind: RefreshMissing}, fmt.Errorf("get: %w", err)
	}

	if !ok {
		return RefreshResult{Kind: RefreshMissing}, nil
	}

	if current.RotatedTo != nil {
		out, err := s.refreshResultForRotatedTo(ctx, *current.RotatedTo)
		if err != nil {
			return out, fmt.Errorf("refresh result for rotated to: %w", err)
		}

		return out, nil
	}

	if idle {
		return RefreshResult{}, errors.New("idle session refresh CAS did not converge")
	}

	return RefreshResult{Kind: RefreshRefreshed, Session: &current}, nil
}

// Rotate reports ok=false when no rotation was performed and that is the expected
// outcome: the source token is gone, it was past its absolute deadline, or the
// rotation interval has not elapsed. Callers must keep using the current token then.
func (s *Store) Rotate(ctx context.Context, oldID string) (Session, bool, error) {
	oldData, old, ok, err := s.rotateSource(ctx, oldID)
	if err != nil {
		return Session{}, false, fmt.Errorf("rotate source: %w", err)
	}

	if !ok {
		return Session{}, false, nil
	}

	now := time.Now().UTC()

	rotated, rotatedOK, done, err := s.currentRotation(ctx, oldID, &old, now)
	if err != nil {
		return Session{}, false, fmt.Errorf("current rotation: %w", err)
	}

	if done {
		return rotated, rotatedOK, nil
	}

	newSession, oldMarker, err := s.buildRotation(&old, now)
	if err != nil {
		return Session{}, false, fmt.Errorf("build rotation: %w", err)
	}

	result, err := s.rotateExec(ctx, oldID, oldData, &newSession, &oldMarker, now)
	if err != nil {
		return Session{}, false, fmt.Errorf("rotate exec: %w", err)
	}

	out, outOK, err := s.rotateOutcome(ctx, oldID, &newSession, result)
	if err != nil {
		return Session{}, false, fmt.Errorf("rotate outcome: %w", err)
	}

	return out, outOK, nil
}

func (s *Store) rotateOutcome(ctx context.Context, oldID string, newSession *Session, result int64) (Session, bool, error) {
	if result == 0 {
		out, ok, err := s.rotationWinner(ctx, oldID)
		if err != nil {
			return Session{}, false, fmt.Errorf("rotation winner: %w", err)
		}

		return out, ok, nil
	}

	switch result {
	case 1:
		return *newSession, true, nil
	case -1:
		return Session{}, false, errors.New("session family lease points to another token")
	}

	return Session{}, false, fmt.Errorf("unexpected session rotate result: %d", result)
}

func (s *Store) currentRotation(ctx context.Context, oldID string, old *Session, now time.Time) (sess Session, ok, done bool, err error) {
	if isAbsolutelyExpiredAt(old, now) {
		if deleteErr := s.Delete(ctx, oldID); deleteErr != nil {
			return Session{}, false, true, fmt.Errorf("delete: %w", deleteErr)
		}

		return Session{}, false, true, nil
	}

	if old.RotatedTo != nil {
		rotated, rotatedOK, getErr := s.Get(ctx, *old.RotatedTo)
		if getErr != nil {
			return Session{}, false, true, fmt.Errorf("get: %w", getErr)
		}

		return rotated, rotatedOK, true, nil
	}

	if now.Sub(old.LastRotatedAt) < s.cfg.RotationInterval {
		return Session{}, false, true, nil
	}

	return Session{}, false, false, nil
}

func (s *Store) rotateSource(ctx context.Context, oldID string) (data string, sess Session, ok bool, err error) {
	oldData, found, err := s.getRaw(ctx, oldID)
	if err != nil {
		return "", Session{}, false, fmt.Errorf("get raw: %w", err)
	}

	if !found {
		return "", Session{}, false, nil
	}

	var old Session

	if unmarshalErr := jsonv2.Unmarshal([]byte(oldData), &old); unmarshalErr != nil {
		return "", Session{}, false, fmt.Errorf("unmarshal: %w", unmarshalErr)
	}

	normalizeLegacySession(&old)

	return oldData, old, true, nil
}

func (s *Store) buildRotation(old *Session, now time.Time) (newSession, oldMarker Session, err error) {
	newID, err := auth.GenerateSessionID()
	if err != nil {
		return Session{}, Session{}, fmt.Errorf("generate session ID: %w", err)
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
	newData, err := jsonv2.Marshal(*newSession)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}

	markerData, err := jsonv2.Marshal(*oldMarker)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}

	out, err := s.evalInt(ctx, rotateScript,
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
	if err != nil {
		return out, fmt.Errorf("eval int: %w", err)
	}

	return out, nil
}
