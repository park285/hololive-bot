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

package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionWithSchemaName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		option Option
		want   string
	}{
		{
			name:   "sets name",
			option: WithSchemaName("member_news_summary"),
			want:   "member_news_summary",
		},
		{
			name:   "empty string is no-op",
			option: WithSchemaName(""),
			want:   "event_summary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options := Options{SchemaName: "event_summary"}
			tt.option(&options)

			assert.Equal(t, tt.want, options.SchemaName)
		})
	}
}

func TestOptionWithTemperature(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		option    Option
		wantValue float64
		wantSet   bool
	}{
		{
			name:      "sets positive temperature",
			option:    WithTemperature(0.7),
			wantValue: 0.7,
			wantSet:   true,
		},
		{
			name:    "zero is no-op",
			option:  WithTemperature(0),
			wantSet: false,
		},
		{
			name:    "negative is no-op",
			option:  WithTemperature(-0.1),
			wantSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options := Options{}
			tt.option(&options)

			if !tt.wantSet {
				assert.Nil(t, options.Temperature)
				return
			}

			require.NotNil(t, options.Temperature)
			assert.Equal(t, tt.wantValue, *options.Temperature)
		})
	}
}

func TestOptionWithWebSearch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{
			name: "enabled",
			want: true,
		},
		{
			name: "disabled",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options := Options{}
			WithWebSearch(tt.want)(&options)

			require.NotNil(t, options.WebSearch)
			assert.Equal(t, tt.want, *options.WebSearch)
		})
	}
}

func TestOptionWithChatCompletions(t *testing.T) {
	t.Parallel()

	options := Options{}
	WithChatCompletions()(&options)

	assert.True(t, options.ChatCompletions)
}

func TestOptionWithReasoningEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		option Option
		want   string
	}{
		{
			name:   "sets effort",
			option: WithReasoningEffort("high"),
			want:   "high",
		},
		{
			name:   "empty string is no-op",
			option: WithReasoningEffort(""),
			want:   "xhigh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options := Options{ReasoningEffort: "xhigh"}
			tt.option(&options)

			assert.Equal(t, tt.want, options.ReasoningEffort)
		})
	}
}

func TestOptionsCompose(t *testing.T) {
	t.Parallel()

	options := Options{SchemaName: "event_summary"}
	for _, option := range []Option{
		WithSchemaName("member_news_summary"),
		WithTemperature(0.2),
		WithWebSearch(false),
		WithChatCompletions(),
		WithReasoningEffort("high"),
	} {
		option(&options)
	}

	require.NotNil(t, options.Temperature)
	require.NotNil(t, options.WebSearch)
	assert.Equal(t, "member_news_summary", options.SchemaName)
	assert.Equal(t, 0.2, *options.Temperature)
	assert.False(t, *options.WebSearch)
	assert.True(t, options.ChatCompletions)
	assert.Equal(t, "high", options.ReasoningEffort)
}
