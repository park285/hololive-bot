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
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// 잘못된 pool 크기는 연결 시도 전에 거부돼야 한다.
	service, err := NewPostgresService(t.Context(), &PostgresConfig{PoolMinConns: -1, PoolMaxConns: 4}, nil)
	require.ErrorContains(t, err, "postgres pool min conns must not be negative: -1")
	assert.Nil(t, service)
}

func TestNewPostgresServiceRejectsInvalidPoolBounds(t *testing.T) {
	t.Parallel()

	for _, config := range []PostgresConfig{{PoolMaxConns: 0}, {PoolMaxConns: -1}, {PoolMinConns: 3, PoolMaxConns: 2}} {
		service, err := NewPostgresService(t.Context(), &config, nil)
		require.Error(t, err)
		assert.Nil(t, service)
	}
}

func TestNewPostgresServiceRejectsReserveWithoutOrdinaryCapacity(t *testing.T) {
	t.Parallel()

	service, err := NewPostgresService(t.Context(), &PostgresConfig{
		Host: "127.0.0.1", Port: 1, User: "test", Database: "test", SSLMode: "disable",
		PoolMinConns: 0, PoolMaxConns: 1,
		AcquirePolicy: &pgxdb.AcquirePolicy{ReservedConns: 1, Timeout: 5 * time.Second},
	}, nil)
	require.ErrorContains(t, err, "acquire policy")
	require.Nil(t, service)
}
