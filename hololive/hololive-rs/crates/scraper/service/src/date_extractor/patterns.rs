use std::sync::LazyLock;

use regex::Regex;

pub(super) static JAPANESE_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})年(\d{1,2})月(\d{1,2})日").expect("valid regex"));

pub(super) static SLASH_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})[/\-](\d{1,2})[/\-](\d{1,2})").expect("valid regex"));

pub(super) static DOT_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{4})\.\s*(\d{1,2})\.(\d{1,2})").expect("valid regex"));

pub(super) static SHORT_JAPANESE_DATE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d{1,2})月(\d{1,2})日").expect("valid regex"));

pub(super) static MULTI_DAY_RANGE_PATTERN: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(\d{4})年(\d{1,2})月(\d{1,2})日(?:\([^)]*\)|（[^）]*）)?\s*[～〜~\-]\s*(?:(\d{1,2})月)?(\d{1,2})日",
    )
    .expect("valid regex")
});

pub(super) static HTML_TAG_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"<[^>]+>").expect("valid regex"));

pub(super) static SCRIPT_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?is)<script[^>]*>.*?</script>").expect("valid regex"));

pub(super) static STYLE_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?is)<style[^>]*>.*?</style>").expect("valid regex"));

pub(super) static SECTION_HEADER_PATTERN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(?is)<h[456][^>]*>(.*?)</h[456]>").expect("valid regex"));

