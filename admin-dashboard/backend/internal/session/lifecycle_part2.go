package session

import (
	"context"
	"errors"
	"fmt"
)

func (s *Store) rotationWinner(ctx context.Context, oldID string) (Session, bool, error) {
	current, ok, err := s.Get(ctx, oldID)
	if err != nil {
		return Session{}, false, fmt.Errorf("get: %w", err)
	}

	if !ok || current.RotatedTo == nil {
		return Session{}, false, nil
	}

	winner, winnerOK, err := s.Get(ctx, *current.RotatedTo)
	if err != nil {
		return Session{}, false, fmt.Errorf("get: %w", err)
	}

	if !winnerOK || winner.RotatedTo != nil || winner.FamilyID != current.FamilyID {
		return Session{}, false, errors.New("session rotation winner is missing or inconsistent")
	}

	return winner, true, nil
}

func (s *Store) refreshResultForRotatedTo(ctx context.Context, rotatedTo string) (RefreshResult, error) {
	replacement, ok, err := s.Get(ctx, rotatedTo)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("get: %w", err)
	}

	if !ok {
		return RefreshResult{Kind: RefreshNotRefreshable}, nil
	}

	return RefreshResult{Kind: RefreshRotated, Session: &replacement}, nil
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
