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

// alarm_state의 non-owner 소비자가 write 메서드에 도달하지 못하게 하는 capability 경계다.
// alarm.Repository를 직접 넘기면 Add/Remove/ClearByRoom까지 함께 노출되므로, 이 인터페이스를
// 거치는 것이 repository-ownership.allowlist의 readers 선언을 코드로 강제하는 유일한 수단이다.
package alarmread

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Reader interface {
	GetAllChannelIDs(ctx context.Context) ([]string, error)
	LoadAll(ctx context.Context) ([]*domain.Alarm, error)
}
