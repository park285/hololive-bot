package member

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewMemberRepository(postgres database.Client) *Repository {
	return &Repository{
		pool: postgres.GetPool(),
	}
}
