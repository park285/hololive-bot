package alarmstore

import (
	"context"
	"fmt"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func writeWarmSet(ctx context.Context, cacheClient cache.LowLevelCache, key string, members []string, scope string) error {
	if err := writeWarmSetMap(ctx, cacheClient, map[string][]string{key: members}, scope); err != nil {
		return fmt.Errorf("write warm set map: %w", err)
	}

	return nil
}

func writeWarmSetMap(ctx context.Context, cacheClient cache.LowLevelCache, setMembers map[string][]string, scope string) error {
	keys := make([]string, 0, len(setMembers))
	cmds := make([]valkey.Completed, 0, len(setMembers))

	for key, members := range setMembers {
		dedupedMembers := compactUniqueStrings(members)
		if len(dedupedMembers) == 0 {
			continue
		}

		keys = append(keys, key)
		cmds = append(cmds, cacheClient.B().Sadd().Key(key).Member(dedupedMembers...).Build())
	}

	if len(cmds) == 0 {
		return nil
	}

	results := cacheClient.DoMulti(ctx, cmds...)
	if len(results) != len(cmds) {
		return fmt.Errorf("add %s: unexpected result count: %d", scope, len(results))
	}

	if err := verifyWarmSetBatchResults(results, keys, scope); err != nil {
		return fmt.Errorf("verify warm set batch results: %w", err)
	}

	return nil
}

func verifyWarmSetBatchResults(results []valkey.ValkeyResult, keys []string, scope string) error {
	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("add %s for key %s: %w", scope, keys[i], err)
		}
	}

	return nil
}

func writeWarmHash(ctx context.Context, cacheClient cache.HashWriter, key string, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}

	fields := make(map[string]any, len(values))
	for field, value := range values {
		fields[field] = value
	}

	if err := cacheClient.HMSet(ctx, key, fields); err != nil {
		return fmt.Errorf("h mset %s: %w", key, err)
	}

	return nil
}
