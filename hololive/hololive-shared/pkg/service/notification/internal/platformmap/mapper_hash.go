package platformmap

import (
	"context"
	"fmt"
	"time"

	"github.com/park285/shared-go/pkg/stringutil"
	"github.com/valkey-io/valkey-go"
)

const replaceHashMappingsScript = `
local source = ARGV[1]
local target = ARGV[2]
if redis.call('EXISTS', source) == 1 then
  redis.call('RENAME', source, target)
else
  redis.call('DEL', target)
end
return 1
`

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

	tmpKey := fmt.Sprintf("%s:tmp:%d", key, time.Now().UnixNano())
	if err := m.cache.Del(ctx, tmpKey); err != nil {
		return fmt.Errorf("delete temp mapping key %s: %w", tmpKey, err)
	}

	if err := m.cache.HMSet(ctx, tmpKey, fields); err != nil {
		_ = m.cache.Del(context.WithoutCancel(ctx), tmpKey)
		return fmt.Errorf("hmset temp mapping key %s: %w", tmpKey, err)
	}

	if err := m.renameHashMappingKey(ctx, tmpKey, key, fields); err != nil {
		_ = m.cache.Del(context.WithoutCancel(ctx), tmpKey)
		return fmt.Errorf("rename mapping key %s from %s: %w", key, tmpKey, err)
	}

	return nil
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

	resp := client.Do(ctx, builder.Eval().Script(replaceHashMappingsScript).Numkeys(0).Arg(tmpKey, key).Build())
	if err := resp.Error(); err != nil {
		return fmt.Errorf("eval rename key %s from %s: %w", key, tmpKey, err)
	}

	return nil
}

func (m *Mapper) rawEvalClient() (_ valkey.Client, _ valkey.Builder, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	client := m.cache.GetClient()
	builder := m.cache.B()
	if client == nil {
		return nil, valkey.Builder{}, false
	}

	return client, builder, true
}
