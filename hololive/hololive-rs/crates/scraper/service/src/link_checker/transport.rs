use std::time::Duration;

use crate::repository::BoxFuture;

pub(super) trait LinkHttpClient: Send + Sync {
    fn probe(
        &self,
        method: reqwest::Method,
        url: &str,
        timeout: Duration,
    ) -> BoxFuture<'_, Result<HttpProbeResponse, String>>;
}

#[derive(Clone)]
pub(super) struct ReqwestLinkHttpClient {
    client: reqwest::Client,
}

#[derive(Debug, Clone)]
pub(super) struct HttpProbeResponse {
    pub(super) status: reqwest::StatusCode,
    pub(super) response_url: String,
    pub(super) redirect_location: Option<String>,
}

impl ReqwestLinkHttpClient {
    pub(super) fn new(client: reqwest::Client) -> Self {
        Self { client }
    }
}

impl LinkHttpClient for ReqwestLinkHttpClient {
    fn probe(
        &self,
        method: reqwest::Method,
        url: &str,
        timeout: Duration,
    ) -> BoxFuture<'_, Result<HttpProbeResponse, String>> {
        let client = self.client.clone();
        let target_url = url.to_owned();

        Box::pin(async move {
            let mut request = client.request(method.clone(), &target_url).timeout(timeout);

            if method == reqwest::Method::GET {
                request = request.header(reqwest::header::RANGE, "bytes=0-0");
            }

            let response = request.send().await.map_err(|err| err.to_string())?;
            let status = response.status();
            let response_url = response.url().to_string();
            let redirect_location = response
                .headers()
                .get(reqwest::header::LOCATION)
                .and_then(|value| value.to_str().ok())
                .map(ToOwned::to_owned);

            Ok(HttpProbeResponse {
                status,
                response_url,
                redirect_location,
            })
        })
    }
}
