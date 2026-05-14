package llm

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/openai/openai-go/v3"
)

type safeProviderError struct {
	statusCode int
	code       string
	param      string
	apiType    string
	errType    string
}

func (e safeProviderError) Error() string {
	parts := []string{"llm provider request failed"}
	parts = appendSafeProviderIntPart(parts, "status_code", e.statusCode)
	parts = appendSafeProviderStringPart(parts, "code", e.code)
	parts = appendSafeProviderStringPart(parts, "api_type", e.apiType)
	parts = appendSafeProviderStringPart(parts, "param", e.param)
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
	if errors.Is(err, errOpenAIEmptyOutput) {
		return safeProviderError{errType: "openai_empty_output"}
	}
	if apiErr, ok := openAIError(err); ok {
		return safeOpenAIProviderError(apiErr)
	}
	return err
}

func safeOpenAIProviderError(apiErr *openai.Error) safeProviderError {
	return safeProviderError{
		statusCode: apiErr.StatusCode,
		code:       strings.TrimSpace(apiErr.Code),
		param:      strings.TrimSpace(apiErr.Param),
		apiType:    strings.TrimSpace(apiErr.Type),
		errType:    llmErrorType(apiErr),
	}
}

func llmProviderErrorAttrs(err error) []slog.Attr {
	if err == nil {
		return nil
	}
	if errors.Is(err, errOpenAIEmptyOutput) {
		return []slog.Attr{slog.String("error_type", "openai_empty_output")}
	}
	if apiErr, ok := openAIError(err); ok {
		return openAIProviderErrorAttrs(apiErr)
	}
	return []slog.Attr{slog.String("error_type", llmErrorType(err))}
}

func openAIProviderErrorAttrs(apiErr *openai.Error) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("error_type", llmErrorType(apiErr)),
		slog.Bool("provider_error", true),
	}
	attrs = appendStatusCodeAttr(attrs, apiErr.StatusCode)
	attrs = appendTrimmedStringAttr(attrs, "error_code", apiErr.Code)
	attrs = appendTrimmedStringAttr(attrs, "provider_error_type", apiErr.Type)
	return appendTrimmedStringAttr(attrs, "provider_error_param", apiErr.Param)
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

func openAIError(err error) (*openai.Error, bool) {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr != nil {
		return apiErr, true
	}
	return nil, false
}

func llmErrorType(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", err), "*")
}
