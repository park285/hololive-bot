package session

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ErrFamilyInactive는 만료·폐기·eviction된 family의 변경 진입을 거부합니다.
var ErrFamilyInactive = errors.New("session family is inactive")

var mutationIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ValidMutationID는 변경 ID의 단일 정본 UUIDv4 형식을 검사합니다.
func ValidMutationID(id string) bool { return mutationIDPattern.MatchString(id) }

// ClaimMutation은 유효한 family에서 ID를 한 번만 선점하며 결과·취소 시에도 해제하지 않습니다.
// ASVS 2.3.1/2.3.2: C02의 브라우저 재전송을 차단하며 family와 함께 eviction되어야 안전합니다.
func (s *Store) ClaimMutation(ctx context.Context, sess Session, id string) (bool, error) {
	if !ValidMutationID(id) {
		return false, errors.New("invalid mutation ID")
	}

	now := time.Now().UTC()
	if sess.FamilyID == "" || !now.Before(sess.ExpiresAt) || !now.Before(sess.AbsoluteExpiresAt) {
		return false, ErrFamilyInactive
	}

	result, err := s.evalInt(ctx, claimMutationScript, []string{familyKey(sess.FamilyID), sessionKey(sess.ID)}, []string{sess.ID, "mutation:" + id})
	if err != nil {
		return false, fmt.Errorf("claim administrator mutation: %w", err)
	}

	switch result {
	case 1:
		return true, nil
	case 0:
		return false, nil
	case -1:
		return false, ErrFamilyInactive
	default:
		return false, fmt.Errorf("unexpected mutation claim result: %d", result)
	}
}

const claimMutationScript = `
if redis.call('HGET', KEYS[1], 'token') ~= ARGV[1] then return -1 end
if redis.call('EXISTS', KEYS[2]) == 0 then return -1 end
return redis.call('HSETNX', KEYS[1], ARGV[2], '1')
`
