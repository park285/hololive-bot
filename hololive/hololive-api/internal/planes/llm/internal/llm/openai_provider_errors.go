package llm

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type safeProviderError struct {
	statusCode int
	code       string
	apiType    string
	errType    string
}

func (e safeProviderError) Error() string {
	parts := []string{"llm provider request failed"}

	parts = appendSafeProviderIntPart(parts, "status_code", e.statusCode)
	parts = appendSafeProviderStringPart(parts, "code", e.code)
	parts = appendSafeProviderStringPart(parts, "api_type", e.apiType)
	parts = appendSafeProviderStringPart(parts, "error_type", e.errType)

	return strings.Join(parts, " ")
}

func appendSafeProviderStringPart(parts []string, key, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}

	return append(parts, key+"="+value)
}

func appendSafeProviderIntPart(parts []string, key string, value int) []string {
	if value <= 0 {
		return parts
	}

	return append(parts, fmt.Sprintf("%s=%d", key, value))
}

func safeLLMProviderError(err error) error {
	if err == nil {
		return nil
	}

	if providerErr, ok := errors.AsType[geminiProviderError](err); ok {
		return safeProviderError{statusCode: providerErr.statusCode, code: providerErr.code, apiType: "gemini", errType: "gemini_provider_error"}
	}

	if providerErr, ok := errors.AsType[safeProviderError](err); ok {
		return providerErr
	}

	return safeProviderError{errType: llmErrorType(err)}
}

func llmProviderErrorAttrs(err error) []slog.Attr {
	if err == nil {
		return nil
	}

	if providerErr, ok := errors.AsType[geminiProviderError](err); ok {
		attrs := []slog.Attr{
			slog.String("error_type", "gemini_provider_error"),
			slog.Bool("provider_error", true),
		}

		attrs = appendStatusCodeAttr(attrs, providerErr.statusCode)

		return appendTrimmedStringAttr(attrs, "error_code", providerErr.code)
	}

	if providerErr, ok := errors.AsType[safeProviderError](err); ok {
		attrs := []slog.Attr{slog.String("error_type", providerErr.errType)}

		attrs = appendStatusCodeAttr(attrs, providerErr.statusCode)
		attrs = appendTrimmedStringAttr(attrs, "error_code", providerErr.code)

		return appendTrimmedStringAttr(attrs, "provider_error_type", providerErr.apiType)
	}

	return []slog.Attr{slog.String("error_type", llmErrorType(err))}
}

func appendStatusCodeAttr(attrs []slog.Attr, statusCode int) []slog.Attr {
	if statusCode <= 0 {
		return attrs
	}

	return append(attrs, slog.Int("status_code", statusCode))
}

func appendTrimmedStringAttr(attrs []slog.Attr, key, value string) []slog.Attr {
	value = strings.TrimSpace(value)
	if value == "" {
		return attrs
	}

	return append(attrs, slog.String(key, value))
}

func llmErrorType(err error) string {
	if err == nil {
		return ""
	}

	return strings.TrimPrefix(fmt.Sprintf("%T", err), "*")
}
