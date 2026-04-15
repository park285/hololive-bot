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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
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

func (r *Repository) FindByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	query := `
		SELECT id, slug, channel_id, english_name, japanese_name, korean_name,
		       status, is_graduated, aliases, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE channel_id = $1
		LIMIT 1
	`

	var (
		id           int
		slug         string
		channelIDVal *string
		englishName  string
		japaneseName *string
		koreanName   *string
		status       string
		isGraduated  bool
		aliasesJSON  []byte
		org          string
		suborg       *string
		syncSource   string
		twitchUserID *string
	)

	err := r.pool.QueryRow(ctx, query, channelID).Scan(
		&id, &slug, &channelIDVal, &englishName, &japaneseName, &koreanName,
		&status, &isGraduated, &aliasesJSON, &org, &suborg, &syncSource, &twitchUserID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query member by channel_id: %w", err)
	}

	return r.scanMember(id, slug, channelIDVal, englishName, japaneseName, koreanName, status, isGraduated, aliasesJSON, nil, org, suborg, syncSource, twitchUserID)
}

func (r *Repository) FindByName(ctx context.Context, name string) (*domain.Member, error) {
	query := `
		SELECT id, slug, channel_id, english_name, japanese_name, korean_name,
		       status, is_graduated, aliases, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE english_name = $1
		LIMIT 1
	`

	var (
		id           int
		slug         string
		channelID    *string
		englishName  string
		japaneseName *string
		koreanName   *string
		status       string
		isGraduated  bool
		aliasesJSON  []byte
		org          string
		suborg       *string
		syncSource   string
		twitchUserID *string
	)

	err := r.pool.QueryRow(ctx, query, name).Scan(
		&id, &slug, &channelID, &englishName, &japaneseName, &koreanName,
		&status, &isGraduated, &aliasesJSON, &org, &suborg, &syncSource, &twitchUserID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query member by name: %w", err)
	}

	return r.scanMember(id, slug, channelID, englishName, japaneseName, koreanName, status, isGraduated, aliasesJSON, nil, org, suborg, syncSource, twitchUserID)
}

func (r *Repository) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	query := `
		SELECT m.id, m.slug, m.channel_id, m.english_name, m.japanese_name, m.korean_name,
		       m.status, m.is_graduated, m.aliases, m.org, m.suborg, m.sync_source, m.twitch_user_id
		FROM members m
		WHERE m.aliases->'ko' ? $1
		   OR m.aliases->'ja' ? $1
		   OR m.english_name ILIKE $1
		   OR m.japanese_name ILIKE $1
		   OR m.korean_name ILIKE $1
		LIMIT 1
	`

	var (
		id           int
		slug         string
		channelID    *string
		englishName  string
		japaneseName *string
		koreanName   *string
		status       string
		isGraduated  bool
		aliasesJSON  []byte
		org          string
		suborg       *string
		syncSource   string
		twitchUserID *string
	)

	err := r.pool.QueryRow(ctx, query, alias).Scan(
		&id, &slug, &channelID, &englishName, &japaneseName, &koreanName,
		&status, &isGraduated, &aliasesJSON, &org, &suborg, &syncSource, &twitchUserID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query member by alias: %w", err)
	}

	return r.scanMember(id, slug, channelID, englishName, japaneseName, koreanName, status, isGraduated, aliasesJSON, nil, org, suborg, syncSource, twitchUserID)
}

func (r *Repository) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	query := `
		SELECT channel_id
		FROM members
		WHERE channel_id IS NOT NULL
		ORDER BY english_name
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query channel ids: %w", err)
	}
	defer rows.Close()

	channelIDs := make([]string, 0, 256)
	for rows.Next() {
		var channelID string
		if err := rows.Scan(&channelID); err != nil {
			r.logger.Warn("Failed to scan channel ID", slog.Any("error", err))
			continue
		}
		channelIDs = append(channelIDs, channelID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return channelIDs, nil
}

func (r *Repository) GetAllMembers(ctx context.Context) ([]*domain.Member, error) {
	query := `
		SELECT id, slug, channel_id, english_name, japanese_name, korean_name,
		       status, is_graduated, aliases, photo, org, suborg, sync_source, twitch_user_id
		FROM members
		ORDER BY english_name
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all members: %w", err)
	}
	defer rows.Close()

	return r.collectAllMembersFromRows(rows)
}

