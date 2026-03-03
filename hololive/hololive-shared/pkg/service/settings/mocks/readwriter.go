package mocks

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/service/settings"
)

// ReadWriter is a manual mock for settings.ReadWriter.
type ReadWriter struct {
	GetFunc    func() settings.Settings
	UpdateFunc func(newSettings settings.Settings) error
}

var _ settings.ReadWriter = (*ReadWriter)(nil)

func (m *ReadWriter) Get() settings.Settings {
	if m.GetFunc != nil {
		return m.GetFunc()
	}
	return settings.Settings{}
}

func (m *ReadWriter) Update(newSettings settings.Settings) error {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(newSettings)
	}
	return fmt.Errorf("settings mock: UpdateFunc not set")
}
