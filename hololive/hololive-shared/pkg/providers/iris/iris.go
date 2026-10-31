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

// Package iris는 Iris egress runtime이 typed IRIS_* 설정으로 room 조회·발송 클라이언트를 만드는 provider를 담는다.
package iris

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	irisclient "github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

// ManagedIrisClient는 Hololive runtime이 room 조회와 종료까지 소유하는 Iris 계약입니다.
type ManagedIrisClient interface {
	irisclient.BotClient
	SendRichText(ctx context.Context, room, markdown string, opts ...irisclient.SendOption) (*irisclient.ReplyAcceptedResponse, error)
	GetRooms(ctx context.Context) (*irisclient.RoomListResponse, error)
	Close() error
}

// ProvideIrisClient는 settings.IrisConfig의 typed 값(Base URL 원천·token·transport·H3 CA·server name)으로
// Iris 발송 클라이언트를 만든다. SDK가 env를 읽지 않으므로 이 설정이 유일한 원천이다.
// 옵션 opts는 기본 옵션 뒤에 적용되며 Base URL은 회전 해석 결과가 항상 이긴다.
func ProvideIrisClient(ctx context.Context, irisConfig *settings.IrisConfig, logger *slog.Logger, opts ...irisclient.ClientOption) (ManagedIrisClient, error) {
	if irisConfig == nil {
		return nil, errors.New("provide iris client: iris config is required")
	}

	client, err := delivery.NewRuntimeIrisClient(ctx, *irisConfig, logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("provide iris client: %w", err)
	}

	return client, nil
}
