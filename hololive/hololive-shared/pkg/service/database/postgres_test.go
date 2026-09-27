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

package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func TestResolvePoolConns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  PostgresConfig
		wantMin int
		wantMax int
	}{
		{
			// plane 검증이 허용하는 MIN=0은 idle 연결 없음으로 그대로 전달돼야 한다.
			name:    "명시적 MIN=0 보존",
			config:  PostgresConfig{PoolMinConns: 0, PoolMaxConns: 4},
			wantMin: 0,
			wantMax: 4,
		},
		{
			// 예전 0→2 치환에서는 pgxdb가 "min conns 2 exceeds max conns 1"로 기동을 거부했다.
			name:    "MIN=0과 MAX=1 조합 허용",
			config:  PostgresConfig{PoolMinConns: 0, PoolMaxConns: 1},
			wantMin: 0,
			wantMax: 1,
		},
		{
			name:    "명시 값 그대로 사용",
			config:  PostgresConfig{PoolMinConns: 3, PoolMaxConns: 6},
			wantMin: 3,
			wantMax: 6,
		},
		{
			name:    "MAX 미설정은 기본 최대값",
			config:  PostgresConfig{PoolMinConns: 1},
			wantMin: 1,
			wantMax: constants.DatabaseConfig.MaxOpenConns,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotMin, gotMax, err := resolvePoolConns(&tt.config)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMin, gotMin)
			assert.Equal(t, tt.wantMax, gotMax)
		})
	}
}

func TestNewPostgresServiceRejectsNegativeMinConns(t *testing.T) {
	t.Parallel()

	// 음수는 pgxdb가 0으로 흡수하므로 연결 시도 전에 거부돼야 한다.
	service, err := NewPostgresService(t.Context(), &PostgresConfig{PoolMinConns: -1, PoolMaxConns: 4}, nil)
	require.ErrorContains(t, err, "postgres pool min conns must not be negative: -1")
	assert.Nil(t, service)
}
