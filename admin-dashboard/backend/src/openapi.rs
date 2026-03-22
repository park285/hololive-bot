#![allow(clippy::needless_for_each)]

use utoipa::OpenApi;

#[allow(clippy::needless_for_each)]
#[derive(OpenApi)]
#[openapi(
    paths(
        crate::handlers::auth::handle_login,
        crate::handlers::auth::handle_logout,
        crate::handlers::auth::handle_session_status,
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
        crate::handlers::auth::SessionStatusResponse,
        crate::handlers::auth::HeartbeatRequest,
        crate::handlers::auth::HeartbeatResponse,
        crate::handlers::docker::DockerHealthResponse,
        crate::handlers::docker::DockerContainerListResponse,
        crate::handlers::docker::DockerActionResponse,
        crate::docker::Container,
        crate::docker::PortMapping,
        crate::status::AggregatedStatus,
        crate::status::ServiceStatus,
    )),
    tags(
        (name = "auth", description = "Authentication endpoints"),
        (name = "docker", description = "Docker management endpoints"),
        (name = "status", description = "Status and monitoring endpoints"),
    )
)]
#[allow(missing_debug_implementations)]
pub struct ApiDoc;
