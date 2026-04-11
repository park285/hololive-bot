use admin_dashboard::openapi::ApiDoc;
use utoipa::OpenApi;

#[test]
fn openapi_document_serializes() {
    let json = serde_json::to_string(&ApiDoc::openapi()).expect("serialize openapi");
    assert!(json.contains("/admin/api/holo/members"));
    assert!(json.contains("/admin/api/holo/alarms"));
}
