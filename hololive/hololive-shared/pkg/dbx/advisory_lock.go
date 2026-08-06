// Copyright (c) 2026 Kapu
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
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sessionAdvisoryUnlockTimeout = 5 * time.Second
	sessionAdvisoryCloseTimeout  = 5 * time.Second
)

var errSessionAdvisoryUnlock = errors.New("session advisory unlock failed")

// 세션 락이므로 acquire/release가 반드시 같은 연결에서 일어나야 한다. q에
// *pgxpool.Pool을 넘기면 두 문장이 서로 다른 연결로 나가 락이 영구히 남을 수
// 있으니 단일 연결(*pgxpool.Conn, pgx.Tx 등)을 넘긴다. 트랜잭션 종료 시 자동
// 해제되는 pg_try_advisory_xact_lock과 달리 해제가 명시적이라, ctx가 취소돼도
// 별도 timeout 안에서 해제 문장을 반드시 보낸다.
func WithSessionAdvisoryLock(ctx context.Context, q Querier, key int64, fn func(context.Context) error) (acquired bool, err error) {
	if q == nil {
		return false, fmt.Errorf("session advisory lock querier is nil")
	}

	var locked bool
	if err := q.QueryRow(ctx, sessionAdvisoryLockAcquireSQL, key).Scan(&locked); err != nil {
		return false, fmt.Errorf("acquire session advisory lock %d: %w", key, err)
	}
	if !locked {
		return false, nil
	}

	defer func() {
		unlockErr := releaseSessionAdvisoryLock(ctx, q, key)
		if unlockErr == nil {
			return
		}
		discardAdvisoryLockPoolConnection(ctx, q, key)
		err = errors.Join(err, unlockErr)
	}()

	if fn == nil {
		return true, nil
	}
	return true, fn(ctx)
}

func releaseSessionAdvisoryLock(ctx context.Context, q Querier, key int64) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionAdvisoryUnlockTimeout)
	defer cancel()

	var unlocked bool
	if err := q.QueryRow(releaseCtx, sessionAdvisoryLockReleaseSQL, key).Scan(&unlocked); err != nil {
		return fmt.Errorf("%w: key=%d: %w", errSessionAdvisoryUnlock, key, err)
	}
	if !unlocked {
		return fmt.Errorf("%w: key=%d was not held", errSessionAdvisoryUnlock, key)
	}
	return nil
}

func discardAdvisoryLockPoolConnection(ctx context.Context, q Querier, key int64) {
	conn, ok := q.(*pgxpool.Conn)
	if !ok {
		return
	}

	rawConn := conn.Hijack()
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sessionAdvisoryCloseTimeout)
	defer cancel()
	if err := rawConn.Close(closeCtx); err != nil {
		slog.Default().Warn("close connection after advisory unlock failure",
			slog.Int64("key", key),
			slog.Any("error", err),
		)
	}
}
