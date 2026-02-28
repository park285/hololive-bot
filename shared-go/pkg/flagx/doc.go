// Package flagx: 엔티티 플래그 관리를 위한 유틸리티 패키지입니다.
//
// flagx 패키지는 Junction Table 방식의 DB 플래그 시스템을 제공합니다.
// 여러 개의 독립적인 플래그를 엔티티에 부착할 수 있으며,
// 변경 이력(마지막 변경자)을 추적할 수 있습니다.
//
// # 주요 타입
//
//   - Flag: 플래그를 나타내는 string 기반 타입
//   - FlagSet: 플래그들의 집합 (O(1) 조회)
//   - FlagRecord: DB에 저장된 플래그 레코드
//   - Repository: 플래그 저장소 인터페이스
//   - PostgresRepository: PostgreSQL 구현체
//
// # 사용 예시
//
// FlagSet 사용:
//
//	fs := flagx.NewFlagSet("premium", "verified")
//	fs.Add("active")
//	if fs.Has("premium") {
//	    // premium 플래그가 존재
//	}
//
// PostgreSQL Repository 사용:
//
//	repo, err := flagx.NewPostgresRepository(pool, "user_flags")
//	if err != nil {
//	    return err
//	}
//
//	// 플래그 설정 (idempotent)
//	err = repo.Set(ctx, "user123", "premium", "trace-abc")
//
//	// 플래그 확인
//	has, err := repo.Has(ctx, "user123", "premium")
//
//	// 엔티티의 모든 플래그 조회
//	records, err := repo.List(ctx, "user123")
//
// # 마이그레이션
//
// migration.sql.tmpl 파일을 사용하여 테이블을 생성합니다.
// 각 엔티티 타입별로 별도의 테이블을 사용해야 합니다.
// (예: user_flags, thread_flags, channel_flags)
//
// # 아키텍처 결정
//
// Junction Table 방식을 채택한 이유:
//   - 변경 이력 추적 가능 (created_at, created_by)
//   - 인덱싱 유연성 (플래그별, 엔티티별 조회)
//   - 정규화된 구조
//
// 비트마스크 대신 Junction Table을 사용합니다.
package flagx
