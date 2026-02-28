package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
)

// FetchProfilesRuntime: 프로필 수집 작업을 실행하기 위한 런타임 환경
type FetchProfilesRuntime struct {
	Logger     *slog.Logger
	HTTPClient *http.Client

	cleanup func()
}

// Close: 런타임 리소스를 정리합니다.
func (r *FetchProfilesRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// BuildFetchProfilesRuntime: 프로필 수집 런타임 환경을 구성하고 초기화합니다.
func BuildFetchProfilesRuntime(ctx context.Context) (*FetchProfilesRuntime, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	runtime, cleanup, err := InitializeFetchProfilesRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize fetch profiles runtime: %w", err)
	}
	runtime.cleanup = cleanup

	return runtime, nil
}
