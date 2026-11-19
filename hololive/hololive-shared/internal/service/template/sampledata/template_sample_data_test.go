package sampledata_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestTemplateSampleData_AllKeysPresent(t *testing.T) {
	for _, key := range sampledata.GetAllTemplateKeys() {
		t.Run(string(key), func(t *testing.T) {
			data := sampledata.GetTemplateSampleData(key)
			assert.NotNil(t, data, "sample data should exist for key %s", key)
		})
	}
}

func TestTemplateSampleData_OutboxTypes(t *testing.T) {
	tests := []struct {
		key           domain.TemplateKey
		requiredField string
	}{
		{domain.TemplateKeyOutboxShorts, "MemberName"},
		{domain.TemplateKeyOutboxCommunity, "ContentText"},
		{domain.TemplateKeyOutboxVideo, "Title"},
	}

	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			data := sampledata.GetTemplateSampleData(tt.key)
			m, ok := data.(map[string]any)
			assert.True(t, ok, "outbox data should be map[string]any")
			assert.Contains(t, m, tt.requiredField)
		})
	}
}
