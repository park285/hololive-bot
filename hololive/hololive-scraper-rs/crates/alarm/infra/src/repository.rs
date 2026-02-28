use alarm_core::error::AlarmError;
use async_trait::async_trait;
use sea_orm::{ColumnTrait, Database, DatabaseConnection, EntityTrait, QueryFilter, QueryOrder};
use std::time::Duration;

use crate::config::DatabaseConfig;

mod notification_template {
    use sea_orm::entity::prelude::*;

    #[derive(Clone, Debug, PartialEq, DeriveEntityModel)]
    #[sea_orm(table_name = "notification_templates")]
    pub struct Model {
        #[sea_orm(primary_key, auto_increment = false)]
        pub command: String,
        pub template: String,
    }

    #[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
    pub enum Relation {}

    impl ActiveModelBehavior for ActiveModel {}
}

mod holo_member {
    use sea_orm::entity::prelude::*;

    #[derive(Clone, Debug, PartialEq, DeriveEntityModel)]
    #[sea_orm(table_name = "holo_members")]
    pub struct Model {
        #[sea_orm(primary_key, auto_increment = false)]
        pub channel_id: String,
        pub name: String,
    }

    #[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
    pub enum Relation {}

    impl ActiveModelBehavior for ActiveModel {}
}

// ─────────────────────────────────────────────────────────────────────────────
// 도메인 모델
// ─────────────────────────────────────────────────────────────────────────────

/// DB에서 로드한 알림 템플릿 (메시지 포맷팅에 사용)
#[derive(Debug, Clone)]
pub struct NotificationTemplate {
    pub command: String,
    pub template: String,
}

// ─────────────────────────────────────────────────────────────────────────────
// Repository 인터페이스
// ─────────────────────────────────────────────────────────────────────────────

/// 알람 서비스 DB 접근 인터페이스
#[async_trait]
pub trait AlarmRepository: Send + Sync {
    /// 알림 템플릿 전체 로드 (기동 시 1회 사용)
    async fn get_notification_templates(&self) -> Result<Vec<NotificationTemplate>, AlarmError>;

    /// 채널 ID로 멤버 이름 조회 (캐시 미스 폴백용)
    async fn get_member_name(&self, channel_id: &str) -> Result<Option<String>, AlarmError>;
}

// ─────────────────────────────────────────────────────────────────────────────
// PostgreSQL 구현
// ─────────────────────────────────────────────────────────────────────────────

/// SeaORM 연결 기반 알람 Repository 구현체
#[derive(Clone)]
pub struct PgAlarmRepository {
    db: DatabaseConnection,
}

/// DB 연결 생성 헬퍼 — DatabaseConfig에서 연결 풀 초기화
pub async fn create_pool(config: &DatabaseConfig) -> Result<DatabaseConnection, AlarmError> {
    let mut options = sea_orm::ConnectOptions::new(config.database_url());
    options
        .max_connections(config.max_connections)
        .min_connections(0)
        .idle_timeout(Duration::from_secs(300))
        .connect_timeout(Duration::from_secs(30));

    Database::connect(options)
        .await
        .map_err(|e| AlarmError::Database(format!("DB 연결 실패: {e}")))
}

impl PgAlarmRepository {
    pub fn new(db: DatabaseConnection) -> Self {
        Self { db }
    }

    pub fn connection(&self) -> &DatabaseConnection {
        &self.db
    }

    /// DB 연결 상태 확인
    pub async fn health_check(&self) -> Result<(), AlarmError> {
        self.db
            .ping()
            .await
            .map_err(|e| AlarmError::Database(format!("헬스체크 실패: {e}")))?;
        Ok(())
    }
}

#[async_trait]
impl AlarmRepository for PgAlarmRepository {
    async fn get_notification_templates(&self) -> Result<Vec<NotificationTemplate>, AlarmError> {
        let rows = notification_template::Entity::find()
            .filter(notification_template::Column::Template.is_not_null())
            .order_by_asc(notification_template::Column::Command)
            .all(&self.db)
            .await
            .map_err(|e| AlarmError::Database(format!("알림 템플릿 조회 실패: {e}")))?;

        Ok(rows
            .into_iter()
            .map(|r| NotificationTemplate {
                command: r.command,
                template: r.template,
            })
            .collect())
    }

    async fn get_member_name(&self, channel_id: &str) -> Result<Option<String>, AlarmError> {
        let row = holo_member::Entity::find()
            .filter(holo_member::Column::ChannelId.eq(channel_id))
            .one(&self.db)
            .await
            .map_err(|e| {
                AlarmError::Database(format!(
                    "멤버 이름 조회 실패 (channel_id={channel_id}): {e}"
                ))
            })?;

        Ok(row.map(|r| r.name))
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Mock 구현 (테스트용)
// ─────────────────────────────────────────────────────────────────────────────

/// 테스트용 인메모리 알람 Repository Mock
pub struct MockAlarmRepository {
    templates: Vec<NotificationTemplate>,
    /// channel_id → name
    member_names: std::collections::HashMap<String, String>,
}

impl MockAlarmRepository {
    pub fn new(
        templates: Vec<NotificationTemplate>,
        member_names: std::collections::HashMap<String, String>,
    ) -> Self {
        Self {
            templates,
            member_names,
        }
    }
}

#[async_trait]
impl AlarmRepository for MockAlarmRepository {
    async fn get_notification_templates(&self) -> Result<Vec<NotificationTemplate>, AlarmError> {
        Ok(self.templates.clone())
    }

    async fn get_member_name(&self, channel_id: &str) -> Result<Option<String>, AlarmError> {
        Ok(self.member_names.get(channel_id).cloned())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    fn make_templates() -> Vec<NotificationTemplate> {
        vec![
            NotificationTemplate {
                command: "live".into(),
                template: "{name}님이 방송을 시작했습니다!".into(),
            },
            NotificationTemplate {
                command: "upcoming_5".into(),
                template: "{name}님의 방송이 5분 후 시작됩니다.".into(),
            },
        ]
    }

    #[tokio::test]
    async fn mock_returns_templates() {
        let repo = MockAlarmRepository::new(make_templates(), HashMap::new());

        let templates = repo.get_notification_templates().await.unwrap();
        assert_eq!(templates.len(), 2);
        assert_eq!(templates[0].command, "live");
    }

    #[tokio::test]
    async fn mock_returns_member_name() {
        let mut names = HashMap::new();
        names.insert("UCtest".into(), "아이나 아루마".into());

        let repo = MockAlarmRepository::new(vec![], names);

        let name = repo.get_member_name("UCtest").await.unwrap();
        assert_eq!(name, Some("아이나 아루마".into()));
    }

    #[tokio::test]
    async fn mock_returns_none_for_unknown_channel() {
        let repo = MockAlarmRepository::new(vec![], HashMap::new());

        let name = repo.get_member_name("UC_unknown").await.unwrap();
        assert!(name.is_none());
    }

    #[tokio::test]
    async fn mock_empty_templates() {
        let repo = MockAlarmRepository::new(vec![], HashMap::new());

        let templates = repo.get_notification_templates().await.unwrap();
        assert!(templates.is_empty());
    }
}
