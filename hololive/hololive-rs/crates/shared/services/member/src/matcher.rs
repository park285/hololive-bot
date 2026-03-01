use shared_core::model::member::Member;

use crate::{MemberMatcher, normalize_key};

pub struct DefaultMemberMatcher {
    members: Vec<Member>,
}

impl DefaultMemberMatcher {
    pub fn new(members: Vec<Member>) -> Self {
        Self { members }
    }

    pub fn set_members(&mut self, members: Vec<Member>) {
        self.members = members;
    }

    fn score_member(member: &Member, query: &str) -> i32 {
        if query.is_empty() {
            return 0;
        }

        let normalized_query = normalize_key(query);
        let name = normalize_key(&member.name);

        if name == normalized_query {
            return 100;
        }

        if let Some(english_name) = &member.english_name {
            let normalized_english_name = normalize_key(english_name);
            if normalized_english_name == normalized_query {
                return 95;
            }
            if normalized_english_name.contains(&normalized_query)
                || normalized_query.contains(&normalized_english_name)
            {
                return 70;
            }
        }

        for alias in member.all_aliases() {
            let normalized_alias = normalize_key(&alias);
            if normalized_alias == normalized_query {
                return 90;
            }
            if normalized_alias.contains(&normalized_query)
                || normalized_query.contains(&normalized_alias)
            {
                return 65;
            }
        }

        if name.contains(&normalized_query) || normalized_query.contains(&name) {
            return 60;
        }

        0
    }
}

impl MemberMatcher for DefaultMemberMatcher {
    fn best_match(&self, query: &str) -> Option<Member> {
        self.find_candidates(query, 1).into_iter().next()
    }

    fn find_candidates(&self, query: &str, limit: usize) -> Vec<Member> {
        let max_items = limit.max(1);

        let mut scored: Vec<(i32, Member)> = self
            .members
            .iter()
            .filter_map(|member| {
                let score = Self::score_member(member, query);
                (score > 0).then_some((score, member.clone()))
            })
            .collect();

        scored.sort_by(|left, right| {
            right
                .0
                .cmp(&left.0)
                .then_with(|| left.1.name.cmp(&right.1.name))
        });

        scored
            .into_iter()
            .take(max_items)
            .map(|(_, member)| member)
            .collect()
    }
}
