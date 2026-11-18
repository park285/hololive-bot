package dbtest

import "github.com/jackc/pgx/v5/pgxpool"

type PoolSource struct {
	pool *pgxpool.Pool
}

func WrapPool(pool *pgxpool.Pool) PoolSource {
	return PoolSource{pool: pool}
}

func (s PoolSource) GetPool() *pgxpool.Pool {
	return s.pool
}
