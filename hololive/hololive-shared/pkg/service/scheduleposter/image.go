package scheduleposter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Image는 포스터 그림 바이트와 내용으로 판정한 MIME이며, MIME은 migration 024의 CHECK대로 image/jpeg·image/png·image/webp다.
type Image struct {
	MIME string
	Data []byte
}

// Image는 포스터에 저장한 그림을 돌려주고, 아직 저장하지 않았으면 false다. 포스터가 없으면 ErrNotFound다.
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

// SaveImage는 그림을 아직 저장하지 않은 검토 대기·확정 포스터에만 저장하고, 거절·만료한 포스터에는 저장하지 않는다.
// 이미 저장한 그림은 바꾸지 않고 revision·검토·행 상태도 바꾸지 않는다.
func (s *Store) SaveImage(ctx context.Context, id int64, image Image) error {
	if _, err := s.pool.Exec(ctx, saveImageSQL, id, image.Data, image.MIME); err != nil {
		return fmt.Errorf("save schedule poster image: %w", err)
	}

	return nil
}
