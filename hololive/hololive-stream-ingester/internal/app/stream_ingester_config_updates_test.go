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

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyScraperProxyToggle_NilDeps(t *testing.T) {
	t.Parallel()

	// nil 의존성에서 패닉 없이 실행되어야 함
	assert.NotPanics(t, func() {
		applyScraperProxyToggle(true, nil, nil, nil, testLogger())
	})
}

func TestApplyScraperProxyToggle_EnableDisable(t *testing.T) {
	t.Parallel()

	svc := &fakeYouTubeService{}

	applyScraperProxyToggle(true, svc, nil, nil, testLogger())
	assert.Equal(t, 1, svc.setCalls)
	assert.True(t, svc.lastEnabled)

	applyScraperProxyToggle(false, svc, nil, nil, testLogger())
	assert.Equal(t, 2, svc.setCalls)
	assert.False(t, svc.lastEnabled)
}
