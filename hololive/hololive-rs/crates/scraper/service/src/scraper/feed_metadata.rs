use std::{collections::HashMap, sync::Arc};

use parking_lot::RwLock;

#[derive(Debug, Clone, Default)]
pub(super) struct FeedMetadata {
    e_tag: String,
    last_modified: String,
}

#[derive(Clone, Default)]
pub(super) struct FeedMetadataStore {
    feed_metadata_by_page_url: Arc<RwLock<HashMap<String, FeedMetadata>>>,
}

impl FeedMetadataStore {
    pub(super) fn apply_conditional_request_headers(
        &self,
        mut request: reqwest::RequestBuilder,
        page_url: &str,
    ) -> reqwest::RequestBuilder {
        let Some(metadata) = self.get_feed_metadata(page_url) else {
            return request;
        };

        if !metadata.e_tag.is_empty() {
            request = request.header(reqwest::header::IF_NONE_MATCH, metadata.e_tag);
        }
        if !metadata.last_modified.is_empty() {
            request = request.header(reqwest::header::IF_MODIFIED_SINCE, metadata.last_modified);
        }

        request
    }

    pub(super) fn save_feed_metadata(
        &self,
        page_url: &str,
        e_tag: Option<&reqwest::header::HeaderValue>,
        last_modified: Option<&reqwest::header::HeaderValue>,
    ) {
        let normalize_header = |value: Option<&reqwest::header::HeaderValue>| -> String {
            value
                .and_then(|header| header.to_str().ok())
                .unwrap_or_default()
                .trim()
                .to_owned()
        };

        let normalized_e_tag = normalize_header(e_tag);
        let normalized_last_modified = normalize_header(last_modified);

        if normalized_e_tag.is_empty() && normalized_last_modified.is_empty() {
            return;
        }

        let mut guard = self.feed_metadata_by_page_url.write();
        let entry = guard.entry(page_url.to_owned()).or_default();

        if !normalized_e_tag.is_empty() {
            entry.e_tag = normalized_e_tag;
        }
        if !normalized_last_modified.is_empty() {
            entry.last_modified = normalized_last_modified;
        }
    }

    fn get_feed_metadata(&self, page_url: &str) -> Option<FeedMetadata> {
        let guard = self.feed_metadata_by_page_url.read();
        guard.get(page_url).cloned()
    }
}
