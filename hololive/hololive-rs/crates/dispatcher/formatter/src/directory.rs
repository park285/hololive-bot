use super::ResponseFormatter;

#[derive(Debug, Clone)]
pub struct MemberDirectoryGroup {
    pub group_name: String,
    pub members: Vec<MemberDirectoryEntry>,
}

#[derive(Debug, Clone)]
pub struct MemberDirectoryEntry {
    pub primary_name: String,
    pub secondary_name: String,
}

pub trait DirectoryFormatting: Send + Sync {
    fn member_directory(&self, groups: &[MemberDirectoryGroup], total: usize) -> String;
}

impl DirectoryFormatting for ResponseFormatter {
    fn member_directory(&self, groups: &[MemberDirectoryGroup], total: usize) -> String {
        if groups.is_empty() {
            return self.decorate("표시할 멤버 목록이 없습니다.");
        }

        let mut lines = vec![format!("멤버 목록 (총 {total})")];

        for group in groups {
            lines.push(format!("\n[{}]", group.group_name));
            for member in &group.members {
                if member.secondary_name.trim().is_empty() {
                    lines.push(format!("- {}", member.primary_name));
                } else {
                    lines.push(format!(
                        "- {} ({})",
                        member.primary_name, member.secondary_name
                    ));
                }
            }
        }

        self.decorate(&lines.join("\n"))
    }
}
