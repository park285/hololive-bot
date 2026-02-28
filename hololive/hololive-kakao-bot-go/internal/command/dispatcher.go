package command

import (
	"context"
	"maps"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// NormalizeFunc: 명령어 타입과 파라미터를 기반으로 실행할 명령어 키와 최종 파라미터를 결정하는 함수 타입
type NormalizeFunc func(domain.CommandType, map[string]any) (string, map[string]any)

type sequentialDispatcher struct {
	registry  *Registry
	normalize NormalizeFunc
}

// NewSequentialDispatcher: 이벤트를 순차적으로 처리하는 기본 Dispatcher 구현체를 생성합니다.
func NewSequentialDispatcher(registry *Registry, normalize NormalizeFunc) Dispatcher {
	return &sequentialDispatcher{registry: registry, normalize: normalize}
}

func (d *sequentialDispatcher) Publish(ctx context.Context, cmdCtx *domain.CommandContext, events ...Event) (int, error) {
	if d == nil || d.registry == nil || d.normalize == nil {
		return 0, nil
	}

	executed := 0
	for _, event := range events {
		if event.Type == domain.CommandUnknown {
			continue
		}

		normalizedParams := cloneParams(event.Params)
		key, params := d.normalize(event.Type, normalizedParams)
		if err := d.registry.Execute(ctx, cmdCtx, key, params); err != nil {
			return executed, err
		}
		executed++
	}
	return executed, nil
}

func cloneParams(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	clone := make(map[string]any, len(src))
	maps.Copy(clone, src)
	return clone
}
