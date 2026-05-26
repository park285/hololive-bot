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
        crate::holo::handlers::get_alarms,
        crate::holo::handlers::delete_alarm,
        crate::holo::handlers::set_room_name,
        crate::holo::handlers::set_user_name,
        crate::holo::handlers::get_members,
        crate::holo::handlers::add_member,
        crate::holo::handlers::add_alias,
        crate::holo::handlers::remove_alias,
        crate::holo::handlers::set_graduation,
        crate::holo::handlers::update_channel,
        crate::holo::handlers::update_member_name,
        crate::holo::handlers::get_rooms,
        crate::holo::handlers::add_room,
        crate::holo::handlers::remove_room,
        crate::holo::handlers::set_acl,
        crate::holo::handlers::get_settings,
        crate::holo::handlers::update_settings,
        crate::holo::handlers::get_stats,
        crate::holo::handlers::get_channel_stats,
        crate::holo::handlers::get_youtube_community_shorts_ops,
        crate::holo::handlers::get_live_streams,
        crate::holo::handlers::get_upcoming_streams,
        crate::holo::handlers::get_milestones,
        crate::holo::handlers::get_near_milestones,
        crate::holo::handlers::get_milestone_stats,
        crate::holo::handlers::get_calendar,
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
        crate::error::ErrorResponse,
        crate::docker::Container,
        crate::docker::PortMapping,
        crate::status::AggregatedStatus,
        crate::status::ServiceStatus,
        crate::holo::types::StatusOnlyResponse,
        crate::holo::types::Alarm,
        crate::holo::types::AlarmsResponse,
        crate::holo::types::DeleteAlarmRequest,
        crate::holo::types::RoomNameUpdateRequest,
        crate::holo::types::UserNameUpdateRequest,
        crate::holo::types::Aliases,
        crate::holo::types::Member,
        crate::holo::types::MembersResponse,
        crate::holo::types::AddMemberRequest,
        crate::holo::types::AddAliasRequest,
        crate::holo::types::RemoveAliasRequest,
        crate::holo::types::SetGraduationRequest,
        crate::holo::types::UpdateChannelRequest,
        crate::holo::types::UpdateMemberNameRequest,
        crate::holo::types::RoomsResponse,
        crate::holo::types::AddRoomRequest,
        crate::holo::types::RemoveRoomRequest,
        crate::holo::types::SetAclRequest,
        crate::holo::types::SetAclResponse,
        crate::holo::types::Settings,
        crate::holo::types::SettingsResponse,
        crate::holo::types::StatsResponse,
        crate::holo::types::ChannelStat,
        crate::holo::types::ChannelStatsResponse,
        crate::holo::types::YouTubeCommunityShortsOpsOverview,
        crate::holo::types::YouTubeCommunityShortsOpsChannel,
        crate::holo::types::YouTubeCommunityShortsOpsResponse,
        crate::holo::types::Stream,
        crate::holo::types::StreamsResponse,
        crate::holo::types::Milestone,
        crate::holo::types::MilestonesResponse,
        crate::holo::types::NearMilestone,
        crate::holo::types::NearMilestonesResponse,
        crate::holo::types::MilestoneStats,
        crate::holo::types::MilestoneStatsResponse,
        crate::holo::types::CalendarMember,
        crate::holo::types::CalendarEntry,
        crate::holo::types::CalendarResponse,
    )),
    tags(
        (name = "auth", description = "Authentication endpoints"),
        (name = "docker", description = "Docker management endpoints"),
        (name = "status", description = "Status and monitoring endpoints"),
        (name = "holo", description = "Typed admin contract for holo dashboard endpoints"),
    )
)]
#[allow(missing_debug_implementations)]
pub struct ApiDoc;

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::BTreeSet;

    fn required_fields(json: &serde_json::Value, schema: &str) -> BTreeSet<String> {
        json["components"]["schemas"][schema]["required"]
            .as_array()
            .into_iter()
            .flatten()
            .filter_map(|value| value.as_str().map(str::to_string))
            .collect()
    }

    #[test]
    fn test_openapi_includes_holo_paths() {
        let json = serde_json::to_value(ApiDoc::openapi()).expect("openapi to json");
        assert!(json["paths"]["/admin/api/holo/alarms"].is_object());
        assert!(json["paths"]["/admin/api/holo/members"].is_object());
        assert!(json["paths"]["/admin/api/holo/stats/youtube/community-shorts"].is_object());
    }

    #[test]
    fn test_openapi_includes_api_error_schema() {
        let json = serde_json::to_value(ApiDoc::openapi()).expect("openapi to json");
        assert!(json["components"]["schemas"]["ErrorResponse"].is_object());
    }

    #[test]
    fn test_openapi_holo_query_documents_typed_error_responses() {
        let json = serde_json::to_value(ApiDoc::openapi()).expect("openapi to json");
        let responses = &json["paths"]["/admin/api/holo/members"]["get"]["responses"];
        assert!(
            responses["400"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );
        assert!(
            responses["401"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );
        assert!(
            responses["502"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );
    }

    #[test]
    fn test_openapi_holo_command_documents_typed_error_responses() {
        let json = serde_json::to_value(ApiDoc::openapi()).expect("openapi to json");
        let responses = &json["paths"]["/admin/api/holo/members"]["post"]["responses"];
        assert!(
            responses["400"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );
        assert!(
            responses["401"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );
        assert!(
            responses["502"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );

        let remove_room_responses = &json["paths"]["/admin/api/holo/rooms"]["delete"]["responses"];
        assert!(
            remove_room_responses["400"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );
        assert!(
            remove_room_responses["401"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );
        assert!(
            remove_room_responses["502"]["content"]["application/json"]["schema"]["$ref"]
                .as_str()
                .is_some_and(|value| value.ends_with("/ErrorResponse"))
        );
    }

    #[test]
    fn test_openapi_holo_response_schemas_keep_required_fields() {
        let json = serde_json::to_value(ApiDoc::openapi()).expect("openapi to json");
        assert_eq!(
            required_fields(&json, "Alarm"),
            BTreeSet::from([
                "channelId".to_string(),
                "memberName".to_string(),
                "roomId".to_string(),
                "roomName".to_string(),
                "userId".to_string(),
                "userName".to_string(),
            ])
        );
        assert_eq!(
            required_fields(&json, "AlarmsResponse"),
            BTreeSet::from(["alarms".to_string(), "status".to_string()])
        );
        assert_eq!(
            required_fields(&json, "Member"),
            BTreeSet::from([
                "aliases".to_string(),
                "channelId".to_string(),
                "id".to_string(),
                "isGraduated".to_string(),
                "name".to_string(),
            ])
        );
    }

    #[test]
    fn test_openapi_holo_command_schemas_keep_required_fields() {
        let json = serde_json::to_value(ApiDoc::openapi()).expect("openapi to json");
        assert_eq!(
            required_fields(&json, "UserNameUpdateRequest"),
            BTreeSet::from(["userId".to_string(), "userName".to_string()])
        );
        assert_eq!(
            required_fields(&json, "AddMemberRequest"),
            BTreeSet::from([
                "aliases".to_string(),
                "channelId".to_string(),
                "isGraduated".to_string(),
                "name".to_string(),
            ])
        );
        assert_eq!(
            required_fields(&json, "SetGraduationRequest"),
            BTreeSet::from(["isGraduated".to_string()])
        );
    }
}
