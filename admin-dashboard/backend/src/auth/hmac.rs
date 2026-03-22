use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use hmac::{Hmac, Mac};
use rand::Rng;
use sha2::Sha256;

type HmacSha256 = Hmac<Sha256>;

#[allow(clippy::expect_used)] // HMAC-SHA256은 모든 키 길이를 허용
pub fn sign_session_id(session_id: &str, secret: &str) -> String {
    assert!(!secret.is_empty(), "session secret must not be empty");
    let mut mac =
        HmacSha256::new_from_slice(secret.as_bytes()).expect("HMAC accepts any key length");
    mac.update(session_id.as_bytes());
    let signature = URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes());
    format!("{session_id}.{signature}")
}

#[allow(clippy::expect_used)] // HMAC-SHA256은 모든 키 길이를 허용
pub fn validate_session_signature(full_id: &str, secret: &str) -> (String, bool) {
    assert!(!secret.is_empty(), "session secret must not be empty");
    let Some((session_id, provided_sig)) = full_id.split_once('.') else {
        return (String::new(), false);
    };

    let mut mac =
        HmacSha256::new_from_slice(secret.as_bytes()).expect("HMAC accepts any key length");
    mac.update(session_id.as_bytes());
    let expected_sig = URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes());

    if !constant_time_eq(provided_sig.as_bytes(), expected_sig.as_bytes()) {
        return (String::new(), false);
    }
    (session_id.to_string(), true)
}

pub fn generate_session_id() -> String {
    let mut bytes = [0u8; 32];
    rand::rng().fill_bytes(&mut bytes);
    hex::encode(bytes)
}

pub fn truncate_session_id(session_id: &str) -> String {
    if session_id.len() <= 8 {
        return session_id.to_string();
    }
    format!("{}...", &session_id[..8])
}

fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    a.iter()
        .zip(b.iter())
        .fold(0u8, |acc, (x, y)| acc | (x ^ y))
        == 0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_sign_and_validate_roundtrip() {
        let session_id = "abcdef1234567890";
        let secret = "test-secret-key";
        let signed = sign_session_id(session_id, secret);

        assert!(signed.contains('.'));
        let (extracted_id, valid) = validate_session_signature(&signed, secret);
        assert!(valid);
        assert_eq!(extracted_id, session_id);
    }

    #[test]
    fn test_validate_wrong_secret() {
        let signed = sign_session_id("session123", "secret1");
        let (_, valid) = validate_session_signature(&signed, "secret2");
        assert!(!valid);
    }

    #[test]
    fn test_validate_tampered_signature() {
        let _signed = sign_session_id("session123", "secret");
        let tampered = format!("session123.{}", "invalid_sig");
        let (_, valid) = validate_session_signature(&tampered, "secret");
        assert!(!valid);
    }

    #[test]
    fn test_validate_no_dot() {
        let (_, valid) = validate_session_signature("noseparator", "secret");
        assert!(!valid);
    }

    #[test]
    #[should_panic]
    fn test_sign_empty_secret_panics() {
        sign_session_id("session123", "");
    }

    #[test]
    fn test_generate_session_id_length() {
        let id = generate_session_id();
        assert_eq!(id.len(), 64);
    }

    #[test]
    fn test_generate_session_id_uniqueness() {
        let id1 = generate_session_id();
        let id2 = generate_session_id();
        assert_ne!(id1, id2);
    }

    #[test]
    fn test_truncate_session_id() {
        assert_eq!(truncate_session_id("abcdef1234567890"), "abcdef12...");
        assert_eq!(truncate_session_id("short"), "short");
    }
}
