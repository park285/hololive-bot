package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateEmail(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		email string
		want  bool
	}{
		"유효한 이메일":   {email: "user@example.com", want: true},
		"대문자 포함":    {email: "User@Example.COM", want: true},
		"빈 문자열":     {email: "", want: false},
		"@ 없음":      {email: "userexample.com", want: false},
		"도메인 없음":    {email: "user@", want: false},
		"공백 포함":     {email: "user @example.com", want: false},
		"서브도메인":     {email: "user@sub.example.com", want: true},
		"특수문자 로컬파트": {email: "user+tag@example.com", want: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, validateEmail(tc.email))
		})
	}
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		password string
		want     bool
	}{
		"유효한 비밀번호":       {password: strings.Repeat("A", 7) + "1", want: true},
		"최소 길이(8자)":      {password: strings.Repeat("B", 7) + "1", want: true},
		"너무 짧음(7자)":      {password: strings.Repeat("C", 6) + "1", want: false},
		"너무 긴 비밀번호(73B)": {password: strings.Repeat("A", 72) + "1", want: false},
		"최대 길이(72B)":     {password: strings.Repeat("A", 71) + "1", want: true},
		"숫자 없음":          {password: strings.Repeat("D", 12), want: false},
		"문자 없음":          {password: "12345678", want: false},
		"빈 문자열":          {password: "", want: false},
		"대소문자 혼합 + 숫자":   {password: strings.Repeat("E", 3) + strings.Repeat("f", 3) + "12", want: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, validatePassword(tc.password))
		})
	}
}

func TestValidateDisplayName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		want bool
	}{
		"유효한 이름":         {name: "TestUser", want: true},
		"빈 문자열":          {name: "", want: false},
		"공백만":            {name: "   ", want: false},
		"유니코드 이름":        {name: "테스트유저", want: true},
		"최대 길이(64 rune)": {name: strings.Repeat("a", 64), want: true},
		"초과 길이(65 rune)": {name: strings.Repeat("a", 65), want: false},
		"한국어 64자":        {name: strings.Repeat("가", 64), want: true},
		"한국어 65자":        {name: strings.Repeat("가", 65), want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, validateDisplayName(tc.name))
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"소문자 변환":  {input: "User@Example.COM", want: "user@example.com"},
		"공백 제거":   {input: "  user@test.com  ", want: "user@test.com"},
		"이미 정규화됨": {input: "user@test.com", want: "user@test.com"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, normalizeEmail(tc.input))
		})
	}
}

func TestNormalizeDisplayName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"공백 제거": {input: "  TestUser  ", want: "TestUser"},
		"정상 통과": {input: "TestUser", want: "TestUser"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, normalizeDisplayName(tc.input))
		})
	}
}
