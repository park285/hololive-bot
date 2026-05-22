package scraping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

const SnapshotSchemaVersion = "v1"

type Snapshot struct {
	Operation     string
	ChannelID     string
	URL           string
	Source        FailureSource
	Reason        FailureReason
	Stage         string
	StatusCode    int
	Body          []byte
	CapturedAt    time.Time
	SchemaVersion string
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
		raw = raw[:maxBytes]
	}
	return sanitizeSnapshotBody(raw)
}

var snapshotRedactionPatterns = []*regexp.Regexp{
	// Set-Cookie 한 항목 (`; Path=/` 등 attribute 포함 가능) — 끝 토큰 또는 줄바꿈까지
	regexp.MustCompile(`(?i)(set-cookie\s*:\s*)([^<\n\r]+)`),
	// Authorization 헤더(Bearer/Basic 등)
	regexp.MustCompile(`(?i)(authorization\s*:\s*)([^<\n\r]+)`),
	// JSON-encoded sensitive 식별자
	regexp.MustCompile(`(?i)("x-goog-visitor-id"\s*:\s*")([^"]+)(")`),
	regexp.MustCompile(`(?i)("__Secure-[A-Za-z0-9_-]+"\s*:\s*")([^"]+)(")`),
}

func sanitizeSnapshotBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	result := body
	for _, pattern := range snapshotRedactionPatterns {
		result = pattern.ReplaceAllFunc(result, func(match []byte) []byte {
			groups := pattern.FindSubmatch(match)
			if len(groups) < 3 {
				return match
			}
			head := groups[1]
			tail := []byte{}
			if len(groups) >= 4 {
				tail = groups[3]
			}
			redacted := make([]byte, 0, len(head)+len("[REDACTED]")+len(tail))
			redacted = append(redacted, head...)
			redacted = append(redacted, []byte("[REDACTED]")...)
			redacted = append(redacted, tail...)
			return redacted
		})
	}
	return result
}

func SnapshotID(snapshot Snapshot) string {
	sum := sha256.Sum256([]byte(snapshot.Operation + "\n" + snapshot.ChannelID + "\n" + snapshot.Stage + "\n" + string(snapshot.Body)))
	return hex.EncodeToString(sum[:])
}
