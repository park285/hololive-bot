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
	"context"
	"reflect"
)

// ScraperProxyToggler: 스크래퍼 스케줄러 프록시 토글 인터페이스
type ScraperProxyToggler interface {
	SetProxyEnabled(enabled bool) int
	ProxyEnabled() (enabled bool, known bool)
}

type scraperProxyRuntimeService interface {
	SetScraperProxyEnabled(enabled bool) bool
	ScraperProxyEnabled() bool
}

func normalizeScraperProxyRuntimeService(service scraperProxyRuntimeService) scraperProxyRuntimeService {
	if service == nil {
		return nil
	}

	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func:
		if value.IsNil() {
			return nil
		}
	}

	return service
}

// SettingsApplier: 설정 변경을 런타임에 적용하는 인터페이스
type SettingsApplier interface {
	ApplyScraperProxy(ctx context.Context, enabled bool) ScraperProxyApplyResult
	ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) AlarmAdvanceMinutesApplyResult
	ApplyMemberNewsWeeklyRunNow(ctx context.Context) MemberNewsWeeklyRunNowResult
	ScraperProxyRuntimeState(requested bool) ScraperProxyRuntimeStateResult
}

// ConfigPublisher는 설정 변경을 다른 런타임으로 전파하는 인터페이스입니다.
type ConfigPublisher interface {
	PublishScraperProxy(ctx context.Context, enabled bool) error
	PublishAlarmAdvanceMinutes(ctx context.Context, minutes int) error
}
