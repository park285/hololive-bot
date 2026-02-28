// Package flagx: 엔티티 플래그 관리를 위한 유틸리티 패키지입니다.
package flagx

import (
	"context"
	"errors"
	"fmt"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrInvalidTableName: 잘못된 테이블명 에러입니다.
var ErrInvalidTableName = errors.New("flagx: invalid table name")

// DB: 데이터베이스 인터페이스입니다. 테스트 목킹을 위해 사용합니다.
type DB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
	Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error)
}

// PostgresRepository: PostgreSQL 기반 플래그 저장소입니다.
type PostgresRepository struct {
	db        DB
	tableName string
}

// NewPostgresRepository: 새 PostgresRepository를 생성합니다.
// tableName은 알파벳, 숫자, 언더스코어만 허용됩니다.
func NewPostgresRepository(db *pgxpool.Pool, tableName string) (*PostgresRepository, error) {
	if err := validateTableName(tableName); err != nil {
		return nil, err
	}
	return &PostgresRepository{
		db:        db,
		tableName: tableName,
	}, nil
}

// NewPostgresRepositoryWithDB: DB 인터페이스를 직접 받는 생성자입니다.
// 테스트 시 목 DB를 주입할 때 사용합니다.
func NewPostgresRepositoryWithDB(db DB, tableName string) (*PostgresRepository, error) {
	if err := validateTableName(tableName); err != nil {
		return nil, err
	}
	return &PostgresRepository{
		db:        db,
		tableName: tableName,
	}, nil
}

// validateTableName: 테이블명이 유효한지 검사합니다.
func validateTableName(name string) error {
	if name == "" {
		return ErrInvalidTableName
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return ErrInvalidTableName
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return ErrInvalidTableName
			}
		}
	}
	return nil
}

// Set: 엔티티에 플래그를 설정합니다.
func (r *PostgresRepository) Set(ctx context.Context, entityID string, flag Flag, createdBy string) error {
	if err := flag.Validate(); err != nil {
		return fmt.Errorf("flagx: Set: %w", err)
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (entity_id, flag, created_at, created_by)
		VALUES ($1, $2, NOW(), $3)
		ON CONFLICT (entity_id, flag) DO UPDATE 
		SET created_by = EXCLUDED.created_by, created_at = NOW()
	`, r.tableName)

	_, err := r.db.Exec(ctx, query, entityID, string(flag), createdBy)
	if err != nil {
		return fmt.Errorf("flagx: Set: %w", err)
	}
	return nil
}

// Unset: 엔티티에서 플래그를 제거합니다.
func (r *PostgresRepository) Unset(ctx context.Context, entityID string, flag Flag) error {
	if err := flag.Validate(); err != nil {
		return fmt.Errorf("flagx: Unset: %w", err)
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE entity_id = $1 AND flag = $2`, r.tableName)

	_, err := r.db.Exec(ctx, query, entityID, string(flag))
	if err != nil {
		return fmt.Errorf("flagx: Unset: %w", err)
	}
	return nil
}

// Has: 엔티티에 플래그가 존재하는지 확인합니다.
func (r *PostgresRepository) Has(ctx context.Context, entityID string, flag Flag) (bool, error) {
	if err := flag.Validate(); err != nil {
		return false, fmt.Errorf("flagx: Has: %w", err)
	}

	query := fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE entity_id = $1 AND flag = $2)`, r.tableName)

	var exists bool
	err := r.db.QueryRow(ctx, query, entityID, string(flag)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("flagx: Has: %w", err)
	}
	return exists, nil
}

// List: 엔티티의 모든 플래그 레코드를 조회합니다.
func (r *PostgresRepository) List(ctx context.Context, entityID string) ([]FlagRecord, error) {
	query := fmt.Sprintf(`
		SELECT entity_id, flag, created_at, created_by 
		FROM %s 
		WHERE entity_id = $1 
		ORDER BY flag
	`, r.tableName)

	rows, err := r.db.Query(ctx, query, entityID)
	if err != nil {
		return nil, fmt.Errorf("flagx: List: %w", err)
	}
	defer rows.Close()

	var records []FlagRecord
	for rows.Next() {
		var rec FlagRecord
		var flagStr string
		if err := rows.Scan(&rec.EntityID, &flagStr, &rec.CreatedAt, &rec.CreatedBy); err != nil {
			return nil, fmt.Errorf("flagx: List: %w", err)
		}
		rec.Flag = Flag(flagStr)
		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flagx: List: %w", err)
	}

	return records, nil
}

// ListByFlag: 특정 플래그를 가진 모든 엔티티를 조회합니다.
func (r *PostgresRepository) ListByFlag(ctx context.Context, flag Flag) ([]FlagRecord, error) {
	if err := flag.Validate(); err != nil {
		return nil, fmt.Errorf("flagx: ListByFlag: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT entity_id, flag, created_at, created_by 
		FROM %s 
		WHERE flag = $1 
		ORDER BY entity_id
	`, r.tableName)

	rows, err := r.db.Query(ctx, query, string(flag))
	if err != nil {
		return nil, fmt.Errorf("flagx: ListByFlag: %w", err)
	}
	defer rows.Close()

	var records []FlagRecord
	for rows.Next() {
		var rec FlagRecord
		var flagStr string
		if err := rows.Scan(&rec.EntityID, &flagStr, &rec.CreatedAt, &rec.CreatedBy); err != nil {
			return nil, fmt.Errorf("flagx: ListByFlag: %w", err)
		}
		rec.Flag = Flag(flagStr)
		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("flagx: ListByFlag: %w", err)
	}

	return records, nil
}

var _ Repository = (*PostgresRepository)(nil)
