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

package alarmcache

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

const nonCanonicalRoomID = "상대방닉네임 님과의 대화"

func TestMarkUpcomingEventNotifiedRecordNeverCarriesRoomPlaintext(t *testing.T) {
	var sink bytes.Buffer
	client := cachemocks.NewStrictClient()
	client.SetFunc = func(context.Context, string, any, time.Duration) error {
		return errors.New("valkey unreachable")
	}

	scheduled := time.Unix(1785499200, 0).UTC()
	state := &State{
		Cache:  client,
		Logger: slog.New(slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	err := state.MarkUpcomingEventNotified(context.Background(), nonCanonicalRoomID, "UC-chan", &domain.Stream{
		ID:             "stream-1",
		Title:          "테스트 방송",
		StartScheduled: &scheduled,
	})
	require.Error(t, err)

	record := sink.String()
	require.NotEmpty(t, record, "실패 경로가 아무것도 남기지 않으면 이 테스트는 회귀를 못 잡는다")
	assert.NotContains(t, record, nonCanonicalRoomID,
		"익명화한 room_id와 같은 레코드에 평문이 있으면 그 한 줄이 de-anonymization 오라클이 된다")
	assert.True(t, strings.Contains(record, "anon:"), "room 식별자는 privacylog token으로 남아야 한다")
}
