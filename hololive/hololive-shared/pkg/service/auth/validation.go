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

package auth

import (
	"net/mail"
	"regexp"

	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
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
