package scraping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type Snapshot struct {
	Operation  string
	ChannelID  string
	URL        string
	Source     FailureSource
	Reason     FailureReason
	Stage      string
	StatusCode int
	Body       []byte
	CapturedAt time.Time
}

type SnapshotSink interface {
	Capture(ctx context.Context, snapshot Snapshot) error
}

type SnapshotPolicy struct {
	Enabled        bool
	MaxBodyBytes   int
	MinInterval    time.Duration
	AllowedReasons map[FailureReason]bool
}

func DefaultSnapshotPolicy() SnapshotPolicy {
	return SnapshotPolicy{
		Enabled:      false,
		MaxBodyBytes: 512 << 10,
		MinInterval:  30 * time.Minute,
		AllowedReasons: map[FailureReason]bool{
			FailureReasonParserDrift:   true,
			FailureReasonEmptyResponse: true,
		},
	}
}

func (p SnapshotPolicy) allows(reason FailureReason) bool {
	if !p.Enabled {
		return false
	}
	if len(p.AllowedReasons) == 0 {
		return true
	}
	return p.AllowedReasons[reason]
}

func trimSnapshotBody(body string, maxBytes int) []byte {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	raw := []byte(body)
	if maxBytes > 0 && len(raw) > maxBytes {
		return raw[:maxBytes]
	}
	return raw
}

func SnapshotID(snapshot Snapshot) string {
	sum := sha256.Sum256([]byte(snapshot.Operation + "\n" + snapshot.ChannelID + "\n" + snapshot.Stage + "\n" + string(snapshot.Body)))
	return hex.EncodeToString(sum[:])
}
