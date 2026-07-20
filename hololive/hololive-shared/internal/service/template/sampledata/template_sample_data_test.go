// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package sampledata_test

import (
	"testing"

	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
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
		{domain.TemplateKeyOutboxMilestone, "Milestone"},
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
