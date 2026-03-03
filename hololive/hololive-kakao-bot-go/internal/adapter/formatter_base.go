package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// ResponseFormatter: 봇의 응답 메시지를 생성하는 포맷터 (카카오톡 UI 템플릿 적용)
type ResponseFormatter struct {
	prefix   string
	renderer *template.Renderer
}

func (f *ResponseFormatter) render(ctx context.Context, key domain.TemplateKey, data any) (string, error) {
	rendered, err := f.renderer.Render(ctx, key, "", data)
	if err != nil {
		return "", fmt.Errorf("render template %s: %w", key, err)
	}
	return strings.TrimRight(rendered, "\n"), nil
}

func splitTemplateInstruction(rendered string) (instruction string, body string) {
	trimmed := strings.TrimLeft(rendered, "\r\n")
	if trimmed == "" {
		return "", ""
	}

	parts := strings.SplitN(trimmed, "\n", 2)
	instruction = stringutil.TrimSpace(strings.TrimSuffix(parts[0], "\r"))
	if len(parts) < 2 {
		return instruction, ""
	}

	body = strings.TrimLeft(parts[1], "\r\n")
	return instruction, body
}

// NewResponseFormatter: 새로운 ResponseFormatter 인스턴스를 생성합니다.
func NewResponseFormatter(prefix string, renderer *template.Renderer) *ResponseFormatter {
	if stringutil.TrimSpace(prefix) == "" {
		prefix = "!"
	}
	return &ResponseFormatter{prefix: prefix, renderer: renderer}
}

// Prefix: 현재 설정된 명령어 접두사를 반환합니다.
func (f *ResponseFormatter) Prefix() string {
	if f == nil {
		return "!"
	}
	if trimmed := stringutil.TrimSpace(f.prefix); trimmed != "" {
		return trimmed
	}
	return "!"
}

// FormatError: 에러 메시지를 사용자 친화적인 포맷으로 변환합니다.
func (f *ResponseFormatter) FormatError(message string) string {
	return ErrorMessage(message)
}

// MemberNotFound: 멤버를 찾을 수 없을 때의 에러 메시지를 생성합니다.
func (f *ResponseFormatter) MemberNotFound(memberName string) string {
	return f.FormatError(fmt.Sprintf("'%s' 멤버를 찾을 수 없습니다.", memberName))
}
