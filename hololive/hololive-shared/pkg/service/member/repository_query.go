package member

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *Repository) FindByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	query := findByChannelIDSQL

	member, err := r.querySingleMember(ctx, query, channelID)
	if err != nil {
		return nil, fmt.Errorf("query member by channel_id: %w", err)
	}

	return member, nil
}

func (r *Repository) FindByName(ctx context.Context, name string) (*domain.Member, error) {
	query := findByNameSQL

	member, err := r.querySingleMember(ctx, query, name)
	if err != nil {
		return nil, fmt.Errorf("query member by name: %w", err)
	}

	return member, nil
}

func (r *Repository) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	query := findByAliasSQL

	member, err := r.querySingleMember(ctx, query, alias)
	if err != nil {
		return nil, fmt.Errorf("query member by alias: %w", err)
	}

	return member, nil
}

func (r *Repository) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	query := getAllChannelIDsSQL

	rows, err := pgxdb.Query(ctx, r.pool, query)
	if err != nil {
		return nil, fmt.Errorf("query channel ids: %w", err)
	}
	defer rows.Close()

	return collectChannelIDs(rows)
}

func collectChannelIDs(rows pgx.Rows) ([]string, error) {
	channelIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("collect channel ids: %w", err)
	}

	return channelIDs, nil
}

func (r *Repository) GetAllMembers(ctx context.Context) ([]*domain.Member, error) {
	query := getAllMembersSQL

	rows, err := pgxdb.Query(ctx, r.pool, query)
	if err != nil {
		return nil, fmt.Errorf("query all members: %w", err)
	}
	defer rows.Close()

	out, err := collectMembersFromRows(rows, memberIterLabel, "member")
	if err != nil {
		return out, fmt.Errorf("collect all members from rows: %w", err)
	}

	return out, nil
}

func (r *Repository) CountMembers(ctx context.Context) (int, error) {
	var count int

	if err := pgxdb.QueryRow(ctx, r.pool, countMembersSQL).Scan(&count); err != nil {
		return 0, fmt.Errorf("count members: %w", err)
	}

	return count, nil
}
