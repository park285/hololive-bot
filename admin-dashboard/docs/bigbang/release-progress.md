# 관리자 교체 출시 실행 기록

2026-09-10. `PLN-20260909-hololive-admin-bigbang-replacement` T10/T11, `DEC-20260909-hololive-admin-bigbang-replacement`, `DEC-20260910-hololive-admin-performance-release-exception`.

## 승인 범위와 성능 예외

사용자는 관찰된 RSS 회복 실패를 수용하고, 적대적 리뷰 뒤 현재 관리자 교체를 커밋·푸시·운영 반영하도록 승인했습니다. 이전 장시간 대기 축소 지시도 유지합니다. 정규 12회/60분 결과를 새로 통과로 만들지 않으며, 4회 비교·10분 관찰과 [기존 실패 원본](performance-resolution.md)을 보존합니다. GC와 OS 메모리 보유 영향은 진단 근거가 있지만, RSS 증가 전체의 원인이나 누수 부재를 확정하지 않습니다.

대상은 `hololive-bot`의 관리자 교체 변경과 관련 메타 저장소 기록, 각 저장소의 `origin/main`, 중앙 `hololive-osaka`의 `admin-dashboard`·`admin-docker-proxy`입니다. 첫 세대 전환에 필요한 관리자 ingress 정비/개방, 기존 source 제한 적용, 관리자 세션 폐기와 새 signing secret, 이미지/배포 파일 전송·설치 및 필요한 재생성을 포함합니다. 업무 API·알람 worker·collector·업무 DB의 동작이나 데이터를 변경하지 않습니다. 기존 password hash·Holo API key·Valkey credential은 유지합니다.

순서는 리뷰·수정 → 필수 검사·커밋·푸시 → 깨끗한 검토 revision의 로컬 arm64 빌드와 검증 → 구형 복구 artifact 보존·전송 → source 제한 확인 → 관리자 정비·구형 종료·세대 전환 → smoke·개방·300초 관찰입니다. 실패 시 owning runbook의 정비 유지·전체 rollback 경계를 적용하며 불명 효과를 재실행하지 않습니다.

## 현재 증거

- [적대적 리뷰](release-review.md): P2 I/O 원인 유실 1건 수정, 미해결 출시 차단 결함 없음. 부모가 Holo adapter 전체 검사를 실행해 통과했습니다.
- 최종 소스의 publish gate, 커밋/원격 ref, 운영용 arm64 artifact와 실제 전환은 아직 진행 중입니다. 과거 fixture image의 revision `unknown`을 운영 revision으로 사용하지 않습니다.
- 시작 시 중앙 `admin-dashboard`는 healthy이며 실제 image는 `sha256:17b56c3a48b055fdab3d60691ec40e90de1f32e65691e1be0718b316b84e4fb7`, revision `6ba2ede142501eb5bc436f9d1af2587a3f1d81b0`입니다. ingress source 제한 service는 disabled/inactive입니다. 이 상태는 현재 배포 준비에서 해소할 항목입니다.

성능 예외를 제외한 출시 조건을 자동으로 면제하지 않습니다. 실제 검증과 전환 결과에 따라 이 기록 및 PLN/DEC를 갱신합니다.
