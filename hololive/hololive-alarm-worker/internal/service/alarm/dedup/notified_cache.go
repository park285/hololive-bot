package dedup

import (
	"context"
	"fmt"
	"strconv"
)

func (s *Service) readNotifiedData(ctx context.Context, key string) (NotifiedData, bool, error) {
	data, ok, err := s.loadNotifiedData(ctx, key)
	if err != nil {
		return NotifiedData{}, false, fmt.Errorf("read notified data: load notified data: %w", err)
	}

	return data, ok, nil
}

func (s *Service) loadNotifiedData(ctx context.Context, key string) (NotifiedData, bool, error) {
	fields, err := s.readNotifiedHashFields(ctx, key)
	if err != nil {
		return NotifiedData{}, false, fmt.Errorf("load notified data: %w", err)
	}

	if len(fields) == 0 {
		return NotifiedData{}, false, nil
	}

	return parseNotifiedHash(fields), true, nil
}

func (s *Service) readNotifiedHashFields(ctx context.Context, key string) (map[string]string, error) {
	fields, err := s.cache.HGetAll(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read notified hash fields: %w", err)
	}

	return fields, nil
}

func parseNotifiedHash(fields map[string]string) NotifiedData {
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

	return NotifiedData{
		StartScheduled: startScheduled,
		SentAt:         sentAt,
	}
}
