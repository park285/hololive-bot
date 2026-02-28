package valkeyx

import (
	"context"
	"fmt"
	"strconv"

	"github.com/valkey-io/valkey-go"
)

// XAutoClaimResult: XAUTOCLAIM 응답 파싱 결과
type XAutoClaimResult struct {
	NextStartID string               // 다음 호출 시 사용할 커서 ID ("0-0"이면 스캔 완료)
	Entries     []valkey.XRangeEntry // claim된 메시지 목록
	DeletedIDs  []string             // 스트림에서 이미 삭제된 메시지 ID 목록
}

// ParseXAutoClaim: XAUTOCLAIM 응답을 파싱합니다.
// valkey-go에 AsXAutoClaim이 없으므로 ToArray() 기반 수동 파싱.
// 응답 형식: [next_start_id, [[id, [field, value, ...]], ...], [deleted_id, ...]]
func ParseXAutoClaim(res valkey.ValkeyResult) (XAutoClaimResult, error) {
	arr, err := res.ToArray()
	if err != nil {
		return XAutoClaimResult{}, fmt.Errorf("xautoclaim parse array: %w", err)
	}
	if len(arr) < 2 {
		return XAutoClaimResult{}, fmt.Errorf("xautoclaim unexpected array length: %d", len(arr))
	}

	// [0]: next cursor ID
	nextID, err := arr[0].ToString()
	if err != nil {
		return XAutoClaimResult{}, fmt.Errorf("xautoclaim parse next_id: %w", err)
	}

	// [1]: claimed entries (같은 형식의 XRANGE 응답)
	entries, err := arr[1].AsXRange()
	if err != nil {
		return XAutoClaimResult{}, fmt.Errorf("xautoclaim parse entries: %w", err)
	}

	result := XAutoClaimResult{
		NextStartID: nextID,
		Entries:     entries,
	}

	// [2]: deleted IDs (v7.0+, 없을 수 있음)
	if len(arr) >= 3 {
		deletedIDs, parseErr := arr[2].AsStrSlice()
		if parseErr == nil {
			result.DeletedIDs = deletedIDs
		}
	}

	return result, nil
}

// XPendingEntry: XPENDING 확장 응답의 개별 항목
type XPendingEntry struct {
	ID            string
	Consumer      string
	IdleMs        int64
	DeliveryCount int64
}

// ParseXPendingEntries: XPENDING 확장 응답을 파싱합니다.
// 응답 형식: [[id, consumer, idle_ms, delivery_count], ...]
func ParseXPendingEntries(res valkey.ValkeyResult) ([]XPendingEntry, error) {
	arr, err := res.ToArray()
	if err != nil {
		return nil, fmt.Errorf("xpending parse array: %w", err)
	}

	entries := make([]XPendingEntry, 0, len(arr))
	for _, item := range arr {
		fields, fieldErr := item.ToArray()
		if fieldErr != nil || len(fields) < 4 {
			continue
		}

		id, idErr := fields[0].ToString()
		if idErr != nil {
			continue
		}

		consumer, consumerErr := fields[1].ToString()
		if consumerErr != nil {
			continue
		}

		idleMs, idleErr := fields[2].AsInt64()
		if idleErr != nil {
			continue
		}

		deliveryCount, deliveryErr := fields[3].AsInt64()
		if deliveryErr != nil {
			continue
		}

		entries = append(entries, XPendingEntry{
			ID:            id,
			Consumer:      consumer,
			IdleMs:        idleMs,
			DeliveryCount: deliveryCount,
		})
	}
	return entries, nil
}

// MoveToDLQ: 메시지를 DLQ 스트림으로 복사(XADD)하고 원본을 ACK합니다.
// 참고: 현재 정책은 "복사+ACK"이며 원본 스트림 엔트리에 대한 XDEL은 수행하지 않습니다.
// DLQ 메시지에는 원본 메타데이터(stream, id, delivery_count)가 포함됩니다.
func MoveToDLQ(
	ctx context.Context,
	client valkey.Client,
	stream, group, dlqStream string,
	entry valkey.XRangeEntry,
	deliveryCount int64,
	maxLen int64,
) error {
	// DLQ 메시지 필드: 원본 데이터 + 메타데이터
	fields := make([]string, 0, len(entry.FieldValues)*2+6)
	for k, v := range entry.FieldValues {
		fields = append(fields, k, v)
	}
	fields = append(fields,
		"_dlq_original_stream", stream,
		"_dlq_original_id", entry.ID,
		"_dlq_delivery_count", strconv.FormatInt(deliveryCount, 10),
	)

	// pipeline: XADD to DLQ + XACK original (no XDEL)
	addCmd := client.B().Xadd().Key(dlqStream).Maxlen().Almost().Threshold(strconv.FormatInt(maxLen, 10)).Id("*").FieldValue()
	for i := 0; i < len(fields)-1; i += 2 {
		addCmd = addCmd.FieldValue(fields[i], fields[i+1])
	}

	cmds := valkey.Commands{
		addCmd.Build(),
		client.B().Xack().Key(stream).Group(group).Id(entry.ID).Build(),
	}

	results := client.DoMulti(ctx, cmds...)
	for _, r := range results {
		if rErr := r.Error(); rErr != nil {
			return fmt.Errorf("move to dlq pipeline: %w", rErr)
		}
	}
	return nil
}
