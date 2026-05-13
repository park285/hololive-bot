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

package dedup

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func (s *Service) readNotifiedData(ctx context.Context, key string) (*NotifiedData, error) {
	data, source, err := s.loadNotifiedData(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read notified data: load notified data: %w", err)
	}
	if data == nil {
		return nil, nil
	}

	if source == notifiedDataSourceLegacyString {
		if migErr := s.migrateLegacyNotifiedData(ctx, key, data); migErr != nil {
			s.logger.Warn("Failed to migrate legacy notified cache",
				slog.String("key", key),
				slog.Any("error", migErr),
			)
		}
	}

	return data, nil
}

func (s *Service) loadNotifiedData(ctx context.Context, key string) (*NotifiedData, notifiedDataSource, error) {
	fields, err := s.readNotifiedHashFields(ctx, key)
	if err == nil {
		if len(fields) == 0 {
			return nil, notifiedDataSourceMissing, nil
		}
		return parseNotifiedHash(fields), notifiedDataSourceHash, nil
	}
	if !isWrongTypeError(err) {
		return nil, notifiedDataSourceMissing, err
	}

	var legacy NotifiedData
	if err := s.cache.Get(ctx, key, &legacy); err != nil {
		return nil, notifiedDataSourceMissing, fmt.Errorf("get legacy string: %w", err)
	}
	if legacy.StartScheduled == "" && len(legacy.SentAt) == 0 {
		return nil, notifiedDataSourceMissing, nil
	}
	if legacy.SentAt == nil {
		legacy.SentAt = make(map[int]bool)
	}

	return &legacy, notifiedDataSourceLegacyString, nil
}

func (s *Service) readNotifiedHashFields(ctx context.Context, key string) (map[string]string, error) {
	fields, err := s.cache.HGetAll(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read notified hash fields: %w", err)
	}
	return fields, nil
}

func (s *Service) migrateLegacyNotifiedData(ctx context.Context, key string, data *NotifiedData) error {
	if err := s.cache.Del(ctx, key); err != nil {
		return fmt.Errorf("delete legacy key: %w", err)
	}
	if err := s.persistNotifiedHash(ctx, key, data); err != nil {
		return fmt.Errorf("persist migrated hash: %w", err)
	}
	return nil
}

func (s *Service) persistNotifiedHash(ctx context.Context, key string, data *NotifiedData) error {
	fields := make(map[string]any, len(data.SentAt)+1)
	fields["start_scheduled"] = data.StartScheduled
	for minute, sent := range data.SentAt {
		if !sent {
			continue
		}
		fields[strconv.Itoa(minute)] = "1"
	}
	if err := s.cache.HMSet(ctx, key, fields); err != nil {
		return fmt.Errorf("hmset notified hash: %w", err)
	}
	if err := s.cache.Expire(ctx, key, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("expire notified hash: %w", err)
	}
	return nil
}

func parseNotifiedHash(fields map[string]string) *NotifiedData {
	startScheduled := fields["start_scheduled"]
	sentAt := make(map[int]bool)
	for k := range fields {
		if k == "start_scheduled" {
			continue
		}
		if m, err := strconv.Atoi(k); err == nil {
			sentAt[m] = true
		}
	}

	return &NotifiedData{
		StartScheduled: startScheduled,
		SentAt:         sentAt,
	}
}

func isWrongTypeError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "WRONGTYPE")
}
