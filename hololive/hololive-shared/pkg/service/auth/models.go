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
	"time"
)

// userModel: auth_users 테이블 매핑 (password_hash는 절대 API로 노출하지 않음)
type userModel struct {
	ID           string  `gorm:"primaryKey;column:id"`
	Email        string  `gorm:"uniqueIndex;column:email"`
	PasswordHash string  `gorm:"column:password_hash"`
	DisplayName  string  `gorm:"column:display_name"`
	AvatarURL    *string `gorm:"column:avatar_url"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (userModel) TableName() string { return "auth_users" }

// passwordResetTokenModel: 비밀번호 재설정 토큰 테이블 매핑
type passwordResetTokenModel struct {
	TokenHash string     `gorm:"primaryKey;column:token_hash"`
	UserID    string     `gorm:"column:user_id"`
	ExpiresAt time.Time  `gorm:"column:expires_at"`
	UsedAt    *time.Time `gorm:"column:used_at"`
	CreatedAt time.Time
}

func (passwordResetTokenModel) TableName() string { return "auth_password_reset_tokens" }

type User struct {
	ID          string
	Email       string
	DisplayName string
	AvatarURL   *string
	CreatedAt   time.Time
}

func toUser(m *userModel) *User {
	if m == nil {
		return nil
	}
	return &User{
		ID:          m.ID,
		Email:       m.Email,
		DisplayName: m.DisplayName,
		AvatarURL:   m.AvatarURL,
		CreatedAt:   m.CreatedAt,
	}
}
