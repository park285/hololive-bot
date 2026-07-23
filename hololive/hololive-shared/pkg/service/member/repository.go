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

	"github.com/kapu/hololive-shared/pkg/service/database"
)

type Model struct {
	ID             int        `db:"id"`
	Slug           string     `db:"slug"`
	ChannelID      *string    `db:"channel_id"`
	EnglishName    string     `db:"english_name"`
	JapaneseName   *string    `db:"japanese_name"`
	KoreanName     *string    `db:"korean_name"`
	Status         string     `db:"status"`
	IsGraduated    bool       `db:"is_graduated"`
	Aliases        []byte     `db:"aliases"`
	Photo          *string    `db:"photo"`
	PhotoUpdatedAt *time.Time `db:"photo_updated_at"`
	Org            string     `db:"org"`
	Suborg         *string    `db:"suborg"`
	SyncSource     string     `db:"sync_source"`
	TwitchUserID   *string    `db:"twitch_user_id"`
}

type Repository struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

func NewMemberRepository(postgres database.Client, logger *slog.Logger) *Repository {
	return &Repository{
		pool:   postgres.GetPool(),
		logger: logger,
	}
}
