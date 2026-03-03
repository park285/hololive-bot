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

// User: API 응답용 유저 정보
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
