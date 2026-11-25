package scheduleposter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
)

type Image struct {
	MIME string
	Data []byte
}

func (s *Store) Image(ctx context.Context, id int64) (Image, bool, error) {
	var (
		mime *string
		data []byte
	)

	err := pgxdb.QueryRow(ctx, s.pool, imageSQL, id).Scan(&mime, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return Image{}, false, ErrNotFound
	}

	if err != nil {
		return Image{}, false, fmt.Errorf("load schedule poster image: %w", err)
	}

	if mime == nil {
		return Image{}, false, nil
	}

	return Image{MIME: *mime, Data: data}, true, nil
}

func (s *Store) SaveImage(ctx context.Context, id int64, image Image) error {
	if _, err := pgxdb.Exec(ctx, s.pool, saveImageSQL, id, image.Data, image.MIME); err != nil {
		return fmt.Errorf("save schedule poster image: %w", err)
	}

	return nil
}
