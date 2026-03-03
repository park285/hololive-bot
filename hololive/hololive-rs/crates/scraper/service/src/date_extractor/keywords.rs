use std::sync::LazyLock;

use aho_corasick::{AhoCorasick, AhoCorasickBuilder};

const POSITIVE_KEYWORDS: &[&str] = &[
    "開催",
    "日時",
    "日程",
    "公演",
    "開演",
    "会期",
    "期間",
    "ライブ",
    "コンサート",
    "開場",
    "ステージ",
];

const NEGATIVE_KEYWORDS: &[&str] = &[
    "チケット",
    "先行",
    "抽選",
    "受付",
    "販売",
    "発売",
    "申込",
    "締切",
    "アーカイブ",
    "予約",
    "募集",
    "応募",
    "視聴期限",
    "購入",
    "配信期間",
];

const STRONG_POSITIVE_KEYWORDS: &[&str] = &["開催日時", "開催日程", "公演日時"];

pub(super) static POSITIVE_KEYWORD_MATCHER: LazyLock<AhoCorasick> = LazyLock::new(|| {
    AhoCorasickBuilder::new()
        .ascii_case_insensitive(true)
        .build(POSITIVE_KEYWORDS)
        .expect("valid positive keyword matcher")
});

pub(super) static NEGATIVE_KEYWORD_MATCHER: LazyLock<AhoCorasick> = LazyLock::new(|| {
    AhoCorasickBuilder::new()
        .ascii_case_insensitive(true)
        .build(NEGATIVE_KEYWORDS)
        .expect("valid negative keyword matcher")
});

pub(super) static STRONG_POSITIVE_KEYWORD_MATCHER: LazyLock<AhoCorasick> = LazyLock::new(|| {
    AhoCorasickBuilder::new()
        .ascii_case_insensitive(true)
        .build(STRONG_POSITIVE_KEYWORDS)
        .expect("valid strong positive keyword matcher")
});

