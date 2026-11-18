package scheduleposter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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

	err := s.pool.QueryRow(ctx, imageSQL, id).Scan(&mime, &data)
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
	if _, err := s.pool.Exec(ctx, saveImageSQL, id, image.Data, image.MIME); err != nil {
		return fmt.Errorf("save schedule poster image: %w", err)
	}

	return nil
}
