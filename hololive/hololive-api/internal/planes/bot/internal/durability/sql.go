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

package durability

import (
	"embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	ErrPoolNotConfigured = errors.New("durability: postgres pool is not configured")
	ErrInvalidArgument   = errors.New("durability: invalid argument")
)

// 123_create_bot_durable_admission_tables.sql의 CHECK와 짝을 이룬다 — 한쪽만 바꾸면 전이가 실패한다.
const (
	roomIDRuneLimit         = 256
	messageIDRuneLimit      = 512
	orderingKeyRuneLimit    = 512
	phaseRuneLimit          = 32
	claimTokenRuneLimit     = 256
	commandKindRuneLimit    = 128
	terminalReasonByteLimit = 512
	lastErrorByteLimit      = 8192
	resultSummaryByteLimit  = 2048
)

const inboxStatusDead = "dead"

var clientRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,160}$`)

func ensurePool(pool *pgxpool.Pool) error {
	if pool == nil {
		return ErrPoolNotConfigured
	}

	return nil
}

func requireIdentity(name, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.Join(ErrInvalidArgument, errors.New(name+" must not be empty"))
	}

	return trimmed, nil
}

func requireBoundedIdentity(name, value string, runeLimit int) (string, error) {
	trimmed, err := requireIdentity(name, value)
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(trimmed) > runeLimit {
		return "", errors.Join(ErrInvalidArgument,
			fmt.Errorf("%s must be at most %d runes", name, runeLimit))
	}

	return trimmed, nil
}

func requireMessageIdentity(value string) (string, error) {
	identity := MessageIdentity(value)
	if identity == "" {
		return "", errors.Join(ErrInvalidArgument, errors.New("message id must not be empty"))
	}
	if utf8.RuneCountInString(identity) > messageIDRuneLimit {
		return "", errors.Join(ErrInvalidArgument,
			fmt.Errorf("message id must be at most %d runes", messageIDRuneLimit))
	}

	return identity, nil
}

func requireRoomID(value string) (string, error) {
	return requireBoundedIdentity("room id", value, roomIDRuneLimit)
}

func requireBoundedCommandKind(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if utf8.RuneCountInString(trimmed) > commandKindRuneLimit {
		return "", errors.Join(ErrInvalidArgument,
			fmt.Errorf("command kind must be at most %d runes", commandKindRuneLimit))
	}

	return trimmed, nil
}

func requireClientRequestID(value string) (string, error) {
	trimmed, err := requireIdentity("client request id", value)
	if err != nil {
		return "", err
	}
	if !clientRequestIDPattern.MatchString(trimmed) {
		return "", errors.Join(ErrInvalidArgument,
			fmt.Errorf("client request id %q must match %s", trimmed, clientRequestIDPattern))
	}

	return trimmed, nil
}

// lease는 밀리초로 절단돼 SQL에 들어가므로, 1ms 미만은 태어나자마자 만료된 lease가 된다.
func leaseMilliseconds(lease time.Duration) (int64, error) {
	if lease < time.Millisecond {
		return 0, errors.Join(ErrInvalidArgument, errors.New("lease duration must be at least 1ms"))
	}

	return lease.Milliseconds(), nil
}

// CHECK 위반은 전이 자체를 실패시켜 행을 claim 상태에 가둔다. 진단 문자열 때문에 원장이 막히는 것보다
// 잘린 진단이 낫고, 잘렸다는 사실과 원래 길이를 남겨 사후 조사에서 오독하지 않게 한다.
func clampColumnText(value string, limitBytes int) string {
	if limitBytes <= 0 || len(value) <= limitBytes {
		return value
	}

	suffix := fmt.Sprintf("...[truncated from %d bytes]", len(value))
	if len(suffix) >= limitBytes {
		return suffix[:limitBytes]
	}

	head := value[:limitBytes-len(suffix)]
	for head != "" {
		r, size := utf8.DecodeLastRuneInString(head)
		if r != utf8.RuneError || size != 1 {
			break
		}
		head = head[:len(head)-1]
	}

	return head + suffix
}
