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

package database_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

func TestIsDuplicateKeyError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil 에러 - false 반환",
			err:  nil,
			want: false,
		},
		{
			name: "23505 유니크 제약 위반 - true 반환",
			err:  &pgconn.PgError{Code: "23505"},
			want: true,
		},
		{
			name: "23503 외래 키 제약 위반 - false 반환",
			err:  &pgconn.PgError{Code: "23503"},
			want: false,
		},
		{
			name: "일반 에러 - false 반환",
			err:  errors.New("some random error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := database.IsDuplicateKeyError(tt.err)
			if got != tt.want {
				t.Errorf("IsDuplicateKeyError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsNoRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "pgx.ErrNoRows - true 반환",
			err:  pgx.ErrNoRows,
			want: true,
		},
		{
			name: "다른 에러 - false 반환",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "nil 에러 - false 반환",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := database.IsNoRows(tt.err)
			if got != tt.want {
				t.Errorf("IsNoRows(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
