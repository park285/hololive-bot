use std::sync::Arc;

use axum::Json;
use axum::extract::{Path, State};
use axum::http::StatusCode;
use reqwest::Method;

use crate::error::AppError;
use crate::state::AppState;

use super::helpers::send_typed;
use super::types::{
    AddAliasRequest, AddMemberRequest, AddRoomRequest, DeleteAlarmRequest, RemoveAliasRequest,
    RemoveRoomRequest, RoomNameUpdateRequest, SetAclRequest, SetAclResponse, SetGraduationRequest,
    Settings, StatusOnlyResponse, UpdateChannelRequest, UpdateMemberNameRequest,
    UserNameUpdateRequest,
};

#[utoipa::path(
    delete,
    path = "/admin/api/holo/alarms",
    operation_id = "holoDeleteAlarm",
    request_body = DeleteAlarmRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn delete_alarm(
    State(state): State<Arc<AppState>>,
    Json(body): Json<DeleteAlarmRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::DELETE, "/api/holo/alarms", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/names/room",
    operation_id = "holoSetRoomName",
    request_body = RoomNameUpdateRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn set_room_name(
    State(state): State<Arc<AppState>>,
    Json(body): Json<RoomNameUpdateRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/names/room", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/names/user",
    operation_id = "holoSetUserName",
    request_body = UserNameUpdateRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn set_user_name(
    State(state): State<Arc<AppState>>,
    Json(body): Json<UserNameUpdateRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/names/user", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/members",
    operation_id = "holoAddMember",
    request_body = AddMemberRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn add_member(
    State(state): State<Arc<AppState>>,
    Json(body): Json<AddMemberRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/members", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/members/{id}/aliases",
    operation_id = "holoAddAlias",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = AddAliasRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn add_alias(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<AddAliasRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::POST,
        &format!("/api/holo/members/{id}/aliases"),
        &body,
    )
    .await
}

#[utoipa::path(
    delete,
    path = "/admin/api/holo/members/{id}/aliases",
    operation_id = "holoRemoveAlias",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = RemoveAliasRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn remove_alias(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<RemoveAliasRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::DELETE,
        &format!("/api/holo/members/{id}/aliases"),
        &body,
    )
    .await
}

#[utoipa::path(
    patch,
    path = "/admin/api/holo/members/{id}/graduation",
    operation_id = "holoSetGraduation",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = SetGraduationRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn set_graduation(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<SetGraduationRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::PATCH,
        &format!("/api/holo/members/{id}/graduation"),
        &body,
    )
    .await
}

#[utoipa::path(
    patch,
    path = "/admin/api/holo/members/{id}/channel",
    operation_id = "holoUpdateChannel",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = UpdateChannelRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn update_channel(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<UpdateChannelRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::PATCH,
        &format!("/api/holo/members/{id}/channel"),
        &body,
    )
    .await
}

#[utoipa::path(
    patch,
    path = "/admin/api/holo/members/{id}/name",
    operation_id = "holoUpdateMemberName",
    params(("id" = i64, Path, description = "Member ID")),
    request_body = UpdateMemberNameRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn update_member_name(
    State(state): State<Arc<AppState>>,
    Path(id): Path<i64>,
    Json(body): Json<UpdateMemberNameRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(
        &state,
        Method::PATCH,
        &format!("/api/holo/members/{id}/name"),
        &body,
    )
    .await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/rooms",
    operation_id = "holoAddRoom",
    request_body = AddRoomRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn add_room(
    State(state): State<Arc<AppState>>,
    Json(body): Json<AddRoomRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/rooms", &body).await
}

#[utoipa::path(
    delete,
    path = "/admin/api/holo/rooms",
    operation_id = "holoRemoveRoom",
    request_body = RemoveRoomRequest,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn remove_room(
    State(state): State<Arc<AppState>>,
    Json(body): Json<RemoveRoomRequest>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::DELETE, "/api/holo/rooms", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/rooms/acl",
    operation_id = "holoSetAcl",
    request_body = SetAclRequest,
    responses(
        (status = 200, body = SetAclResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn set_acl(
    State(state): State<Arc<AppState>>,
    Json(body): Json<SetAclRequest>,
) -> Result<(StatusCode, Json<SetAclResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/rooms/acl", &body).await
}

#[utoipa::path(
    post,
    path = "/admin/api/holo/settings",
    operation_id = "holoUpdateSettings",
    request_body = Settings,
    responses(
        (status = 200, body = StatusOnlyResponse),
        (status = 400, body = crate::error::ErrorResponse),
        (status = 401, body = crate::error::ErrorResponse),
        (status = 502, body = crate::error::ErrorResponse),
    ),
    tag = "holo"
)]
pub async fn update_settings(
    State(state): State<Arc<AppState>>,
    Json(body): Json<Settings>,
) -> Result<(StatusCode, Json<StatusOnlyResponse>), AppError> {
    send_typed(&state, Method::POST, "/api/holo/settings", &body).await
}
