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

	"github.com/kapu/hololive-shared/internal/dbx"
	yttimestamp "github.com/kapu/hololive-shared/internal/service/youtube/timestamp"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type completedNotificationSentStateInput struct {
	Kind               domain.OutboxKind
	IdentityKey        string
	RequestedContentID string
	CanonicalContentID string
	RawContentID       string
	ReactivationPostID string
}

type completedNotificationSentAtRow struct {
	IdentityKey string    `db:"identity_key"`
	SentAt      time.Time `db:"sent_at"`
}

func loadCompletedNotificationSentAtByIdentity(ctx context.Context, tx batchDB, notifications []*domain.YouTubeNotificationOutbox) (map[string]time.Time, error) {
	completed := make(map[string]time.Time)
	if len(notifications) == 0 || tx == nil {
		return completed, nil
	}

	inputs := collectCompletedNotificationSentStateInputs(notifications)
	if len(inputs) == 0 {
		return completed, nil
	}

	trackingRows, err := loadCompletedNotificationTrackingSentAtRows(ctx, tx, inputs)
	if err != nil {
		return nil, err
	}
	mergeCompletedNotificationSentAtRows(completed, trackingRows)

	alarmStateRows, err := loadCompletedNotificationAlarmStateSentAtRows(ctx, tx, inputs)
	if err != nil {
		return nil, err
	}
	mergeCompletedNotificationSentAtRows(completed, alarmStateRows)
	return completed, nil
}

func collectCompletedNotificationSentStateInputs(notifications []*domain.YouTubeNotificationOutbox) []completedNotificationSentStateInput {
	inputs := make([]completedNotificationSentStateInput, 0, len(notifications))
	seen := make(map[string]struct{}, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		input, ok := completedNotificationSentStateInputFor(notification)
		if !ok {
			continue
		}
		if _, ok := seen[input.IdentityKey]; ok {
			continue
		}
		seen[input.IdentityKey] = struct{}{}
		inputs = append(inputs, input)
	}
	return inputs
}

func completedNotificationSentStateInputFor(notification *domain.YouTubeNotificationOutbox) (completedNotificationSentStateInput, bool) {
	if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
		return completedNotificationSentStateInput{}, false
	}
	contentID := strings.TrimSpace(notification.ContentID)
	if contentID == "" {
		return completedNotificationSentStateInput{}, false
	}
	return completedNotificationSentStateInput{
		Kind:               notification.Kind,
		IdentityKey:        notificationIdentityKey(notification.Kind, contentID),
		RequestedContentID: contentID,
		CanonicalContentID: normalizeContentID(notification.Kind, contentID),
		RawContentID:       rawNotificationContentID(notification.Kind, contentID),
		ReactivationPostID: resolveNotificationReactivationPostID(notification.Kind, contentID, notification.Payload),
	}, true
}

func loadCompletedNotificationTrackingSentAtRows(
	ctx context.Context,
	tx batchDB,
	inputs []completedNotificationSentStateInput,
) ([]completedNotificationSentAtRow, error) {
	args := make([]any, 0, len(inputs)*5)
	var values strings.Builder
	appendValuesPlaceholders(&values, len(inputs), 5)
	for i := range inputs {
		args = append(args,
			inputs[i].Kind,
			inputs[i].IdentityKey,
			inputs[i].RequestedContentID,
			inputs[i].CanonicalContentID,
			inputs[i].RawContentID,
		)
	}

	var rows []completedNotificationSentAtRow
	if err := dbx.SelectSQL(ctx, tx, &rows, "load completed notification tracking sent state", `
		WITH input(kind, identity_key, requested_content_id, canonical_content_id, raw_content_id) AS (
			VALUES `+values.String()+mustSQL("repository_batch_completed_state_0130_01.sql"), args...); err != nil {
		return nil, fmt.Errorf("load completed notification tracking sent state: %w", err)
	}
	return rows, nil
}

func loadCompletedNotificationAlarmStateSentAtRows(
	ctx context.Context,
	tx batchDB,
	inputs []completedNotificationSentStateInput,
) ([]completedNotificationSentAtRow, error) {
	filtered := make([]completedNotificationSentStateInput, 0, len(inputs))
	for i := range inputs {
		if strings.TrimSpace(inputs[i].ReactivationPostID) != "" {
			filtered = append(filtered, inputs[i])
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(filtered)*3)
	var values strings.Builder
	appendValuesPlaceholders(&values, len(filtered), 3)
	for i := range filtered {
		args = append(args, filtered[i].Kind, filtered[i].IdentityKey, strings.TrimSpace(filtered[i].ReactivationPostID))
	}

	var rows []completedNotificationSentAtRow
	if err := dbx.SelectSQL(ctx, tx, &rows, "load completed notification alarm state sent state", `
		WITH input(kind, identity_key, post_id) AS (
			VALUES `+values.String()+mustSQL("repository_batch_completed_state_0174_02.sql"), args...); err != nil {
		return nil, fmt.Errorf("load completed notification alarm state sent state: %w", err)
	}
	return rows, nil
}

func mergeCompletedNotificationSentAtRows(completed map[string]time.Time, rows []completedNotificationSentAtRow) {
	for i := range rows {
		updateIdentitySentAtMin(completed, rows[i].IdentityKey, yttimestamp.Normalize(rows[i].SentAt))
	}
}

func rawNotificationContentID(kind domain.OutboxKind, contentID string) string {
	switch kind {
	case domain.OutboxKindNewShort:
		return normalizeShortVideoResourceID(contentID)
	case domain.OutboxKindCommunityPost:
		return normalizeCommunityPostResourceID(contentID)
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
		return strings.TrimSpace(contentID)
	default:
		return strings.TrimSpace(contentID)
	}
}
