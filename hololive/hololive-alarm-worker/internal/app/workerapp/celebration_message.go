package workerapp

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func renderCelebrationMessage(ctx context.Context, renderer *template.Renderer, envelope *domain.AlarmQueueEnvelope) (string, error) {
	if envelope.Celebration == nil {
		return "", fmt.Errorf("render celebration: payload is nil")
	}
	p := envelope.Celebration
	switch p.Kind {
	case domain.CelebrationKindBirthday:
		return renderer.Render(ctx, domain.TemplateKeyCelebrationBirthday, "", p)
	case domain.CelebrationKindAnniversary:
		if p.Years <= 0 {
			return "", fmt.Errorf("render celebration: anniversary years must be positive, got %d", p.Years)
		}
		return renderer.Render(ctx, domain.TemplateKeyCelebrationAnniversary, "", p)
	default:
		return "", fmt.Errorf("render celebration: unknown kind %q", p.Kind)
	}
}
