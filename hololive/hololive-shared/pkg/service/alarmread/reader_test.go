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

package alarmread_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarmread"
)

type writeCapableStub struct {
	channelIDs []string
	alarms     []*domain.Alarm
	loadErr    error
}

func (s *writeCapableStub) GetAllChannelIDs(context.Context) ([]string, error) {
	return s.channelIDs, nil
}

func (s *writeCapableStub) LoadAll(context.Context) ([]*domain.Alarm, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return s.alarms, nil
}

func (s *writeCapableStub) ClearByRoom(context.Context, string) (int64, error) {
	return 0, nil
}

func TestRestrictDropsWriteCapability(t *testing.T) {
	reader := alarmread.Restrict(&writeCapableStub{})

	if _, ok := reader.(interface {
		ClearByRoom(context.Context, string) (int64, error)
	}); ok {
		t.Fatal("restricted reader must not expose alarm write methods")
	}
}

func TestRestrictForwardsReads(t *testing.T) {
	wantErr := errors.New("load failed")
	source := &writeCapableStub{
		channelIDs: []string{"UC-a", "UC-b"},
		alarms:     []*domain.Alarm{{ChannelID: "UC-a"}},
	}
	reader := alarmread.Restrict(source)

	channelIDs, err := reader.GetAllChannelIDs(context.Background())
	if err != nil {
		t.Fatalf("GetAllChannelIDs returned error: %v", err)
	}
	if len(channelIDs) != 2 || channelIDs[0] != "UC-a" || channelIDs[1] != "UC-b" {
		t.Fatalf("GetAllChannelIDs = %v, want [UC-a UC-b]", channelIDs)
	}

	alarms, err := reader.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll returned error: %v", err)
	}
	if len(alarms) != 1 || alarms[0].ChannelID != "UC-a" {
		t.Fatalf("LoadAll = %v, want one alarm for UC-a", alarms)
	}

	source.loadErr = wantErr
	if _, err := reader.LoadAll(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("LoadAll error = %v, want %v", err, wantErr)
	}
}
