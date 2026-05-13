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

package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
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

func (as *AlarmService) replaceHashMappings(
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
		if err := as.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("delete empty mapping key %s: %w", key, err)
		}

		return nil
	}

	tmpKey := fmt.Sprintf("%s:tmp:%d", key, time.Now().UnixNano())
	if err := as.cache.Del(ctx, tmpKey); err != nil {
		return fmt.Errorf("delete temp mapping key %s: %w", tmpKey, err)
	}

	if err := as.cache.HMSet(ctx, tmpKey, fields); err != nil {
		_ = as.cache.Del(context.WithoutCancel(ctx), tmpKey)
		return fmt.Errorf("hmset temp mapping key %s: %w", tmpKey, err)
	}

	if err := as.renameHashMappingKey(ctx, tmpKey, key, fields); err != nil {
		_ = as.cache.Del(context.WithoutCancel(ctx), tmpKey)
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

func (as *AlarmService) replaceHashMappingsWithEmptyMarker(
	ctx context.Context,
	key string,
	emptyMarkerKey string,
	mappings map[string]string,
) error {
	if err := as.replaceHashMappings(ctx, key, mappings); err != nil {
		return err
	}

	if len(mappings) == 0 {
		if err := as.cache.Set(ctx, emptyMarkerKey, "1", 0); err != nil {
			return fmt.Errorf("set empty marker %s: %w", emptyMarkerKey, err)
		}

		return nil
	}

	if err := as.cache.Del(ctx, emptyMarkerKey); err != nil {
		return fmt.Errorf("clear empty marker %s: %w", emptyMarkerKey, err)
	}

	return nil
}

func (as *AlarmService) renameHashMappingKey(ctx context.Context, tmpKey, key string, fields map[string]any) error {
	client, builder, ok := as.rawPlatformMappingEvalClient()
	if !ok {
		if err := as.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("fallback delete key %s: %w", key, err)
		}
		if err := as.cache.HMSet(ctx, key, fields); err != nil {
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

func (as *AlarmService) rawPlatformMappingEvalClient() (_ valkey.Client, _ valkey.Builder, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	client := as.cache.GetClient()
	builder := as.cache.B()
	if client == nil {
		return nil, valkey.Builder{}, false
	}

	return client, builder, true
}
