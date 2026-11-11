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

package dedup

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

const nonCanonicalRoomID = "상대방닉네임 님과의 대화"

func debugSink() (*bytes.Buffer, *slog.Logger) {
	var sink bytes.Buffer

	return &sink, slog.New(slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestTryClaimKeyRecordsNeverCarryRoomPlaintext(t *testing.T) {
	scheduled := time.Unix(1785499200, 0).UTC()
	notifyKey := keys.BuildNotifyClaimKey(nonCanonicalRoomID, "stream-1", scheduled, "target")
	logicalKey := keys.BuildLogicalEventClaimKey(nonCanonicalRoomID, "UC_TEST", "stream-1", "title", scheduled, "target")

	t.Run("setnx outage path", func(t *testing.T) {
		sink, logger := debugSink()
		client := cachemocks.NewStrictClient()

		client.SetNXFunc = func(context.Context, string, string, time.Duration) (bool, error) {
			return false, errors.New("valkey unreachable")
		}

		service := NewService(client, logger)
		acquired, _, err := service.TryClaimPair(t.Context(), notifyKey, logicalKey, time.Minute)
		require.Error(t, err)
		require.False(t, acquired)

		// 호출자가 이 오류를 그대로 로그에 남기므로 오류 문구에도 room 원문이 없어야 한다.
		assert.NotContains(t, err.Error(), nonCanonicalRoomID,
			"claim key에 보간된 room 식별자가 그대로 오류 값으로 나가면 안 된다")
		assert.NotContains(t, sink.String(), nonCanonicalRoomID,
			"claim key에 보간된 room 식별자가 그대로 로그 값으로 나가면 안 된다")
	})

	t.Run("setnx success path", func(t *testing.T) {
		sink, logger := debugSink()
		client := cachemocks.NewStrictClient()

		client.SetNXFunc = func(context.Context, string, string, time.Duration) (bool, error) {
			return true, nil
		}

		service := NewService(client, logger)
		acquired, _, err := service.TryClaimPair(t.Context(), notifyKey, logicalKey, time.Minute)
		require.NoError(t, err)
		require.True(t, acquired)

		record := sink.String()
		require.NotEmpty(t, record)
		assert.NotContains(t, record, nonCanonicalRoomID)
	})
}

func TestTryClaimKeyDebugRecordFollowsLoggerLevel(t *testing.T) {
	key := keys.BuildNotifyClaimKey(nonCanonicalRoomID, "stream-1", time.Unix(1785499200, 0).UTC(), "target")

	for _, tc := range []struct {
		name       string
		level      slog.Level
		acquired   bool
		wantRecord bool
	}{
		{name: "debug acquired", level: slog.LevelDebug, acquired: true, wantRecord: true},
		{name: "debug already claimed", level: slog.LevelDebug, acquired: false, wantRecord: true},
		{name: "info acquired", level: slog.LevelInfo, acquired: true, wantRecord: false},
		{name: "info already claimed", level: slog.LevelInfo, acquired: false, wantRecord: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sink bytes.Buffer

			logger := slog.New(slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: tc.level}))
			client := cachemocks.NewStrictClient()

			client.SetNXFunc = func(context.Context, string, string, time.Duration) (bool, error) {
				return tc.acquired, nil
			}

			acquired, err := NewService(client, logger).tryClaimKey(t.Context(), key, time.Minute)
			require.NoError(t, err)
			assert.Equal(t, tc.acquired, acquired)

			if !tc.wantRecord {
				assert.Empty(t, sink.String())

				return
			}

			var record struct {
				Level         string `json:"level"`
				Msg           string `json:"msg"`
				ClaimKeyToken string `json:"claim_key_token"`
				Acquired      bool   `json:"acquired"`
			}

			require.NoError(t, json.Unmarshal(sink.Bytes(), &record))
			assert.Equal(t, slog.LevelDebug.String(), record.Level)
			assert.Equal(t, "dedup claim result", record.Msg)
			assert.Equal(t, privacylog.Pseudonym(key), record.ClaimKeyToken)
			assert.Equal(t, tc.acquired, record.Acquired)
		})
	}
}

func TestTryClaimKeySetNXErrorCarriesTokenAtEveryLevel(t *testing.T) {
	key := keys.BuildNotifyClaimKey(nonCanonicalRoomID, "stream-1", time.Unix(1785499200, 0).UTC(), "target")
	errUnreachable := errors.New("valkey unreachable")

	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo} {
		t.Run(level.String(), func(t *testing.T) {
			var sink bytes.Buffer

			logger := slog.New(slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: level}))
			client := cachemocks.NewStrictClient()

			client.SetNXFunc = func(context.Context, string, string, time.Duration) (bool, error) {
				return false, errUnreachable
			}

			acquired, err := NewService(client, logger).tryClaimKey(t.Context(), key, time.Minute)
			require.ErrorIs(t, err, errUnreachable)
			assert.False(t, acquired)
			assert.Contains(t, err.Error(), "dedup claim setnx: claim_key_token="+privacylog.Pseudonym(key)+": ")
			assert.Empty(t, sink.String(), "오류 경로는 로그 대신 오류 값으로만 토큰을 남긴다")
		})
	}
}
