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
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type failedNotificationOutboxRow struct {
	ID        int64             `db:"id"`
	Kind      domain.OutboxKind `db:"kind"`
	ContentID string            `db:"content_id"`
}

type failedNotificationIdentityInput struct {
	Kind      domain.OutboxKind
	ContentID string
}

func loadFailedNotificationOutboxRows(ctx context.Context, tx batchDB, notifications []*domain.YouTubeNotificationOutbox) ([]failedNotificationOutboxRow, error) {
	inputs := collectFailedNotificationIdentityInputs(notifications)
	if len(inputs) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(inputs)*2+1)
	var values strings.Builder
	appendValuesPlaceholders(&values, len(inputs), 2)
	for i := range inputs {
		args = append(args, inputs[i].Kind, inputs[i].ContentID)
	}
	args = append(args, domain.OutboxStatusFailed)

	var rows []failedNotificationOutboxRow
	if err := dbx.SelectSQL(ctx, tx, &rows, "query failed outbox rows", `
		WITH input(kind, content_id) AS (
			VALUES `+values.String()+mustSQL("repository_batch_delivery_state_0063_01.sql"), args...); err != nil {
		return nil, fmt.Errorf("query failed outbox rows: %w", err)
	}
	return rows, nil
}

func collectFailedNotificationIdentityInputs(notifications []*domain.YouTubeNotificationOutbox) []failedNotificationIdentityInput {
	inputs := make([]failedNotificationIdentityInput, 0, len(notifications))
	seen := make(map[string]struct{}, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
			continue
		}
		contentID := strings.TrimSpace(notification.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := notificationIdentityKey(notification.Kind, contentID)
		if _, ok := seen[identityKey]; ok {
			continue
		}
		seen[identityKey] = struct{}{}
		inputs = append(inputs, failedNotificationIdentityInput{Kind: notification.Kind, ContentID: contentID})
	}
	return inputs
}

func partitionFailedNotificationOutboxRows(rows []failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) (result1, result2 []failedNotificationOutboxRow) {
	completed := make([]failedNotificationOutboxRow, 0, len(rows))
	reactivations := make([]failedNotificationOutboxRow, 0, len(rows))
	for i := range rows {
		identityKey := notificationIdentityKey(rows[i].Kind, rows[i].ContentID)
		if _, ok := completedSentAtByIdentity[identityKey]; ok {
			completed = append(completed, rows[i])
			continue
		}
		reactivations = append(reactivations, rows[i])
	}
	return completed, reactivations
}

func filterCompletedNotifications(notifications []*domain.YouTubeNotificationOutbox, completedSentAtByIdentity map[string]time.Time) []*domain.YouTubeNotificationOutbox {
	if len(notifications) == 0 {
		return nil
	}

	filtered := make([]*domain.YouTubeNotificationOutbox, 0, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
			filtered = append(filtered, notification)
			continue
		}
		if _, ok := completedSentAtByIdentity[notificationIdentityKey(notification.Kind, notification.ContentID)]; ok {
			continue
		}
		filtered = append(filtered, notification)
	}
	return filtered
}

func collectFailedNotificationOutboxIDs(rows []failedNotificationOutboxRow) []int64 {
	if len(rows) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for i := range rows {
		if _, ok := seen[rows[i].ID]; ok {
			continue
		}
		seen[rows[i].ID] = struct{}{}
		ids = append(ids, rows[i].ID)
	}
	return ids
}

func resolveNotificationReactivationPostID(kind domain.OutboxKind, contentID, payload string) string {
	switch kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		return resolveShortNotificationReactivationPostID(contentID, payload)
	case domain.OutboxKindCommunityPost:
		return resolveCommunityNotificationReactivationPostID(contentID, payload)
	case domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
		return strings.TrimSpace(contentID)
	}

	return strings.TrimSpace(contentID)
}

func resolveShortNotificationReactivationPostID(contentID, payload string) string {
	var parsed shortNotificationPublishedAtPayload
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return strings.TrimSpace(contentID)
	}
	return firstNonBlankNotificationPostID(parsed.CanonicalPostID, contentID, parsed.VideoID)
}

func resolveCommunityNotificationReactivationPostID(contentID, payload string) string {
	var parsed communityNotificationPublishedAtPayload
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return strings.TrimSpace(contentID)
	}
	return firstNonBlankNotificationPostID(parsed.CanonicalPostID, contentID, parsed.PostID)
}

func firstNonBlankNotificationPostID(values ...string) string {
	for i := range values {
		if postID := strings.TrimSpace(values[i]); postID != "" {
			return postID
		}
	}
	return ""
}

func notificationIdentityKey(kind domain.OutboxKind, contentID string) string {
	return fmt.Sprintf("%s::%s", kind, strings.TrimSpace(contentID))
}

func rearmFailedDeliveryRows(ctx context.Context, tx batchDB, outboxIDs []int64, nextAttemptAt time.Time) error {
	if len(outboxIDs) == 0 {
		return nil
	}

	args := make([]any, 0, 3+len(outboxIDs))
	args = append(args, domain.OutboxStatusPending, nextAttemptAt)
	args = append(args, dbx.AnyArgs(outboxIDs)...)
	args = append(args, domain.OutboxStatusFailed)
	if _, err := dbx.ExecSQL(ctx, tx, "update delivery rows", mustSQL("repository_batch_delivery_state_0198_02.sql")+dbx.InPlaceholders(len(outboxIDs))+`)
		  AND status = ?`,
		args...,
	); err != nil {
		return fmt.Errorf("update delivery rows: %w", err)
	}
	return nil
}
