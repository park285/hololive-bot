package holodex

import (
	"log/slog"
	"testing"
)

func TestProvideHolodexServiceRejectsNilConfig(t *testing.T) {
	t.Parallel()

	service, err := ProvideHolodexService(nil, nil, nil, slog.New(slog.DiscardHandler))
	if err == nil || service != nil {
		t.Fatalf("ProvideHolodexService(nil) = (%v, %v), want (nil, error)", service, err)
	}
}
