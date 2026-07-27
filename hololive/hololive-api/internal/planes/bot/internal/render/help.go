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
	helpCardMaxImages    = 6
)

type HelpCardRenderer struct {
	mu           sync.Mutex
	cachedText   string
	cachedImages [][]byte
}

func NewHelpCardRenderer() *HelpCardRenderer {
	return &HelpCardRenderer{}
}

func (r *HelpCardRenderer) RenderHelpImages(ctx context.Context, text string) ([][]byte, error) {
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
	if text == r.cachedText && len(r.cachedImages) != 0 {
		return cloneHelpImages(r.cachedImages), nil
	}

	images, err := renderHelpCards(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("render help cards: %w", err)
	}
	if err := validateRenderedHelpImages(images); err != nil {
		return nil, err
	}

	r.cachedText = text
	r.cachedImages = cloneHelpImages(images)
	return cloneHelpImages(images), nil
}

func validateRenderedHelpImages(images [][]byte) error {
	if len(images) == 0 || len(images) > helpCardMaxImages {
		return fmt.Errorf("render help cards: image count %d is outside 1..%d", len(images), helpCardMaxImages)
	}
	for index, imageData := range images {
		if len(imageData) == 0 {
			return fmt.Errorf("render help card %d: empty png", index+1)
		}
		if len(imageData) > helpCardMaxPNGBytes {
			return fmt.Errorf("render help card %d: png size %d exceeds %d", index+1, len(imageData), helpCardMaxPNGBytes)
		}
	}
	return nil
}

func cloneHelpImages(images [][]byte) [][]byte {
	cloned := make([][]byte, len(images))
	for index, imageData := range images {
		cloned[index] = bytes.Clone(imageData)
	}
	return cloned
}
