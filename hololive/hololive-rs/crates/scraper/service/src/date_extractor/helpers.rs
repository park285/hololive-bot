use super::*;

pub(super) fn collect_simple_pattern_dates(
    text: &str,
    pattern: &Regex,
    dates: &mut BTreeSet<NaiveDate>,
) {
    for captures in pattern.captures_iter(text) {
        if let Some(date) = parse_capture_ymd(&captures, 1, 2, 3) {
            dates.insert(date);
        }
    }
}

pub(super) fn parse_capture_ymd(
    captures: &regex::Captures<'_>,
    year_idx: usize,
    month_idx: usize,
    day_idx: usize,
) -> Option<NaiveDate> {
    let year = parse_capture_year(captures, year_idx)?;
    let month = parse_capture_part(captures, month_idx)?;
    let day = parse_capture_part(captures, day_idx)?;

    NaiveDate::from_ymd_opt(year, month, day)
}

pub(super) fn parse_capture_year(captures: &regex::Captures<'_>, idx: usize) -> Option<i32> {
    captures.get(idx)?.as_str().parse::<i32>().ok()
}

pub(super) fn parse_capture_part(captures: &regex::Captures<'_>, idx: usize) -> Option<u32> {
    captures.get(idx)?.as_str().parse::<u32>().ok()
}

pub(super) fn make_dedupe_key(date: NaiveDate, position: usize) -> String {
    format!("{}@{}", date.format("%Y-%m-%d"), position)
}

pub(super) fn is_position_covered(pos: usize, ranges: &[(usize, usize)]) -> bool {
    ranges
        .iter()
        .any(|(start, end)| pos >= *start && pos < *end)
}

pub(super) fn extract_headers(html: &str) -> Vec<HeaderInfo> {
    SECTION_HEADER_PATTERN
        .captures_iter(html)
        .filter_map(|captures| captures.get(1).map(|inner| inner.as_str().to_owned()))
        .filter_map(|inner_html| {
            let text = HTML_TAG_PATTERN
                .replace_all(&inner_html, "")
                .trim()
                .to_owned();

            if text.is_empty() {
                None
            } else {
                Some(HeaderInfo { text })
            }
        })
        .collect()
}

pub(super) fn build_section_map(plain_text: &str, headers: &[HeaderInfo]) -> Vec<SectionRange> {
    struct MappedHeader {
        text: String,
        start_pos: usize,
        end_pos: usize,
    }

    let mut mapped_headers: Vec<MappedHeader> = Vec::with_capacity(headers.len());
    let mut last_index = 0usize;

    for header in headers {
        let Some(relative_index) = plain_text[last_index..].find(&header.text) else {
            continue;
        };

        let abs_pos = last_index + relative_index;
        let end_pos = abs_pos + header.text.len();

        mapped_headers.push(MappedHeader {
            text: header.text.clone(),
            start_pos: abs_pos,
            end_pos,
        });

        last_index = end_pos;
    }

    let mut sections = Vec::with_capacity(mapped_headers.len());
    for (index, mapped) in mapped_headers.iter().enumerate() {
        let end_pos = mapped_headers
            .get(index + 1)
            .map_or_else(|| plain_text.len(), |next| next.start_pos);

        sections.push(SectionRange {
            id: index,
            start_pos: mapped.end_pos,
            end_pos,
            header: mapped.text.clone(),
        });
    }

    sections
}

pub(super) fn find_section_for_position(
    sections: &[SectionRange],
    position: usize,
) -> Option<usize> {
    sections
        .iter()
        .find(|section| position >= section.start_pos && position < section.end_pos)
        .map(|section| section.id)
}

pub(super) fn section_header_bonus(header: &str) -> i32 {
    let lower = header.to_lowercase();

    if STRONG_POSITIVE_KEYWORD_MATCHER
        .find_iter(&lower)
        .next()
        .is_some()
    {
        return 5;
    }

    if NEGATIVE_KEYWORD_MATCHER.find_iter(&lower).next().is_some() {
        return -5;
    }

    0
}

pub(super) fn score_from_distances(
    pos_distance: Option<usize>,
    neg_distance: Option<usize>,
) -> i32 {
    let pos_distance = pos_distance.filter(|distance| *distance <= MAX_CONTEXT_DISTANCE);
    let neg_distance = neg_distance.filter(|distance| *distance <= MAX_CONTEXT_DISTANCE);

    match (pos_distance, neg_distance) {
        (None, None) => 0,
        (None, Some(_)) => -3,
        (Some(_), None) => 2,
        (Some(pos), Some(neg)) => {
            let diff = pos.abs_diff(neg);
            if diff <= TIE_THRESHOLD {
                return 0;
            }

            if pos < neg { 2 } else { -3 }
        }
    }
}

pub(super) fn filter_positive_matches(matches: &[DateMatch]) -> Vec<DateMatch> {
    matches
        .iter()
        .filter(|matched| matched.score > 0)
        .cloned()
        .collect()
}

pub(super) fn filter_non_negative_matches(matches: &[DateMatch]) -> Vec<DateMatch> {
    matches
        .iter()
        .filter(|matched| matched.score >= 0)
        .cloned()
        .collect()
}

pub(super) fn cluster_score(cluster: &[DateMatch]) -> i32 {
    cluster.iter().map(|matched| matched.score).sum()
}

pub(super) fn extract_unique_dates_from_matches(matches: &[DateMatch]) -> Vec<NaiveDate> {
    let mut seen = HashSet::new();
    let mut dates = Vec::with_capacity(matches.len());

    for matched in matches {
        let key = matched.date.format("%Y-%m-%d").to_string();
        if seen.insert(key) {
            dates.push(matched.date);
        }
    }

    dates
}
