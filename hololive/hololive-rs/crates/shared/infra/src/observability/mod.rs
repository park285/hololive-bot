//! 공통 observability 인프라: tracing 초기화 + (선택) OTEL 연동.
//!
//! 기존 `observability.rs`(단일 파일)를 서브모듈로 분할하되, **기존 public API는 `pub use`로 유지**한다.

pub mod layers;
pub mod logging;

pub use layers::*;
pub use logging::*;
