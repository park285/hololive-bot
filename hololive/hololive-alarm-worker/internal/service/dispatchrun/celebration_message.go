package dispatchrun

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func renderCelebrationMessage(ctx context.Context, renderer *template.Renderer, envelope *domain.AlarmQueueEnvelope) (string, error) {
	if envelope.Celebration == nil {
		return "", errors.New("render celebration: payload is nil")
	}

	p := envelope.Celebration
	if err := validateCelebrationPayload(p); err != nil {
		return "", fmt.Errorf("validate celebration payload: %w", err)
	}

	templateKey, ok := celebrationTemplateKey(p.Kind)
	if !ok {
		return "", fmt.Errorf("render celebration: unknown kind %q", p.Kind)
	}

	out, err := renderer.Render(ctx, templateKey, "", p)
	if err != nil {
		return out, fmt.Errorf("render: %w", err)
	}

	return out, nil
}

func validateCelebrationPayload(payload *domain.CelebrationDispatchPayload) error {
	if payload.Kind == domain.CelebrationKindAnniversary && payload.Years <= 0 {
		return fmt.Errorf("render celebration: anniversary years must be positive, got %d", payload.Years)
	}

	return nil
}

func celebrationTemplateKey(kind domain.CelebrationKind) (domain.TemplateKey, bool) {
	switch kind {
	case domain.CelebrationKindBirthday:
		return domain.TemplateKeyCelebrationBirthday, true
	case domain.CelebrationKindAnniversary:
		return domain.TemplateKeyCelebrationAnniversary, true
	case domain.CelebrationKindBirthdayStream:
		return domain.TemplateKeyCelebrationBirthdayStream, true
	default:
		return "", false
	}
}
