package platformmap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/park285/shared-go/pkg/stringutil"
	"github.com/valkey-io/valkey-go"
)

const platformMapTempKeySeparator = ":tmp:"

var platformMapTempKeySeq atomic.Uint64

// hash tag 없는 키는 전체 문자열로 slot이 계산되므로 `{key}` 래핑 tmp 키는
// target과 같은 slot에 떨어진다. tag가 이미 있으면 그대로 보존한다.
func platformMapTempKey(key string) string {
	sequence := platformMapTempKeySeq.Add(1)
	if hasValkeyHashTag(key) {
		return fmt.Sprintf("%s%s%d", key, platformMapTempKeySeparator, sequence)
	}

	return fmt.Sprintf("{%s}%s%d", key, platformMapTempKeySeparator, sequence)
}

func hasValkeyHashTag(key string) bool {
	_, after, ok := strings.Cut(key, "{")
	if !ok {
		return false
	}

	end := strings.IndexByte(after, '}')

	return end > 0
}

func (m *Mapper) replaceHashMappings(
	ctx context.Context,
	key string,
	mappings map[string]string,
) error {
	key = stringutil.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("mapping key is empty")
	}

	fields := make(map[string]any, len(mappings))
	normalizeHashMappingFields(fields, mappings)

	if len(fields) == 0 {
		if err := m.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("delete empty mapping key %s: %w", key, err)
		}

		return nil
	}

	tmpKey := platformMapTempKey(key)
	if err := m.cache.Del(ctx, tmpKey); err != nil {
		return fmt.Errorf("delete temp mapping key %s: %w", tmpKey, err)
	}

	if err := m.cache.HMSet(ctx, tmpKey, fields); err != nil {
		m.deleteTempMappingKey(ctx, tmpKey)
		return fmt.Errorf("hmset temp mapping key %s: %w", tmpKey, err)
	}

	if err := m.renameHashMappingKey(ctx, tmpKey, key, fields); err != nil {
		m.deleteTempMappingKey(ctx, tmpKey)
		return fmt.Errorf("rename mapping key %s from %s: %w", key, tmpKey, err)
	}

	return nil
}

func (m *Mapper) deleteTempMappingKey(ctx context.Context, tmpKey string) {
	if err := m.cache.Del(ctx, tmpKey); err != nil && m.logger != nil {
		m.logger.Warn("delete temp platform mapping key failed", "key", tmpKey, "error", err)
	}
}

func normalizeHashMappingFields(fields map[string]any, mappings map[string]string) {
	for field, value := range mappings {
		field = stringutil.TrimSpace(field)
		value = stringutil.TrimSpace(value)
		if field == "" || value == "" {
			continue
		}
		fields[field] = value
	}
}

func (m *Mapper) replaceHashMappingsWithEmptyMarker(
	ctx context.Context,
	key string,
	emptyMarkerKey string,
	mappings map[string]string,
) error {
	if err := m.replaceHashMappings(ctx, key, mappings); err != nil {
		return err
	}

	if len(mappings) == 0 {
		if err := m.cache.Set(ctx, emptyMarkerKey, "1", 0); err != nil {
			return fmt.Errorf("set empty marker %s: %w", emptyMarkerKey, err)
		}

		return nil
	}

	if err := m.cache.Del(ctx, emptyMarkerKey); err != nil {
		return fmt.Errorf("clear empty marker %s: %w", emptyMarkerKey, err)
	}

	return nil
}

func (m *Mapper) renameHashMappingKey(ctx context.Context, tmpKey, key string, fields map[string]any) error {
	client, builder, ok := m.rawEvalClient()
	if !ok {
		if err := m.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("fallback delete key %s: %w", key, err)
		}
		if err := m.cache.HMSet(ctx, key, fields); err != nil {
			return fmt.Errorf("fallback hmset key %s: %w", key, err)
		}
		return nil
	}

	// source 부재 시 에러를 반환하고 기존 target은 보존한다; 정상 경로는 HMSet 직후라 source가 존재한다.
	resp := client.Do(ctx, builder.Rename().Key(tmpKey).Newkey(key).Build())
	if err := resp.Error(); err != nil {
		return fmt.Errorf("rename key %s from %s: %w", key, tmpKey, err)
	}

	return nil
}

func (m *Mapper) rawEvalClient() (_ valkey.Client, _ valkey.Builder, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			if m.logger != nil {
				m.logger.Warn("raw valkey eval client unavailable", slog.Any("panic", r))
			}
		}
	}()

	client := m.cache.GetClient()
	builder := m.cache.B()
	if client == nil {
		return nil, valkey.Builder{}, false
	}

	return client, builder, true
}
