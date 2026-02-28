package auth

import (
	"net/mail"
	"regexp"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

var (
	reHasLetter = regexp.MustCompile(`[A-Za-z]`)
	reHasDigit  = regexp.MustCompile(`\d`)
)

func normalizeEmail(email string) string {
	return stringutil.Normalize(email)
}

func validateEmail(email string) bool {
	if email == "" {
		return false
	}
	_, err := mail.ParseAddress(email)
	return err == nil
}

func validatePassword(password string) bool {
	// bcrypt 입력 길이 제한(72 bytes)을 고려해 너무 긴 비밀번호는 거부한다.
	if len(password) < 8 || len(password) > 72 {
		return false
	}
	if !reHasLetter.MatchString(password) {
		return false
	}
	if !reHasDigit.MatchString(password) {
		return false
	}
	return true
}

func validateDisplayName(name string) bool {
	name = stringutil.TrimSpace(name)
	if name == "" {
		return false
	}
	// 과도한 길이 제한 (UI/로그 안전)
	if len([]rune(name)) > 64 {
		return false
	}
	return true
}
