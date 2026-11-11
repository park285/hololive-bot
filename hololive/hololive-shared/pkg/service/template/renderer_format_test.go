package template

import (
	"errors"
	"log/slog"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRenderSelectsFormatBeforeChannelOverride(t *testing.T) {
	pool := dbtest.NewPool(t)
	key, channel := domain.TemplateKeyCmdHelp, "channel-a"

	saveTestTemplate(t, pool, key, nil, "PLAIN DEFAULT")
	saveTestTemplate(t, pool, key, &channel, "PLAIN OVERRIDE")

	if _, err := pool.Exec(t.Context(), `INSERT INTO notification_templates (template_key, channel_id, body, format_variant)
VALUES ($1, NULL, 'RICH DEFAULT', 'richtext')
ON CONFLICT (template_key, format_variant) WHERE channel_id IS NULL DO UPDATE SET body = EXCLUDED.body`, key); err != nil {
		t.Fatalf("save richtext default: %v", err)
	}

	r := NewRenderer(pool, slog.New(slog.DiscardHandler))
	cases := []struct {
		format        domain.TemplateFormat
		channel, want string
	}{
		{domain.TemplateFormatPlain, channel, "PLAIN OVERRIDE"},
		{domain.TemplateFormatRichText, channel, "RICH DEFAULT"},
		{domain.TemplateFormatPlain, "channel-b", "PLAIN DEFAULT"},
		{domain.TemplateFormatRichText, "channel-b", "RICH DEFAULT"},
	}
	requests := make([]RenderRequest, 0, len(cases)+1)

	for _, c := range cases {
		got, err := r.Render(t.Context(), key, c.channel, c.format, nil)
		if err != nil || got != c.want {
			t.Errorf("Render(%s, %s) = %q, %v; want %q", c.channel, c.format, got, err, c.want)
		}

		requests = append(requests, RenderRequest{Key: key, ChannelID: c.channel, Format: c.format})
	}

	invalid := domain.TemplateFormat("markdown")
	if _, err := r.Render(t.Context(), key, channel, invalid, nil); !errors.Is(err, ErrInvalidTemplateFormat) {
		t.Errorf("Render(invalid format) error = %v; want ErrInvalidTemplateFormat", err)
	}

	results, err := r.RenderBatch(t.Context(), append(requests, RenderRequest{Key: key, ChannelID: channel, Format: invalid}))
	if err != nil || len(results) != len(cases)+1 {
		t.Fatalf("RenderBatch() = %+v, %v", results, err)
	}

	for i, c := range cases {
		if results[i].Err != nil || results[i].Text != c.want {
			t.Errorf("batch result %d = %+v; want %q", i, results[i], c.want)
		}
	}

	if last := results[len(cases)]; !errors.Is(last.Err, ErrInvalidTemplateFormat) || last.Text != "" {
		t.Errorf("batch invalid format result = %+v; want ErrInvalidTemplateFormat", last)
	}
}
