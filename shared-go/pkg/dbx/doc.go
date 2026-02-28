// Package dbx: PostgreSQL 연결 공통 모듈
//
// # 개요
//
// dbx는 PostgreSQL 연결을 위한 공통 모듈입니다.
// pgxpool + GORM 듀얼 구조를 지원하며, 다음 기능을 제공합니다:
//
//   - DSN 생성 (UDS/TCP 자동 전환)
//   - 커넥션 풀 설정
//   - Exponential backoff 재시도
//   - Ping/Close 헬퍼
//   - 트랜잭션 헬퍼
//   - PostgreSQL 에러 판별
//   - AutoMigrate 래퍼
//
// # 사용 예시
//
// 기본 사용:
//
//	cfg := dbx.Config{
//		Host:     "localhost",
//		Port:     5432,
//		User:     "myuser",
//		Password: "mypassword",
//		Name:     "mydb",
//	}
//
//	client, err := dbx.Open(ctx, cfg, dbx.DefaultOpenOptions())
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer client.Close()
//
//	// GORM 사용
//	db := client.Gorm()
//	db.First(&user, 1)
//
//	// pgxpool 사용 (raw SQL)
//	pool := client.Pool()
//	pool.QueryRow(ctx, "SELECT 1")
//
// Retry 사용:
//
//	client, err := dbx.OpenWithRetry(ctx, cfg, dbx.DefaultOpenOptions())
//
// 트랜잭션 헬퍼:
//
//	err := dbx.InTx(ctx, client.Gorm(), func(tx *gorm.DB) error {
//		return tx.Create(&user).Error
//	})
//
// 에러 판별:
//
//	if dbx.IsDuplicateKey(err) {
//		// unique constraint 위반 처리
//	}
package dbx
