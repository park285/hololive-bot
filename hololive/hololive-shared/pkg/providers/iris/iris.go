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

// Package iris는 Iris egress runtime이 IRIS_* 설정으로 room 조회·발송 클라이언트를 만드는 provider를 담는다.
package iris

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	irisclient "github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

// ManagedIrisClient는 Hololive runtime이 room 조회와 종료까지 소유하는 Iris 계약입니다.
type ManagedIrisClient interface {
	irisclient.Client
	GetRooms(ctx context.Context) (*irisclient.RoomListResponse, error)
	Close() error
}

// ProvideIrisClient - Iris 발송 클라이언트 생성.
func ProvideIrisClient(irisConfig *settings.IrisConfig, logger *slog.Logger, opts ...irisclient.ClientOption) (ManagedIrisClient, error) {
	out, err := provideRuntimeIrisClient(irisConfig, logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("provide runtime iris client: %w", err)
	}

	return out, nil
}

func provideRuntimeIrisClient(irisConfig *settings.IrisConfig, logger *slog.Logger, opts ...irisclient.ClientOption) (*delivery.RuntimeIrisClient, error) {
	if irisConfig == nil {
		return nil, errors.New("provide iris client: iris config is required")
	}

	resolved := irisclient.ResolveClientSDKConfig(opts)
	fallbackBaseURL := strings.TrimSpace(resolved.BaseURL)

	if fallbackBaseURL == "" {
		fallbackBaseURL = strings.TrimSpace(irisConfig.BaseURL)
	}

	baseURLFilePath := strings.TrimSpace(irisConfig.BaseURLFile)
	if fallbackBaseURL == "" && baseURLFilePath == "" {
		return nil, errors.New("provide iris client: IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	botToken := strings.TrimSpace(resolved.BotToken)
	if botToken == "" {
		botToken = strings.TrimSpace(irisConfig.BotToken)
	}

	if botToken == "" {
		return nil, errors.New("provide iris client: bot token is required")
	}

	client := delivery.NewRuntimeIrisClient(
		fallbackBaseURL,
		botToken,
		baseURLFilePath,
		logger,
		opts...,
	)
	if err := client.ValidateBaseURL(); err != nil {
		return nil, fmt.Errorf("provide iris client: %w", err)
	}

	return client, nil
}
