use std::io::Write;

use admin_dashboard::openapi::ApiDoc;
use utoipa::OpenApi;

fn main() {
    let json = ApiDoc::openapi()
        .to_pretty_json()
        .expect("openapi json serialization failed");
    let mut stdout = std::io::stdout();
    stdout
        .write_all(json.as_bytes())
        .expect("failed to write openapi json");
    stdout.write_all(b"\n").expect("failed to write newline");
}
