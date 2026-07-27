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
	"strconv"
	"strings"
)

func (s *Service) readNotifiedData(ctx context.Context, key string) (*NotifiedData, error) {
	data, err := s.loadNotifiedData(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read notified data: load notified data: %w", err)
	}
	return data, nil
}

func (s *Service) loadNotifiedData(ctx context.Context, key string) (*NotifiedData, error) {
	fields, err := s.readNotifiedHashFields(ctx, key)
	if err == nil {
		if len(fields) == 0 {
			return nil, nil
		}
		return parseNotifiedHash(fields), nil
	}
	if isWrongTypeError(err) {
		return nil, fmt.Errorf("notified data has non-hash type: %w", err)
	}
	return nil, err
}

func (s *Service) readNotifiedHashFields(ctx context.Context, key string) (map[string]string, error) {
	fields, err := s.cache.HGetAll(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read notified hash fields: %w", err)
	}
	return fields, nil
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
