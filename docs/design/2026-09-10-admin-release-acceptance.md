# 관리자 교체의 성능 예외와 출시 승인

2026-09-10 사용자 지시에 따라 `DEC-20260910-hololive-admin-performance-release-exception`을 accepted로 기록합니다. 기존 `DEC-20260909-hololive-admin-bigbang-replacement`의 구조·보안·upstream 보존 경계는 유지합니다.

사용자는 최적화 뒤 관찰된 RSS 회복 실패를 수용하고, 적대적 리뷰 후 커밋·푸시·운영 반영하도록 승인했습니다. 이전 장시간 대기 축소 지시와 함께 이번 성능 판정에는 완료된 단축 비교·관찰을 사용합니다. 정규 12회/60분과 시작 RSS 복귀가 통과한 것으로 바꾸지 않습니다. GC와 OS 메모리 보유가 관찰에 영향을 준다는 진단은 있으나, 증가 전체의 원인이나 누수 부재는 확정하지 않습니다.

예외는 이번 관리자 교체의 성능·자원 조건에 한정합니다. 인증·CSRF·세대·변경 결과·Docker 권한·공급망·구형 종료·전체 복구 조건을 면제하지 않습니다. 추가 적대적 리뷰에서 확인한 I/O 원인 유실은 수정하고 공개 발행 검사와 최종 artifact 검증을 수행합니다.

승인된 대상과 전환·검증 순서는 [출시 실행 기록](../../admin-dashboard/docs/bigbang/release-progress.md)에 있습니다. [최적화와 원래 실패](../../admin-dashboard/docs/bigbang/performance-resolution.md), [적대적 리뷰](../../admin-dashboard/docs/bigbang/release-review.md)를 보존합니다. 배포·rollback 수용 또는 새 OOM·연결 누수·지속적인 증가·성능 예산 초과가 관찰되면 이 예외를 다시 검토합니다.
