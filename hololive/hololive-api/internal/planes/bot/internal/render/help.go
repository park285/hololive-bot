package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	helpCardMaxTextBytes = 16 << 10
	helpCardMaxPNGBytes  = 4 << 20
)

type HelpCardRenderer struct {
	mu          sync.Mutex
	cachedText  string
	cachedImage []byte
}

func NewHelpCardRenderer() *HelpCardRenderer {
	return &HelpCardRenderer{}
}

func (r *HelpCardRenderer) RenderHelpImage(ctx context.Context, text string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("help card renderer is nil")
	}
	if ctx == nil {
		return nil, errors.New("help card render context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("help card text is empty")
	}
	if len(text) > helpCardMaxTextBytes {
		return nil, fmt.Errorf("help card text size %d exceeds %d", len(text), helpCardMaxTextBytes)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if text == r.cachedText && len(r.cachedImage) != 0 {
		return bytes.Clone(r.cachedImage), nil
	}

	imageData, err := renderHelpCard(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("render help card: %w", err)
	}
	if len(imageData) == 0 {
		return nil, errors.New("render help card: empty png")
	}
	if len(imageData) > helpCardMaxPNGBytes {
		return nil, fmt.Errorf("render help card: png size %d exceeds %d", len(imageData), helpCardMaxPNGBytes)
	}

	r.cachedText = text
	r.cachedImage = imageData
	return bytes.Clone(imageData), nil
}
