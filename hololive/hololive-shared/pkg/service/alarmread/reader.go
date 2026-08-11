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

package alarmread

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Reader interface {
	GetAllChannelIDs(ctx context.Context) ([]string, error)
	LoadAll(ctx context.Context) ([]*domain.Alarm, error)
}

// alarm.Repository를 Reader로 그대로 넘기면 소비자가 write 메서드로 타입 단언해 되찾을 수 있으므로,
// 메서드 집합을 실제로 잘라내는 이 wrapper를 거쳐야 경계가 성립한다.
func Restrict(source Reader) Reader {
	return restricted{source: source}
}

type restricted struct {
	source Reader
}

func (r restricted) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	return r.source.GetAllChannelIDs(ctx)
}

func (r restricted) LoadAll(ctx context.Context) ([]*domain.Alarm, error) {
	return r.source.LoadAll(ctx)
}
