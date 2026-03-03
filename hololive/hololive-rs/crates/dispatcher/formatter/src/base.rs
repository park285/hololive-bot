#[derive(Debug, Clone)]
pub struct ResponseFormatter {
    pub prefix: String,
}

impl ResponseFormatter {
    pub fn new(prefix: &str) -> Self {
        Self {
            prefix: prefix.trim().to_owned(),
        }
    }

    pub fn prefix(&self) -> &str {
        &self.prefix
    }

    pub fn format_error(&self, message: &str) -> String {
        self.decorate(&format!("오류: {message}"))
    }

    pub fn member_not_found(&self, member_name: &str) -> String {
        self.decorate(&format!("멤버를 찾을 수 없습니다: {member_name}"))
    }

    pub(crate) fn decorate(&self, body: &str) -> String {
        if self.prefix.is_empty() {
            body.to_owned()
        } else {
            format!("{} {}", self.prefix, body)
        }
    }
}
