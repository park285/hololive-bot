use utoipa::OpenApi;

#[derive(OpenApi)]
#[openapi(
    paths(
        crate::handlers::auth::handle_login,
        crate::handlers::auth::handle_logout,
        crate::handlers::auth::handle_heartbeat,
        crate::handlers::docker::handle_docker_health,
        crate::handlers::docker::handle_docker_containers,
        crate::handlers::docker::handle_docker_restart,
        crate::handlers::docker::handle_docker_stop,
        crate::handlers::docker::handle_docker_start,
        crate::handlers::status::handle_aggregated_status,
    ),
    components(schemas(
        crate::handlers::auth::LoginRequest,
        crate::handlers::auth::LoginResponse,
        crate::handlers::auth::HeartbeatRequest,
        crate::handlers::auth::HeartbeatResponse,
    )),
    tags(
        (name = "auth", description = "Authentication endpoints"),
        (name = "docker", description = "Docker management endpoints"),
        (name = "status", description = "Status and monitoring endpoints"),
    )
)]
pub struct ApiDoc;
