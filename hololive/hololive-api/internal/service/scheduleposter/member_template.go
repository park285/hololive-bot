package scheduleposter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var ErrInvalidCardTemplate = errors.New("invalid schedule card template")

func (s *Store) MemberCardTemplate(ctx context.Context, memberID int) (cardtemplate.ID, error) {
	return readMemberCardTemplate(ctx, s.pool, memberID)
}

func readMemberCardTemplate(ctx context.Context, db pgxdb.Querier, memberID int) (cardtemplate.ID, error) {
	var value cardtemplate.ID

	err := pgxdb.QueryRow(ctx, db, getMemberTemplateSQL, memberID, cardtemplate.Classic).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrMemberNotFound
	}

	if err != nil {
		return "", fmt.Errorf("read member schedule card template: %w", err)
	}

	return value, nil
}

func (s *Store) SetMemberCardTemplate(ctx context.Context, memberID int, expected, selected cardtemplate.ID) error {
	if !expected.Valid() || !selected.Valid() {
		return ErrInvalidCardTemplate
	}

	err := dbx.InPgxTxOptions(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		locked, err := pgxdb.Exec(ctx, tx, lockTemplateMemberSQL, memberID)
		if err != nil {
			return fmt.Errorf("lock template member: %w", err)
		}

		if locked.RowsAffected() == 0 {
			return domain.ErrMemberNotFound
		}

		current, err := readMemberCardTemplate(ctx, tx, memberID)
		if err != nil {
			return err
		}

		if current != expected {
			return ErrRevisionConflict
		}

		if current == selected {
			return nil
		}

		if _, err := pgxdb.Exec(ctx, tx, saveMemberTemplateSQL, memberID, selected); err != nil {
			return fmt.Errorf("write member schedule card template: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("set member schedule card template: %w", err)
	}

	return nil
}
