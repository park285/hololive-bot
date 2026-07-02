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

package dbx

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/georgysavva/scany/v2/pgxscan"
)

func PostgresPlaceholders(query string) string {
	var out strings.Builder
	index := 1
	for i := range len(query) {
		if query[i] != '?' {
			out.WriteByte(query[i])
			continue
		}
		out.WriteByte('$')
		out.WriteString(strconv.Itoa(index))
		index++
	}
	return out.String()
}

func InPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func AnyArgs[T any](values []T) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func ExecSQL(ctx context.Context, db Querier, action, query string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, PostgresPlaceholders(query), args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", action, err)
	}
	return tag.RowsAffected(), nil
}

func SelectSQL(ctx context.Context, db Querier, dest any, action, query string, args ...any) error {
	if err := pgxscan.Select(ctx, db, dest, PostgresPlaceholders(query), args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func GetSQL(ctx context.Context, db Querier, dest any, action, query string, args ...any) (bool, error) {
	err := pgxscan.Get(ctx, db, dest, PostgresPlaceholders(query), args...)
	if err == nil {
		return true, nil
	}
	if pgxscan.NotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("%s: %w", action, err)
}