func (r *Repository) GetMembersWithPhoto(ctx context.Context, channelIDs []string) (map[string]*domain.Member, error) {
	if len(channelIDs) == 0 {
		return make(map[string]*domain.Member), nil
	}

	query := `
		SELECT id, channel_id, english_name, japanese_name, korean_name,
		       is_graduated, aliases, photo, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE channel_id = ANY($1::text[])
	`

	rows, err := r.pool.Query(ctx, query, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query members with photo: %w", err)
	}
	defer rows.Close()

	return r.collectMembersWithPhotoFromRows(rows)
}

func (r *Repository) collectAllMembersFromRows(rows pgx.Rows) ([]*domain.Member, error) {
	var (
		members []*domain.Member
		rowErrs []error
	)

	for rows.Next() {
		var (
			id           int
			slug         string
			channelID    *string
			englishName  string
			japaneseName *string
			koreanName   *string
			status       string
			isGraduated  bool
			aliasesJSON  []byte
			photo        *string
			org          string
			suborg       *string
			syncSource   string
			twitchUserID *string
		)

		if err := rows.Scan(&id, &slug, &channelID, &englishName, &japaneseName, &koreanName,
			&status, &isGraduated, &aliasesJSON, &photo, &org, &suborg, &syncSource, &twitchUserID); err != nil {
			rowErrs = append(rowErrs, fmt.Errorf("failed to scan member row: %w", err))
			continue
		}

		member, err := r.scanMember(id, slug, channelID, englishName, japaneseName, koreanName, status, isGraduated, aliasesJSON, photo, org, suborg, syncSource, twitchUserID)
		if err != nil {
			rowErrs = append(rowErrs, fmt.Errorf("failed to parse member row %q: %w", englishName, err))
			continue
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		rowErrs = append(rowErrs, fmt.Errorf("rows iteration error: %w", err))
	}

	if len(rowErrs) > 0 {
		return members, errors.Join(rowErrs...)
	}

	return members, nil
}

func (r *Repository) collectMembersWithPhotoFromRows(rows pgx.Rows) (map[string]*domain.Member, error) {
	result := make(map[string]*domain.Member)
	var rowErrs []error

	for rows.Next() {
		var (
			id           int
			channelID    *string
			englishName  string
			japaneseName *string
			koreanName   *string
			isGraduated  bool
			aliasesJSON  []byte
			photo        *string
			org          string
			suborg       *string
			syncSource   string
			twitchUserID *string
		)

		if err := rows.Scan(&id, &channelID, &englishName, &japaneseName, &koreanName,
			&isGraduated, &aliasesJSON, &photo, &org, &suborg, &syncSource, &twitchUserID); err != nil {
			rowErrs = append(rowErrs, fmt.Errorf("failed to scan member row: %w", err))
			continue
		}

		member, err := r.scanMemberWithPhoto(id, channelID, englishName, japaneseName, koreanName, isGraduated, aliasesJSON, photo, org, suborg, syncSource, twitchUserID)
		if err != nil {
			rowErrs = append(rowErrs, fmt.Errorf("failed to parse member row %q: %w", englishName, err))
			continue
		}

		if channelID != nil {
			result[*channelID] = member
		}
	}

	if err := rows.Err(); err != nil {
		rowErrs = append(rowErrs, fmt.Errorf("rows iteration error: %w", err))
	}

	if len(rowErrs) > 0 {
		return result, errors.Join(rowErrs...)
	}

	return result, nil
}

func (r *Repository) GetMemberWithPhotoByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	query := `
		SELECT id, channel_id, english_name, japanese_name, korean_name,
		       is_graduated, aliases, photo, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE channel_id = $1
		LIMIT 1
	`

	var (
		id           int
		chID         *string
		englishName  string
		japaneseName *string
		koreanName   *string
		isGraduated  bool
		aliasesJSON  []byte
		photo        *string
		org          string
		suborg       *string
		syncSource   string
		twitchUserID *string
	)

	err := r.pool.QueryRow(ctx, query, channelID).Scan(
		&id, &chID, &englishName, &japaneseName, &koreanName,
		&isGraduated, &aliasesJSON, &photo, &org, &suborg, &syncSource, &twitchUserID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query member by channel_id: %w", err)
	}

	return r.scanMemberWithPhoto(id, chID, englishName, japaneseName, koreanName, isGraduated, aliasesJSON, photo, org, suborg, syncSource, twitchUserID)
}

