package render

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestValidateCalendarPhotoConfigRejectsImageBombDimensions(t *testing.T) {
	t.Parallel()

	for _, config := range []image.Config{
		{Width: 0, Height: 1},
		{Width: 1, Height: 0},
		{Width: calendarPhotoMaxDimension + 1, Height: 1},
		{Width: calendarPhotoMaxDimension, Height: 3000},
	} {
		if err := validateCalendarPhotoConfig(config); err == nil {
			t.Fatalf("validateCalendarPhotoConfig(%+v) error = nil", config)
		}
	}

	if err := validateCalendarPhotoConfig(image.Config{Width: 1024, Height: 1024}); err != nil {
		t.Fatalf("valid image config rejected: %v", err)
	}
}

func TestCalendarCardRendererCancelledRenderDoesNotFetchOrPoisonCache(t *testing.T) {
	photoURL := "https://yt3.googleusercontent.com/avatar=s88-c"
	recorder := &calledRoundTripper{body: tinyPNG(t), contentType: "image/png"}
	withCalendarPhotoClient(t, newCalendarPhotoTestClient(recorder))

	entries := []domain.CalendarEntry{{
		Kind: domain.CelebrationKindBirthday,
		Member: &domain.Member{
			ShortKoreanName: "페코라",
			Photo:           photoURL,
		},
		Day: 15,
	}}
	renderer := NewCalendarCardRenderer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	data, err := renderer.RenderCalendarImageContext(ctx, 6, 2026, entries)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled render error = %v, want context.Canceled", err)
	}
	if data != nil {
		t.Fatalf("cancelled render returned %d bytes", len(data))
	}
	if got := recorder.requests.Load(); got != 0 {
		t.Fatalf("cancelled render fetched %d photos, want 0", got)
	}

	data, err = renderer.RenderCalendarImage(6, 2026, entries)
	if err != nil {
		t.Fatalf("subsequent render error = %v", err)
	}
	assertValidPNG(t, data)
	if got := recorder.requests.Load(); got != 1 {
		t.Fatalf("subsequent render fetched %d photos, want 1; cancelled result may have poisoned cache", got)
	}
}
