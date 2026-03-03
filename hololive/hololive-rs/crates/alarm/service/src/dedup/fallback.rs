use std::{fmt::Display, time::Duration as StdDuration};

use alarm_core::constants::{LOCAL_FALLBACK_CLEANUP_MAX_KEYS, LOCAL_FALLBACK_DEDUP_TTL};
use moka::sync::Cache;

/// Valkey 장애 시 로컬 in-memory dedup 폴백.
///
/// - 키 존재 여부만 추적한다.
/// - TTL은 moka Cache의 `time_to_live`로 일괄 관리한다.
pub(crate) struct LocalDedupFallback {
    keys: Cache<String, ()>,
}

impl LocalDedupFallback {
    pub(crate) fn new() -> Self {
        Self {
            keys: Cache::builder()
                .max_capacity(LOCAL_FALLBACK_CLEANUP_MAX_KEYS as u64)
                .time_to_live(LOCAL_FALLBACK_DEDUP_TTL)
                .build(),
        }
    }

    /// Valkey SETNX 실패(outage) 시 로컬 폴백으로 claim을 시도한다.
    pub(crate) fn try_claim_on_outage<E: Display>(
        &self,
        key: &str,
        ttl: StdDuration,
        err: &E,
    ) -> bool {
        let fallback_ttl = normalize_fallback_ttl(ttl);
        let acquired = self.try_claim(key, fallback_ttl);
        tracing::warn!(
            key,
            fallback_acquired = acquired,
            error = %err,
            "SETNX claim 실패, 로컬 폴백 사용"
        );
        acquired
    }

    /// 로컬 dedup 클레임 시도.
    ///
    /// 현재 구현은 moka의 고정 TTL을 사용하므로 per-key TTL은 무시한다.
    fn try_claim(&self, key: &str, _ttl: StdDuration) -> bool {
        if self.keys.contains_key(key) {
            return false;
        }

        self.keys.insert(key.to_owned(), ());
        true
    }

    /// 로컬 dedup claim 해제.
    pub(crate) fn release_claims(&self, keys: &[String]) {
        for key in keys {
            self.keys.invalidate(key);
        }
    }
}

fn normalize_fallback_ttl(ttl: StdDuration) -> StdDuration {
    if ttl.is_zero() || ttl > LOCAL_FALLBACK_DEDUP_TTL {
        LOCAL_FALLBACK_DEDUP_TTL
    } else {
        ttl
    }
}
