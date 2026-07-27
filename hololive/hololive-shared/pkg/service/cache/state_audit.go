package cache

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/valkey-io/valkey-go"
)

type StateShapeCounts struct {
	Missing     int `json:"missing"`
	Canonical   int `json:"canonical"`
	LegacyShape int `json:"legacy_shape"`
}

type PersistedStateAudit struct {
	NotifiedKeysMissing         int `json:"notified_keys_missing"`
	CanonicalNotifiedKeys       int `json:"canonical_notified_keys"`
	AggregateNotifiedLegacyKeys int `json:"aggregate_notified_legacy_keys"`
	MemberHashMissing           int `json:"member_hash_missing"`
	CanonicalMemberFields       int `json:"canonical_member_fields"`
	OldMemberCacheKeys          int `json:"old_member_cache_keys"`
}

func (c *Service) AuditPersistedState(ctx context.Context) (PersistedStateAudit, error) {
	if c == nil || c.client == nil {
		return PersistedStateAudit{}, fmt.Errorf("audit persisted state: cache client is nil")
	}

	notified, err := c.auditNotifiedKeys(ctx)
	if err != nil {
		return PersistedStateAudit{}, err
	}
	members, err := c.auditMemberFields(ctx)
	if err != nil {
		return PersistedStateAudit{}, err
	}
	return PersistedStateAudit{
		NotifiedKeysMissing:         notified.Missing,
		CanonicalNotifiedKeys:       notified.Canonical,
		AggregateNotifiedLegacyKeys: notified.LegacyShape,
		MemberHashMissing:           members.Missing,
		CanonicalMemberFields:       members.Canonical,
		OldMemberCacheKeys:          members.LegacyShape,
	}, nil
}

func (c *Service) auditNotifiedKeys(ctx context.Context) (StateShapeCounts, error) {
	keys, err := c.ScanKeys(ctx, "notified:*", 200)
	if err != nil {
		return StateShapeCounts{}, fmt.Errorf("audit persisted state: scan notified keys: %w", err)
	}

	counts := StateShapeCounts{}
	for _, key := range keys {
		if isKnownUnrelatedNotifiedKey(key) {
			continue
		}
		shape, err := c.classifyNotifiedKey(ctx, key)
		if err != nil {
			return StateShapeCounts{}, fmt.Errorf("audit persisted state: classify notified key: %w", err)
		}
		incrementShapeCount(&counts, shape)
	}
	if counts == (StateShapeCounts{}) {
		counts.Missing = 1
	}
	return counts, nil
}

func (c *Service) classifyNotifiedKey(ctx context.Context, key string) (stateShape, error) {
	keyType, err := c.client.Do(ctx, c.client.B().Type().Key(key).Build()).ToString()
	if err != nil {
		return stateShapeMissing, err
	}
	return c.classifyNotifiedKeyType(ctx, key, keyType)
}

func (c *Service) classifyNotifiedKeyType(ctx context.Context, key, keyType string) (stateShape, error) {
	switch keyType {
	case "none":
		return stateShapeMissing, nil
	case "hash":
		return c.classifyNotifiedHashKey(ctx, key)
	case "string":
		return c.classifyNotifiedStringKey(ctx, key)
	default:
		return stateShapeLegacy, nil
	}
}

func (c *Service) classifyNotifiedHashKey(ctx context.Context, key string) (stateShape, error) {
	if !isAggregateNotifiedKey(key) {
		return stateShapeLegacy, nil
	}
	return c.classifyNotifiedHash(ctx, key)
}

func (c *Service) classifyNotifiedStringKey(ctx context.Context, key string) (stateShape, error) {
	if !isCanonicalNotifiedMinuteKey(key) {
		return stateShapeLegacy, nil
	}
	return c.classifyNotifiedString(ctx, key)
}

func (c *Service) classifyNotifiedHash(ctx context.Context, key string) (stateShape, error) {
	fields, err := c.client.Do(ctx, c.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if valkey.IsValkeyNil(err) || len(fields) == 0 {
		return stateShapeMissing, nil
	}
	if err != nil {
		return stateShapeMissing, err
	}
	if isCanonicalNotifiedHash(fields) {
		return stateShapeCanonical, nil
	}
	return stateShapeLegacy, nil
}

func (c *Service) classifyNotifiedString(ctx context.Context, key string) (stateShape, error) {
	value, err := c.client.Do(ctx, c.client.B().Get().Key(key).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return stateShapeMissing, nil
	}
	if err != nil {
		return stateShapeMissing, err
	}
	if value == "\"1\"" && isCanonicalNotifiedMinuteKey(key) {
		return stateShapeCanonical, nil
	}
	return stateShapeLegacy, nil
}

func (c *Service) auditMemberFields(ctx context.Context) (StateShapeCounts, error) {
	exists, err := c.client.Do(ctx, c.client.B().Exists().Key(memberHashKey).Build()).AsInt64()
	if err != nil {
		return StateShapeCounts{}, fmt.Errorf("audit persisted state: check member hash: %w", err)
	}
	if exists == 0 {
		return StateShapeCounts{Missing: 1}, nil
	}

	fields, err := c.client.Do(ctx, c.client.B().Hkeys().Key(memberHashKey).Build()).AsStrSlice()
	if err != nil {
		return StateShapeCounts{}, fmt.Errorf("audit persisted state: read member fields: %w", err)
	}
	counts := StateShapeCounts{}
	for _, field := range fields {
		if isCanonicalMemberField(field) {
			counts.Canonical++
		} else {
			counts.LegacyShape++
		}
	}
	return counts, nil
}

type stateShape uint8

const (
	stateShapeMissing stateShape = iota
	stateShapeCanonical
	stateShapeLegacy
)

func incrementShapeCount(counts *StateShapeCounts, shape stateShape) {
	switch shape {
	case stateShapeMissing:
		counts.Missing++
	case stateShapeCanonical:
		counts.Canonical++
	case stateShapeLegacy:
		counts.LegacyShape++
	}
}

func isCanonicalNotifiedMinuteKey(key string) bool {
	parts := strings.Split(key, ":")
	if len(parts) != 4 || parts[0] != "notified" || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	if _, err := strconv.ParseInt(parts[len(parts)-2], 10, 64); err != nil {
		return false
	}
	_, err := strconv.Atoi(parts[len(parts)-1])
	return err == nil
}

func isAggregateNotifiedKey(key string) bool {
	parts := strings.Split(key, ":")
	return len(parts) == 2 && parts[0] == "notified" && strings.TrimSpace(parts[1]) != ""
}

func isCanonicalNotifiedHash(fields map[string]string) bool {
	startScheduled, ok := fields["start_scheduled"]
	if !ok {
		return false
	}
	if _, err := time.Parse(time.RFC3339, startScheduled); err != nil {
		return false
	}

	hasMinuteMarker := false
	for field, value := range fields {
		if field == "start_scheduled" {
			continue
		}
		if _, err := strconv.Atoi(field); err != nil || value != "1" {
			return false
		}
		hasMinuteMarker = true
	}
	return hasMinuteMarker
}

func isKnownUnrelatedNotifiedKey(key string) bool {
	for _, prefix := range []string{
		sharedalarmkeys.NotifyClaimKeyPrefix,
		sharedalarmkeys.UpcomingEventKeyPrefix,
		sharedalarmkeys.ScheduleTransitionKeyPrefix,
		sharedalarmkeys.LogicalScheduleIndexKeyPrefix,
		sharedalarmkeys.ChzzkLiveNotifiedKeyPrefix,
		sharedalarmkeys.IntegratedNotifiedKeyPrefix,
	} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
