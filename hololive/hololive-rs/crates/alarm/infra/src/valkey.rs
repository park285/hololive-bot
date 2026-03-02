#[cfg(any(test, feature = "test-support"))]
pub use shared_infra::valkey::MockValkeyClient;
/// Valkey 클라이언트 — shared_infra::valkey 재내보내기
///
/// alarm-service는 shared_infra의 ValkeyClient 트레이트를 직접 사용한다.
/// 에러 타입은 AlarmError → From<SharedError> 변환으로 브리지한다.
pub use shared_infra::valkey::{FredValkeyClient, ValkeyClient};
