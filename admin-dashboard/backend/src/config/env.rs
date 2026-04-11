use std::env;

use anyhow::{Result, anyhow};

pub fn env_string(key: &str, default: &str) -> String {
    env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| default.to_string())
}

pub fn env_bool(key: &str, default: bool) -> bool {
    match env::var(key) {
        Ok(value) => match value.trim().to_ascii_lowercase().as_str() {
            "1" | "true" | "yes" | "on" => true,
            "0" | "false" | "no" | "off" => false,
            _ => default,
        },
        Err(_) => default,
    }
}

#[allow(dead_code)]
pub fn env_i64(key: &str, default: i64) -> i64 {
    env::var(key)
        .ok()
        .and_then(|value| value.trim().parse::<i64>().ok())
        .unwrap_or(default)
}

pub fn env_int(key: &str, default: usize) -> usize {
    env::var(key)
        .ok()
        .and_then(|value| value.trim().parse::<usize>().ok())
        .unwrap_or(default)
}

pub fn required_alias(keys: &[&str]) -> Result<String> {
    keys.iter()
        .find_map(|key| {
            env::var(key)
                .ok()
                .map(|value| value.trim().to_string())
                .filter(|value| !value.is_empty())
        })
        .ok_or_else(|| {
            anyhow!(
                "required environment variable missing: {}",
                keys.join(" or ")
            )
        })
}

pub fn optional_alias(keys: &[&str]) -> Option<String> {
    keys.iter().find_map(|key| {
        env::var(key)
            .ok()
            .map(|value| value.trim().to_string())
            .filter(|value| !value.is_empty())
    })
}

#[cfg(test)]
pub(crate) mod test_support {
    use std::env;
    use std::sync::{Mutex, OnceLock};

    fn env_lock() -> &'static Mutex<()> {
        static ENV_LOCK: OnceLock<Mutex<()>> = OnceLock::new();
        ENV_LOCK.get_or_init(|| Mutex::new(()))
    }

    #[allow(unsafe_code)]
    pub(crate) fn with_env_vars<R>(vars: &[(&str, Option<&str>)], test: impl FnOnce() -> R) -> R {
        let _guard = env_lock().lock().expect("env lock");
        let snapshot = vars
            .iter()
            .map(|(key, _)| ((*key).to_string(), env::var(key).ok()))
            .collect::<Vec<_>>();

        for (key, value) in vars {
            match value {
                Some(value) => unsafe { env::set_var(key, value) },
                None => unsafe { env::remove_var(key) },
            }
        }

        let result = test();

        for (key, value) in snapshot {
            match value {
                Some(value) => unsafe { env::set_var(&key, value) },
                None => unsafe { env::remove_var(&key) },
            }
        }

        result
    }
}