// scanMember: DB 조회 결과를 domain.Member로 변환함
func (r *Repository) scanMember(
	id int,
	_ string,
	channelID *string,
	englishName string,
	japaneseName *string,
	koreanName *string,
	_ string,
	isGraduated bool,
	aliasesJSON []byte,
	photo *string,
	org string,
	suborg *string,
	syncSource string,
	twitchUserID *string,
) (*domain.Member, error) {
	return r.scanMemberWithPhoto(id, channelID, englishName, japaneseName, koreanName, isGraduated, aliasesJSON, photo, org, suborg, syncSource, twitchUserID)
}

// scanMemberWithPhoto: DB 조회 결과를 domain.Member로 변환 (photo 포함)
func (r *Repository) scanMemberWithPhoto(
	id int,
	channelID *string,
	englishName string,
	japaneseName *string,
	koreanName *string,
	isGraduated bool,
	aliasesJSON []byte,
	photo *string,
	org string,
	suborg *string,
	syncSource string,
	twitchUserID *string,
) (*domain.Member, error) {
	var aliases domain.Aliases
	if err := json.Unmarshal(aliasesJSON, &aliases); err != nil {
		return nil, fmt.Errorf("failed to unmarshal aliases: %w", err)
	}

	member := &domain.Member{
		ID:          id,
		Name:        englishName,
		Aliases:     &aliases,
		IsGraduated: isGraduated,
		Org:         org,
		SyncSource:  syncSource,
	}

	if channelID != nil {
		member.ChannelID = *channelID
	}
	if japaneseName != nil {
		member.NameJa = *japaneseName
	}
	if koreanName != nil {
		member.NameKo = *koreanName
	}
	if photo != nil {
		member.Photo = *photo
	}
	if suborg != nil {
		member.Suborg = *suborg
	}
	if twitchUserID != nil {
		member.TwitchUserID = *twitchUserID
	}

	return member, nil
}

