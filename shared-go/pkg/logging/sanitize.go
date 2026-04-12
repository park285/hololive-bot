// Package logging: 공통 로깅 유틸리티를 제공합니다.
package logging

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// 토큰, 패스워드, 시크릿 등을 자동으로 ***REDACTED***로 대체합니다.
type SanitizeHandler struct {
	inner slog.Handler
}

func NewSanitizeHandler(inner slog.Handler) *SanitizeHandler {
	return &SanitizeHandler{inner: inner}
}

// 민감 키 리스트 (case-insensitive 매칭)
var sensitiveKeys = []string{
	"token",
	"password",
	"secret",
	"key",
	"authorization",
	"cookie",
	"api_key",
	"apikey",
}

// Bearer 토큰 패턴 (Bearer xxx 형식)
var bearerTokenRegex = regexp.MustCompile(`Bearer\s+[A-Za-z0-9._-]+`)

func (h *SanitizeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *SanitizeHandler) Handle(ctx context.Context, record slog.Record) error {
	// 새로운 레코드 생성 (Attrs는 깊은 복사 필요)
	newRecord := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)

	// 모든 Attr를 순회하며 민감정보 마스킹
	record.Attrs(func(attr slog.Attr) bool {
		newRecord.AddAttrs(sanitizeAttr(attr))
		return true
	})

	//nolint:wrapcheck // slog.Handler interface implementation
	return h.inner.Handle(ctx, newRecord)
}

func (h *SanitizeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Attrs도 마스킹 적용
	sanitized := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		sanitized = append(sanitized, sanitizeAttr(attr))
	}
	return &SanitizeHandler{inner: h.inner.WithAttrs(sanitized)}
}

func (h *SanitizeHandler) WithGroup(name string) slog.Handler {
	return &SanitizeHandler{inner: h.inner.WithGroup(name)}
}

// sanitizeAttr: 단일 Attr를 재귀적으로 마스킹합니다.
func sanitizeAttr(attr slog.Attr) slog.Attr {
	// Group인 경우 재귀 처리
	if attr.Value.Kind() == slog.KindGroup {
		groupAttrs := attr.Value.Group()
		sanitized := make([]any, 0, len(groupAttrs))
		for _, groupAttr := range groupAttrs {
			sanitized = append(sanitized, sanitizeAttr(groupAttr))
		}
		return slog.Group(attr.Key, sanitized...)
	}

	// String 값만 마스킹 (숫자/bool은 그대로 유지)
	if attr.Value.Kind() != slog.KindString {
		return attr
	}

	// 민감 키 매칭 (case-insensitive)
	if isSensitiveKey(attr.Key) {
		return slog.String(attr.Key, "***REDACTED***")
	}

	// Bearer 토큰 패턴 마스킹
	strValue := attr.Value.String()
	if bearerTokenRegex.MatchString(strValue) {
		return slog.String(attr.Key, bearerTokenRegex.ReplaceAllString(strValue, "Bearer ***REDACTED***"))
	}

	return attr
}

// isSensitiveKey: 키가 민감정보 키인지 확인합니다 (case-insensitive).
func isSensitiveKey(key string) bool {
	for _, sensitiveKey := range sensitiveKeys {
		if strings.EqualFold(key, sensitiveKey) {
			return true
		}
	}
	return false
}
