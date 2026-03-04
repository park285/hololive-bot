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
