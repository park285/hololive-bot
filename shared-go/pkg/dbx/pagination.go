package dbx

import (
	"encoding/base64"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrInvalidCursor = errors.New("invalid cursor token")
	// ErrInvalidCursorField: cursor field가 안전한 SQL 식별자 형식이 아닐 때 반환됩니다.
	ErrInvalidCursorField = errors.New("invalid cursor field")
)

const maxCursorTokenLength = 4096

var cursorFieldRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// CursorData: 커서 페이지네이션 데이터
type CursorData struct {
	Field     string `json:"f"`
	Value     any    `json:"v"`
	Direction string `json:"d"` // ASC or DESC
}

// EncodeCursor: 커서 데이터를 base64 토큰으로 인코딩
func EncodeCursor(field string, value any, direction string) (string, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return "", errors.New("field is required")
	}
	if !isSafeCursorField(field) {
		return "", fmt.Errorf("%w: %s", ErrInvalidCursorField, field)
	}

	data := CursorData{
		Field:     field,
		Value:     value,
		Direction: normalizeDirection(direction),
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal cursor: %w", err)
	}

	return base64.URLEncoding.EncodeToString(jsonBytes), nil
}

// DecodeCursor: base64 토큰을 CursorData로 디코딩
func DecodeCursor(token string) (*CursorData, error) {
	if token == "" {
		return nil, nil
	}
	if len(token) > maxCursorTokenLength {
		return nil, fmt.Errorf("%w: token too long", ErrInvalidCursor)
	}

	jsonBytes, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: decode error", ErrInvalidCursor)
	}

	var data CursorData
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return nil, fmt.Errorf("%w: unmarshal error", ErrInvalidCursor)
	}

	data.Field = strings.TrimSpace(data.Field)
	if !isSafeCursorField(data.Field) {
		return nil, ErrInvalidCursor
	}
	data.Direction = normalizeDirection(data.Direction)

	return &data, nil
}

// BuildKeysetCondition: 커서 데이터로 WHERE 조건 생성
// 반환: (whereClause, args)
// 예시: ("achieved_at < $1", ["2026-01-20T10:00:00Z"])
func BuildKeysetCondition(cursor *CursorData, paramIndex int) (string, []any) {
	if cursor == nil {
		return "", nil
	}
	field := strings.TrimSpace(cursor.Field)
	if !isSafeCursorField(field) {
		return "", nil
	}

	var op string
	if normalizeDirection(cursor.Direction) == "ASC" {
		op = ">"
	} else {
		op = "<"
	}

	whereClause := fmt.Sprintf("%s %s $%d", field, op, paramIndex)
	return whereClause, []any{cursor.Value}
}

func normalizeDirection(direction string) string {
	if strings.EqualFold(strings.TrimSpace(direction), "ASC") {
		return "ASC"
	}
	return "DESC"
}

func isSafeCursorField(field string) bool {
	return cursorFieldRe.MatchString(field)
}
