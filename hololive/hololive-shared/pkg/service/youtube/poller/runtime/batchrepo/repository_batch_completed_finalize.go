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

type completedFailedNotificationFinalizeInput struct {
	ID     int64
	SentAt time.Time
}

func finalizeCompletedFailedNotificationRows(ctx context.Context, tx batchDB, rows []failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) error {
	inputs := collectCompletedFailedNotificationFinalizeInputs(rows, completedSentAtByIdentity)
	if len(inputs) == 0 {
		return nil
	}
	if err := bulkUpdateCompletedFailedNotificationOutboxRows(ctx, tx, inputs); err != nil {
		return err
	}
	return bulkUpdateCompletedFailedNotificationDeliveryRows(ctx, tx, inputs)
}

func completedSentAtForFailedNotification(row failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) (time.Time, bool) {
	identityKey := notificationIdentityKey(row.Kind, row.ContentID)
	sentAt, ok := completedSentAtByIdentity[identityKey]
	if !ok || sentAt.IsZero() {
		return time.Time{}, false
	}
	return yttimestamp.Normalize(sentAt), true
}

func collectCompletedFailedNotificationFinalizeInputs(
	rows []failedNotificationOutboxRow,
	completedSentAtByIdentity map[string]time.Time,
) []completedFailedNotificationFinalizeInput {
	inputs := make([]completedFailedNotificationFinalizeInput, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for i := range rows {
		if _, ok := seen[rows[i].ID]; ok {
			continue
		}
		sentAt, ok := completedSentAtForFailedNotification(rows[i], completedSentAtByIdentity)
		if !ok {
			continue
		}
		seen[rows[i].ID] = struct{}{}
		inputs = append(inputs, completedFailedNotificationFinalizeInput{ID: rows[i].ID, SentAt: sentAt})
	}
	return inputs
}

func bulkUpdateCompletedFailedNotificationOutboxRows(
	ctx context.Context,
	tx batchDB,
	inputs []completedFailedNotificationFinalizeInput,
) error {
	query, args := buildCompletedFailedNotificationFinalizeValues(inputs)
	args = append(args, domain.OutboxStatusSent, domain.OutboxStatusFailed)
	if _, err := dbx.ExecSQL(ctx, tx, fmt.Sprintf("bulk update completed failed outbox rows (%d rows)", len(inputs)), `
		WITH input(id, sent_at) AS (
			VALUES `+query+mustSQL("repository_batch_completed_finalize_0088_01.sql"), args...); err != nil {
		return fmt.Errorf("bulk update completed failed outbox rows: %w", err)
	}
	return nil
}

func bulkUpdateCompletedFailedNotificationDeliveryRows(
	ctx context.Context,
	tx batchDB,
	inputs []completedFailedNotificationFinalizeInput,
) error {
	query, args := buildCompletedFailedNotificationFinalizeValues(inputs)
	args = append(args, domain.OutboxStatusSent, domain.OutboxStatusFailed)
	if _, err := dbx.ExecSQL(ctx, tx, fmt.Sprintf("bulk update completed failed delivery rows (%d rows)", len(inputs)), `
		WITH input(id, sent_at) AS (
			VALUES `+query+mustSQL("repository_batch_completed_finalize_0111_02.sql"), args...); err != nil {
		return fmt.Errorf("bulk update completed failed delivery rows: %w", err)
	}
	return nil
}

func buildCompletedFailedNotificationFinalizeValues(inputs []completedFailedNotificationFinalizeInput) (query string, args []any) {
	args = make([]any, 0, len(inputs)*2)
	var values strings.Builder
	for i := range inputs {
		if i > 0 {
			values.WriteByte(',')
		}
		values.WriteString("(?::bigint, ?::timestamptz)")
		args = append(args, inputs[i].ID, inputs[i].SentAt)
	}
	return values.String(), args
}
