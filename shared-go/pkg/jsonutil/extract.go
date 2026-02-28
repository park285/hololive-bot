// Package jsonutil: LLM 응답에서 JSON을 추출하는 유틸리티
package jsonutil

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

// ErrNoJSONFound: 유효한 JSON을 찾지 못했을 때 반환됩니다.
var ErrNoJSONFound = errors.New("no valid JSON found in response")

// 코드펜스 정규식
var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*([\\s\\S]*?)```")

// Extract: LLM 응답에서 JSON을 추출합니다.
// 1. 코드펜스 내 JSON 우선 시도
// 2. 브라켓 매칭으로 폴백
func Extract(text string) ([]byte, error) {
	text = strings.TrimSpace(text)

	// 1. 코드펜스 우선
	if matches := fenceRe.FindStringSubmatch(text); len(matches) > 1 {
		candidate := strings.TrimSpace(matches[1])
		if json.Valid([]byte(candidate)) {
			return []byte(candidate), nil
		}
	}

	// 2. 브라켓 매칭 폴백
	return extractFirstJSON(text)
}

// ExtractToMap: LLM 응답에서 JSON을 추출하고 map으로 파싱합니다.
func ExtractToMap(text string) (map[string]any, error) {
	data, err := Extract(text)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}
	return result, nil
}

// extractFirstJSON: 텍스트에서 첫 번째 유효한 JSON object/array를 추출합니다.
// 문자열 내 괄호와 이스케이프를 정확히 처리합니다.
func extractFirstJSON(text string) ([]byte, error) {
	b := []byte(text)
	for i := range b {
		if b[i] != '{' && b[i] != '[' {
			continue
		}
		end := findMatchingEnd(b, i)
		if end == -1 {
			continue
		}
		candidate := b[i : end+1]
		if json.Valid(candidate) {
			return candidate, nil
		}
	}
	return nil, ErrNoJSONFound
}

// findMatchingEnd: 문자열/이스케이프를 인식하여 매칭되는 닫는 괄호 위치를 반환합니다.
func findMatchingEnd(b []byte, start int) int {
	open := b[start]
	var closeBracket byte
	if open == '{' {
		closeBracket = '}'
	} else {
		closeBracket = ']'
	}

	depth := 0
	inString := false
	escape := false

	for i := start; i < len(b); i++ {
		c := b[i]

		if inString {
			if escape {
				escape = false
				continue
			}
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}

		// 문자열 밖
		switch c {
		case '"':
			inString = true
		case open:
			depth++
		case closeBracket:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
