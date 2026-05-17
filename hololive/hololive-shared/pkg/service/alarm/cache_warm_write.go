package alarm

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/valkey-io/valkey-go"
)

func writeWarmSet(ctx context.Context, cacheSvc cache.Client, key string, members []string, scope string) error {
	return writeWarmSetMap(ctx, cacheSvc, map[string][]string{key: members}, scope)
}

func writeWarmSetMap(ctx context.Context, cacheSvc cache.Client, setMembers map[string][]string, scope string) error {
	if len(setMembers) == 0 {
		return nil
	}

	if !supportsWarmSetBatch(cacheSvc) {
		return writeWarmSetMapSequential(ctx, cacheSvc, setMembers, scope)
	}

	return writeWarmSetMapBatch(ctx, cacheSvc, setMembers, scope)
}

func writeWarmSetMapSequential(ctx context.Context, cacheSvc cache.Client, setMembers map[string][]string, scope string) error {
	for key, members := range setMembers {
		dedupedMembers := compactUniqueStrings(members)
		if len(dedupedMembers) == 0 {
			continue
		}
		if _, err := cacheSvc.SAdd(ctx, key, dedupedMembers); err != nil {
			return fmt.Errorf("add %s for key %s: %w", scope, key, err)
		}
	}
	return nil
}

func writeWarmSetMapBatch(ctx context.Context, cacheSvc cache.Client, setMembers map[string][]string, scope string) error {
	keys := make([]string, 0, len(setMembers))
	cmds := make([]valkey.Completed, 0, len(setMembers))
	for key, members := range setMembers {
		dedupedMembers := compactUniqueStrings(members)
		if len(dedupedMembers) == 0 {
			continue
		}
		keys = append(keys, key)
		cmds = append(cmds, cacheSvc.Builder().Sadd().Key(key).Member(dedupedMembers...).Build())
	}
	if len(cmds) == 0 {
		return nil
	}

	results := cacheSvc.DoMulti(ctx, cmds...)
	if len(results) != len(cmds) {
		return fmt.Errorf("add %s: unexpected result count: %d", scope, len(results))
	}
	return verifyWarmSetBatchResults(results, keys, scope)
}

func verifyWarmSetBatchResults(results []valkey.ValkeyResult, keys []string, scope string) error {
	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("add %s for key %s: %w", scope, keys[i], err)
		}
	}
	return nil
}

func supportsWarmSetBatch(cacheSvc cache.Client) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	builder := cacheSvc.Builder()
	return builder != (valkey.Builder{})
}

func writeWarmHash(ctx context.Context, cacheSvc cache.Client, key string, values map[string]string) (err error) {
	if len(values) == 0 {
		return nil
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = writeWarmHashFields(ctx, cacheSvc, key, values)
		}
	}()

	fields := make(map[string]any, len(values))
	for field, value := range values {
		fields[field] = value
	}
	if err := cacheSvc.HMSet(ctx, key, fields); err == nil {
		return nil
	}

	return writeWarmHashFields(ctx, cacheSvc, key, values)
}

func writeWarmHashFields(ctx context.Context, cacheSvc cache.Client, key string, values map[string]string) error {
	for field, value := range values {
		if setErr := cacheSvc.HSet(ctx, key, field, value); setErr != nil {
			return setErr
		}
	}
	return nil
}
