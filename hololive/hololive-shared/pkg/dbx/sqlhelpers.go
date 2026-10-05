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

	"github.com/georgysavva/scany/v2/pgxscan"
)

// ExecSQL은 query를 그대로 보낸다. ExecSQL/SelectSQL/GetSQL 모두 PostgreSQL native placeholder($n)만 쓰고,
// 가변 길이 목록은 IN (...) 대신 ANY($n::type[])에 slice 하나를 바인딩한다.
// '?'를 $n으로 바꾸던 문자열 치환은 jsonb 연산자 ?, ?|, ?&까지 망가뜨려 폐기했다.
func ExecSQL(ctx context.Context, db Querier, action, query string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", action, err)
	}

	return tag.RowsAffected(), nil
}

func SelectSQL(ctx context.Context, db Querier, dest any, action, query string, args ...any) error {
	if err := pgxscan.Select(ctx, db, dest, query, args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	return nil
}

func GetSQL(ctx context.Context, db Querier, dest any, action, query string, args ...any) (bool, error) {
	err := pgxscan.Get(ctx, db, dest, query, args...)
	if err == nil {
		return true, nil
	}

	if pgxscan.NotFound(err) {
		return false, nil
	}

	return false, fmt.Errorf("%s: %w", action, err)
}
