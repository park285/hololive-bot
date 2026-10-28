package dbtest

import "github.com/jackc/pgx/v5/pgxpool"

// PoolSource는 GetPool() *pgxpool.Pool로 pool을 받는 production 생성자에 테스트 pool을 넘기는 wrapper다.
// 운영 조립은 database.Client를 넘기므로, 테스트도 같은 생성자를 거쳐 저장소를 만든다.
type PoolSource struct {
	pool *pgxpool.Pool
}

func WrapPool(pool *pgxpool.Pool) PoolSource {
	return PoolSource{pool: pool}
}

func (s PoolSource) GetPool() *pgxpool.Pool {
	return s.pool
}
