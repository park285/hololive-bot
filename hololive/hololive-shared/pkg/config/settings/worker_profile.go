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

package settings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
	"github.com/park285/shared-go/pkg/workerconfig"
)

const (
	irisWorkerProfileFetchAttempts  = 3
	irisWorkerProfileRetryBaseDelay = 500 * time.Millisecond
)

// Iris 진단 조회 실패로 기동을 막으면 Iris 장애가 소비 런타임의 crash-loop로 번지므로,
// 짧은 재시도 뒤 기본 프로파일로 낙하한다. 프로파일 드리프트는 별도 감시 경로가 본다.
func resolveIrisBotWebhookWorkerProfile(config *IrisConfig, options configLoadOptions) workerconfig.IrisBotWebhookWorkerProfile {
	defaultProfile := workerconfig.DefaultIrisBotWebhookWorkerProfile()
	if !options.FetchIrisWorkerProfile {
		return defaultProfile
	}

	profile, err := fetchIrisBotWebhookWorkerProfileWithRetry(config)
	if err == nil {
		return profile
	}

	slog.Warn("iris_worker_profile_fetch_failed_falling_back_to_default",
		slog.Int("attempts", irisWorkerProfileFetchAttempts),
		slog.Int("default_profile_version", defaultProfile.Version),
		slog.String("default_profile_hash", defaultProfile.ProfileHash()),
		slog.Any("error", err))

	return defaultProfile
}

func fetchIrisBotWebhookWorkerProfileWithRetry(config *IrisConfig) (workerconfig.IrisBotWebhookWorkerProfile, error) {
	var lastErr error
	for attempt := 1; attempt <= irisWorkerProfileFetchAttempts; attempt++ {
		profile, err := fetchIrisBotWebhookWorkerProfile(config)
		if err == nil {
			return profile, nil
		}
		lastErr = err
		if errors.Is(err, workerconfig.ErrWorkerProfileDisabled) {
			break
		}
		if attempt < irisWorkerProfileFetchAttempts {
			time.Sleep(time.Duration(attempt) * irisWorkerProfileRetryBaseDelay)
		}
	}

	return workerconfig.IrisBotWebhookWorkerProfile{}, fmt.Errorf("fetch Iris bot webhook worker profile: %w", lastErr)
}

func fetchIrisBotWebhookWorkerProfile(config *IrisConfig) (profile workerconfig.IrisBotWebhookWorkerProfile, err error) {
	if strings.TrimSpace(config.BotToken) == "" {
		return profile, workerconfig.ErrWorkerProfileDisabled
	}
	baseURL, err := resolveIrisBaseURL(config)
	if err != nil {
		return profile, err
	}
	dialGuard, err := newSettingsIrisH3DialGuard(context.Background(), baseURL, config.HTTPDialTimeout)
	if err != nil {
		return profile, fmt.Errorf("configure Iris H3 dial guard: %w", err)
	}
	irisClient, err := iris.NewClient(
		iris.WithBaseURL(baseURL),
		iris.WithBotToken(config.BotToken),
		iris.WithTransport(sharedenv.String("IRIS_TRANSPORT", "")),
		iris.WithTimeout(config.HTTPTimeout),
		iris.WithDialTimeout(config.HTTPDialTimeout),
		iris.WithResponseHeaderTimeout(config.HTTPResponseHeaderTimeout),
		iris.WithH3DialGuardContext(dialGuard),
	)
	if err != nil {
		return profile, err
	}
	defer func() {
		if closeErr := irisClient.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close Iris client: %w", closeErr))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), config.HTTPTimeout)
	defer cancel()

	diagnostics, err := irisClient.GetRuntimeDiagnostics(ctx)
	if err != nil {
		return profile, err
	}
	envelope, err := workerconfig.DecodeRuntimeWorkerProfileEnvelope(bytes.NewReader(diagnostics))
	if err != nil {
		return profile, err
	}
	return envelope.Profile, nil
}