func (r *Repository) AddAlias(ctx context.Context, memberID int, aliasType string, alias string) error {
	if aliasType != "ko" && aliasType != "ja" {
		return fmt.Errorf("invalid alias type: %s (must be 'ko' or 'ja')", aliasType)
	}

	path := fmt.Sprintf("{%s}", aliasType)
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("aliases", gorm.Expr(`
			jsonb_set(
				COALESCE(aliases::jsonb, '{}'::jsonb),
				CAST(? AS text[]),
				CASE
					WHEN jsonb_exists(COALESCE(aliases::jsonb -> ?, '[]'::jsonb), CAST(? AS text)) THEN COALESCE(aliases::jsonb -> ?, '[]'::jsonb)
					ELSE COALESCE(aliases::jsonb -> ?, '[]'::jsonb) || jsonb_build_array(?)
				END,
				true
			)
		`, path, aliasType, alias, aliasType, aliasType, alias))
	if result.Error != nil {
		return fmt.Errorf("failed to add alias: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) RemoveAlias(ctx context.Context, memberID int, aliasType string, alias string) error {
	if aliasType != "ko" && aliasType != "ja" {
		return fmt.Errorf("invalid alias type: %s (must be 'ko' or 'ja')", aliasType)
	}

	path := fmt.Sprintf("{%s}", aliasType)
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("aliases", gorm.Expr(`
			jsonb_set(
				COALESCE(aliases::jsonb, '{}'::jsonb),
				CAST(? AS text[]),
				COALESCE(
					(
						SELECT jsonb_agg(elem)
						FROM jsonb_array_elements(COALESCE(aliases::jsonb -> ?, '[]'::jsonb)) AS elem
						WHERE elem <> to_jsonb(CAST(? AS text))
					),
					'[]'::jsonb
				),
				true
			)
		`, path, aliasType, alias))
	if result.Error != nil {
		return fmt.Errorf("failed to remove alias: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}

	return nil
}

func (r *Repository) SetGraduation(ctx context.Context, memberID int, isGraduated bool) error {
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("is_graduated", isGraduated)
	if result.Error != nil {
		return fmt.Errorf("failed to update graduation status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}
	return nil
}

func (r *Repository) UpdateChannelID(ctx context.Context, memberID int, channelID string) error {
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("channel_id", channelID)
	if result.Error != nil {
		return fmt.Errorf("failed to update channel ID: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}
	return nil
}

func (r *Repository) UpdateMemberName(ctx context.Context, memberID int, name string) error {
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("id = ?", memberID).
		Update("english_name", name)
	if result.Error != nil {
		return fmt.Errorf("failed to update member name: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("member %d not found", memberID)
	}
	return nil
}

func (r *Repository) CreateMember(ctx context.Context, member *domain.Member) error {
	aliasesJSON, err := json.Marshal(member.Aliases)
	if err != nil {
		return fmt.Errorf("failed to marshal aliases: %w", err)
	}

	// domain.Member가 Slug를 노출하지 않으므로 Name을 Slug로 사용함
	slug := member.Name

	chID := member.ChannelID
	var chIDPtr *string
	if chID != "" {
		chIDPtr = &chID
	}

	var nameJaPtr *string
	if member.NameJa != "" {
		val := member.NameJa
		nameJaPtr = &val
	}

	var nameKoPtr *string
	if member.NameKo != "" {
		val := member.NameKo
		nameKoPtr = &val
	}

	// Add default values for org/sync_source (Task 1 requirement)
	org := "Hololive" // 기존 API 호환을 위한 기본값
	syncSource := "manual"

	m := Model{
		Slug:         slug,
		ChannelID:    chIDPtr,
		EnglishName:  member.Name,
		JapaneseName: nameJaPtr,
		KoreanName:   nameKoPtr,
		Status:       "active",
		IsGraduated:  member.IsGraduated,
		Aliases:      aliasesJSON,
		Org:          org,
		Suborg:       nil,
		SyncSource:   syncSource,
	}

	if err := r.gormDB.WithContext(ctx).Create(&m).Error; err != nil {
		return fmt.Errorf("failed to create member: %w", err)
	}

	return nil
}

func (r *Repository) UpdatePhoto(ctx context.Context, channelID string, photoURL string) error {
	now := time.Now()
	result := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Where("channel_id = ?", channelID).
		Updates(map[string]any{
			"photo":            photoURL,
			"photo_updated_at": now,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update photo: %w", result.Error)
	}

	return nil
}

func (r *Repository) GetPhotoByChannelID(ctx context.Context, channelID string) (string, error) {
	var member Model
	err := r.gormDB.WithContext(ctx).
		Select("photo").
		Where("channel_id = ?", channelID).
		First(&member).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get photo: %w", err)
	}

	if member.Photo == nil {
		return "", nil
	}

	return *member.Photo, nil
}

// staleThreshold: 이 기간보다 오래된 photo는 재동기화 대상
func (r *Repository) GetMembersNeedingPhotoSync(ctx context.Context, staleThreshold time.Duration) ([]string, error) {
	staleTime := time.Now().Add(-staleThreshold)

	var channelIDs []string
	err := r.gormDB.WithContext(ctx).
		Model(&Model{}).
		Select("channel_id").
		Where("channel_id IS NOT NULL").
		Where("photo IS NULL OR photo_updated_at IS NULL OR photo_updated_at < ?", staleTime).
		Pluck("channel_id", &channelIDs).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get members needing photo sync: %w", err)
	}

	return channelIDs, nil
}

func UpgradePhotoResolution(photoURL string) string {
	if photoURL == "" {
		return ""
	}

	for _, size := range []string{"=s88", "=s240", "=s800", "=s176", "=s68"} {
		if contains(photoURL, size) {
			return replaceFirst(photoURL, size, "=s1024")
		}
	}

	return photoURL
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr) >= 0)
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func replaceFirst(s, old, replacement string) string {
	idx := findSubstring(s, old)
	if idx < 0 {
		return s
	}
	return s[:idx] + replacement + s[idx+len(old):]
}

// 검색 대상: english_name, korean_name, aliases->>'ko', aliases->>'ja'
func (r *Repository) FindAllByName(ctx context.Context, name string) ([]*domain.Member, error) {
	query := `
		SELECT id, slug, channel_id, english_name, japanese_name, korean_name,
		       status, is_graduated, aliases, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE LOWER(english_name) = LOWER($1)
		   OR LOWER(korean_name) = LOWER($1)
		   OR aliases->'ko' @> to_jsonb($1::text)
		   OR aliases->'ja' @> to_jsonb($1::text)
	`

	rows, err := r.pool.Query(ctx, query, name)
	if err != nil {
		return nil, fmt.Errorf("failed to query members by name: %w", err)
	}
	defer rows.Close()

	var members []*domain.Member
	for rows.Next() {
		var (
			id           int
			slug         string
			channelID    *string
			englishName  string
			japaneseName *string
			koreanName   *string
			status       string
			isGraduated  bool
			aliasesJSON  []byte
			org          string
			suborg       *string
			syncSource   string
			twitchUserID *string
		)

		if err := rows.Scan(&id, &slug, &channelID, &englishName, &japaneseName, &koreanName,
			&status, &isGraduated, &aliasesJSON, &org, &suborg, &syncSource, &twitchUserID); err != nil {
			r.logger.Warn("Failed to scan member row", slog.Any("error", err))
			continue
		}

		member, err := r.scanMember(id, slug, channelID, englishName, japaneseName, koreanName, status, isGraduated, aliasesJSON, nil, org, suborg, syncSource, twitchUserID)
		if err != nil {
			r.logger.Warn("Failed to parse member", slog.String("name", englishName), slog.Any("error", err))
			continue
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return members, nil
}

func (r *Repository) FindByNameAndOrg(ctx context.Context, name, org string) (*domain.Member, error) {
	query := `
		SELECT id, slug, channel_id, english_name, japanese_name, korean_name,
		       status, is_graduated, aliases, org, suborg, sync_source, twitch_user_id
		FROM members
		WHERE (LOWER(english_name) = LOWER($1)
		   OR LOWER(korean_name) = LOWER($1)
		   OR aliases->'ko' @> to_jsonb($1::text)
		   OR aliases->'ja' @> to_jsonb($1::text))
		  AND LOWER(org) = LOWER($2)
		LIMIT 1
	`

	var (
		id           int
		slug         string
		channelID    *string
		englishName  string
		japaneseName *string
		koreanName   *string
		status       string
		isGraduated  bool
		aliasesJSON  []byte
		orgVal       string
		suborg       *string
		syncSource   string
		twitchUserID *string
	)

	err := r.pool.QueryRow(ctx, query, name, org).Scan(
		&id, &slug, &channelID, &englishName, &japaneseName, &koreanName,
		&status, &isGraduated, &aliasesJSON, &orgVal, &suborg, &syncSource, &twitchUserID,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query member by name and org: %w", err)
	}

	return r.scanMember(id, slug, channelID, englishName, japaneseName, koreanName, status, isGraduated, aliasesJSON, nil, orgVal, suborg, syncSource, twitchUserID)
}
