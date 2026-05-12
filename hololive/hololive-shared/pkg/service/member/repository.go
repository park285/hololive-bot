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

package member

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

type Model struct {
	ID             int            `gorm:"primaryKey;column:id"`
	Slug           string         `gorm:"column:slug"`
	ChannelID      *string        `gorm:"column:channel_id"`
	EnglishName    string         `gorm:"column:english_name"`
	JapaneseName   *string        `gorm:"column:japanese_name"`
	KoreanName     *string        `gorm:"column:korean_name"`
	Status         string         `gorm:"column:status"`
	IsGraduated    bool           `gorm:"column:is_graduated"`
	Aliases        datatypes.JSON `gorm:"column:aliases;type:jsonb"`
	Photo          *string        `gorm:"column:photo"`
	PhotoUpdatedAt *time.Time     `gorm:"column:photo_updated_at"`
	Org            string         `gorm:"column:org"`
	Suborg         *string        `gorm:"column:suborg"`
	SyncSource     string         `gorm:"column:sync_source"`
	TwitchUserID   *string        `gorm:"column:twitch_user_id"`
}

func (Model) TableName() string {
	return "members"
}

type Repository struct {
	pool   *pgxpool.Pool
	gormDB *gorm.DB
	logger *slog.Logger
}

func NewMemberRepository(postgres database.Client, logger *slog.Logger) *Repository {
	return &Repository{
		pool:   postgres.GetPool(),
		gormDB: postgres.GetGormDB(),
		logger: logger,
	}
}
