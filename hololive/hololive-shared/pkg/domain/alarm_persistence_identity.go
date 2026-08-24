package domain

import (
	"errors"
	"fmt"
	"strings"
)

const (
	maxAlarmRoomIDBytes    = 100
	maxAlarmStreamIDBytes  = 64
	maxAlarmChannelIDBytes = 64
)

// ValidateLiveDispatchPersistenceIdentity enforces the fixed-width PostgreSQL
// ledger fields before this notification joins a shared batch transaction.
func (n *AlarmNotification) ValidateLiveDispatchPersistenceIdentity() error {
	if n == nil {
		return errors.New("live alarm persistence: notification is nil")
	}

	if err := validateAlarmDispatchIdentifier("room id", n.RoomID, maxAlarmRoomIDBytes); err != nil {
		return fmt.Errorf("validate alarm dispatch identifier: %w", err)
	}

	if n.Stream == nil {
		return errors.New("live alarm persistence: stream is nil")
	}

	if err := validateAlarmDispatchIdentifier("stream id", n.Stream.ID, maxAlarmStreamIDBytes); err != nil {
		return fmt.Errorf("validate alarm dispatch identifier: %w", err)
	}

	channelID, err := n.liveDispatchChannelID()
	if err != nil {
		return fmt.Errorf("live dispatch channel ID: %w", err)
	}

	if err := validateAlarmDispatchIdentifier("channel id", channelID, maxAlarmChannelIDBytes); err != nil {
		return fmt.Errorf("validate alarm dispatch identifier: %w", err)
	}

	return nil
}

func (n *AlarmNotification) liveDispatchChannelID() (string, error) {
	out, err := resolveLiveDispatchChannelID(n.liveDispatchChannelCandidates())
	if err != nil {
		return out, fmt.Errorf("resolve live dispatch channel ID: %w", err)
	}

	return out, nil
}

func (n *AlarmNotification) liveDispatchChannelCandidates() []string {
	candidates := make([]string, 0, 3)

	if n.Channel != nil {
		candidates = append(candidates, n.Channel.ID)
	}

	if n.Stream != nil {
		candidates = append(candidates, n.Stream.ChannelID)
		if n.Stream.Channel != nil {
			candidates = append(candidates, n.Stream.Channel.ID)
		}
	}

	return candidates
}

func resolveLiveDispatchChannelID(candidates []string) (string, error) {
	resolved := ""

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}

		if resolved != "" && candidate != resolved {
			return "", errors.New("live alarm persistence: channel ids disagree")
		}

		resolved = candidate
	}

	return resolved, nil
}

func validateAlarmDispatchIdentifier(name, value string, maxBytes int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("live alarm persistence: %s is empty", name)
	}

	if trimmed != value {
		return fmt.Errorf("live alarm persistence: %s has surrounding whitespace", name)
	}

	if len(value) > maxBytes {
		return fmt.Errorf(
			"live alarm persistence: %s is too long: %d > %d bytes",
			name,
			len(value),
			maxBytes,
		)
	}

	return nil
}
