// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package batchrepo

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *PgxBatchRepository) resolveShortPersistedContentIDs(ctx context.Context, tx batchDB, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking) error {
	canonicalIDs, aliases := collectShortIdentityAliases(notifications, trackingRows)
	if len(canonicalIDs) == 0 {
		return nil
	}

	resolvedByCanonical, err := loadResolvedShortContentIDs(ctx, tx, aliases, canonicalIDs)
	if err != nil {
		return err
	}
	applyResolvedShortContentIDs(notifications, trackingRows, resolvedByCanonical)
	return nil
}

type shortIdentityRow struct {
	ContentID string `db:"content_id"`
}

func collectShortIdentityAliases(
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
) ([]string, []string) {
	canonicalIDs := make([]string, 0, len(notifications)+len(trackingRows))
	aliasSet := make(map[string]struct{}, (len(notifications)+len(trackingRows))*2)
	canonicalIDs = collectNotificationShortIdentityAliases(notifications, aliasSet, canonicalIDs)
	canonicalIDs = collectTrackingShortIdentityAliases(trackingRows, aliasSet, canonicalIDs)
	sort.Strings(canonicalIDs)

	aliases := make([]string, 0, len(aliasSet))
	for alias := range aliasSet {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	return canonicalIDs, aliases
}

func collectNotificationShortIdentityAliases(
	notifications []*domain.YouTubeNotificationOutbox,
	aliasSet map[string]struct{},
	canonicalIDs []string,
) []string {
	for i := range notifications {
		if notifications[i] != nil {
			canonicalIDs = addShortIdentityAliases(notifications[i].Kind, notifications[i].ContentID, aliasSet, canonicalIDs)
		}
	}
	return canonicalIDs
}

func collectTrackingShortIdentityAliases(
	trackingRows []*domain.YouTubeContentAlarmTracking,
	aliasSet map[string]struct{},
	canonicalIDs []string,
) []string {
	for i := range trackingRows {
		if trackingRows[i] != nil {
			canonicalIDs = addShortIdentityAliases(trackingRows[i].Kind, trackingRows[i].ContentID, aliasSet, canonicalIDs)
		}
	}
	return canonicalIDs
}

func addShortIdentityAliases(
	kind domain.OutboxKind,
	contentID string,
	aliasSet map[string]struct{},
	canonicalIDs []string,
) []string {
	if kind != domain.OutboxKindNewShort {
		return canonicalIDs
	}
	canonicalID := normalizeContentID(kind, contentID)
	if canonicalID == "" {
		return canonicalIDs
	}
	if _, exists := aliasSet[canonicalID]; !exists {
		canonicalIDs = append(canonicalIDs, canonicalID)
	}
	aliasSet[canonicalID] = struct{}{}
	if rawID := normalizeShortVideoResourceID(contentID); rawID != "" {
		aliasSet[rawID] = struct{}{}
	}
	return canonicalIDs
}

func loadResolvedShortContentIDs(
	ctx context.Context,
	tx batchDB,
	aliases []string,
	canonicalIDs []string,
) (map[string]string, error) {
	resolvedByCanonical := make(map[string]string, len(canonicalIDs))
	if err := mergeResolvedShortContentIDs(ctx, tx, "youtube_notification_outbox", aliases, resolvedByCanonical, "load existing short outbox identities"); err != nil {
		return nil, err
	}
	if err := mergeResolvedShortContentIDs(ctx, tx, "youtube_content_alarm_tracking", aliases, resolvedByCanonical, "load existing short tracking identities"); err != nil {
		return nil, err
	}
	return resolvedByCanonical, nil
}

func mergeResolvedShortContentIDs(
	ctx context.Context,
	tx batchDB,
	table string,
	aliases []string,
	resolvedByCanonical map[string]string,
	action string,
) error {
	if len(aliases) == 0 {
		return nil
	}

	var rows []shortIdentityRow
	args := []any{domain.OutboxKindNewShort}
	args = append(args, anyArgs(aliases)...)
	if err := selectSQL(ctx, tx, &rows, action, `
		SELECT content_id
		FROM `+table+`
		WHERE kind = ?
		  AND content_id IN (`+inPlaceholders(len(aliases))+`)`, args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	for i := range rows {
		recordResolvedShortContentID(resolvedByCanonical, strings.TrimSpace(rows[i].ContentID))
	}
	return nil
}

func recordResolvedShortContentID(resolvedByCanonical map[string]string, contentID string) {
	canonicalID := normalizeContentID(domain.OutboxKindNewShort, contentID)
	if canonicalID == "" {
		return
	}
	if existing := resolvedByCanonical[canonicalID]; existing == canonicalID {
		return
	}
	if contentID == canonicalID {
		resolvedByCanonical[canonicalID] = canonicalID
		return
	}
	if _, exists := resolvedByCanonical[canonicalID]; !exists {
		resolvedByCanonical[canonicalID] = contentID
	}
}

func applyResolvedShortContentIDs(
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	resolvedByCanonical map[string]string,
) {
	for i := range notifications {
		if notifications[i] == nil || notifications[i].Kind != domain.OutboxKindNewShort {
			continue
		}
		notifications[i].ContentID = resolveShortPersistedContentID(notifications[i].ContentID, resolvedByCanonical)
	}
	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != domain.OutboxKindNewShort {
			continue
		}
		trackingRows[i].ContentID = resolveShortPersistedContentID(trackingRows[i].ContentID, resolvedByCanonical)
	}
}

func resolveShortPersistedContentID(contentID string, resolvedByCanonical map[string]string) string {
	canonicalID := normalizeContentID(domain.OutboxKindNewShort, contentID)
	if canonicalID == "" {
		return strings.TrimSpace(contentID)
	}
	if resolved := resolvedByCanonical[canonicalID]; resolved != "" {
		return resolved
	}
	return canonicalID
}
