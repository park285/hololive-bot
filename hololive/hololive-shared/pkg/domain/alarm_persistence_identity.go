package domain

import (
	"fmt"
	"strings"
)

const maxAlarmDispatchIdentifierBytes = 64

// ValidateLiveDispatchPersistenceIdentity enforces the fixed-width PostgreSQL
// ledger fields before this notification joins a shared batch transaction.
func (n *AlarmNotification) ValidateLiveDispatchPersistenceIdentity() error {
	if n == nil {
		return fmt.Errorf("live alarm persistence: notification is nil")
	}
	if err := validateAlarmDispatchIdentifier("room id", n.RoomID); err != nil {
		return err
	}
	if n.Stream == nil {
		return fmt.Errorf("live alarm persistence: stream is nil")
	}
	if err := validateAlarmDispatchIdentifier("stream id", n.Stream.ID); err != nil {
		return err
	}
	channelID, err := n.liveDispatchChannelID()
	if err != nil {
		return err
	}
	return validateAlarmDispatchIdentifier("channel id", channelID)
}

func (n *AlarmNotification) liveDispatchChannelID() (string, error) {
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

	resolved := ""
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if resolved != "" && candidate != resolved {
			return "", fmt.Errorf("live alarm persistence: channel ids disagree")
		}
		resolved = candidate
	}
	return resolved, nil
}

func validateAlarmDispatchIdentifier(name, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("live alarm persistence: %s is empty", name)
	}
	if trimmed != value {
		return fmt.Errorf("live alarm persistence: %s has surrounding whitespace", name)
	}
	if len(value) > maxAlarmDispatchIdentifierBytes {
		return fmt.Errorf(
			"live alarm persistence: %s is too long: %d > %d bytes",
			name,
			len(value),
			maxAlarmDispatchIdentifierBytes,
		)
	}
	return nil
}
