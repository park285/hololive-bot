use std::time::Duration;

use chrono::{DateTime, Utc};
use scraper_core::{
    error::ScraperError,
    model::{MajorEvent, MajorEventLinkStatus, MajorEventStatus, MajorEventType},
};
use sea_orm::{
    ColumnTrait, Condition, ConnectionTrait, Database, DatabaseBackend, DatabaseConnection,
    EntityTrait, QueryFilter, QueryOrder, QuerySelect, Statement,
};

use crate::config::DatabaseConfig;

mod major_event {
    use sea_orm::entity::prelude::*;

    #[derive(Clone, Debug, PartialEq, DeriveEntityModel)]
    #[sea_orm(table_name = "major_events")]
    pub struct Model {
        #[sea_orm(primary_key)]
        pub id: i32,
        pub external_id: String,
        #[sea_orm(column_name = "type")]
        pub event_type: String,
        pub title: String,
        pub link: String,
        pub description: Option<String>,
        pub members: Option<Vec<String>>,
        pub pub_date: Option<DateTimeUtc>,
        pub event_start_date: Option<Date>,
        pub event_end_date: Option<Date>,
        pub status: String,
        pub link_status: String,
        pub link_checked_at: Option<DateTimeUtc>,
        pub notified_at: Option<DateTimeUtc>,
        pub notified_week: Option<String>,
        pub notified_month: Option<String>,
        pub created_at: DateTimeUtc,
        pub updated_at: DateTimeUtc,
    }

    #[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
    pub enum Relation {}

    impl ActiveModelBehavior for ActiveModel {}
}

pub async fn create_pool(config: &DatabaseConfig) -> Result<DatabaseConnection, ScraperError> {
    let mut options = sea_orm::ConnectOptions::new(config.database_url());
    options
        .max_connections(config.max_connections)
        .min_connections(0)
        .idle_timeout(Duration::from_secs(300))
        .connect_timeout(Duration::from_secs(30));

    Database::connect(options).await.map_err(db_error)
}

#[derive(Clone)]
pub struct Repository {
    db: DatabaseConnection,
}

impl Repository {
    pub fn new(db: DatabaseConnection) -> Self {
        Self { db }
    }

    pub fn connection(&self) -> &DatabaseConnection {
        &self.db
    }

    pub async fn health_check(&self) -> Result<(), ScraperError> {
        self.db.ping().await.map_err(db_error)?;
        Ok(())
    }

    pub async fn upsert_event(&self, event: &MajorEvent) -> Result<i32, ScraperError> {
        let statement = Statement::from_sql_and_values(
            DatabaseBackend::Postgres,
            r#"
            INSERT INTO major_events (
                external_id, type, title, link, description, members, pub_date, event_start_date, event_end_date, status, link_status
            )
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
            ON CONFLICT (external_id) DO UPDATE
            SET
                title = EXCLUDED.title,
                link = EXCLUDED.link,
                description = EXCLUDED.description,
                members = EXCLUDED.members,
                pub_date = EXCLUDED.pub_date,
                event_start_date = EXCLUDED.event_start_date,
                event_end_date = EXCLUDED.event_end_date,
                type = EXCLUDED.type,
                status = CASE
                    WHEN major_events.status = 'canceled' THEN major_events.status
                    WHEN major_events.status = 'ended' AND EXCLUDED.event_start_date >= CURRENT_DATE THEN 'active'
                    ELSE major_events.status
                END,
                link_status = CASE
                    WHEN major_events.link IS DISTINCT FROM EXCLUDED.link THEN 'unchecked'
                    ELSE major_events.link_status
                END,
                link_checked_at = CASE
                    WHEN major_events.link IS DISTINCT FROM EXCLUDED.link THEN NULL
                    ELSE major_events.link_checked_at
                END,
                updated_at = NOW()
            RETURNING id
            "#,
            vec![
                event.external_id.clone().into(),
                event.event_type.as_str().to_owned().into(),
                event.title.clone().into(),
                event.link.clone().into(),
                event.description.clone().into(),
                event.members.clone().into(),
                event.pub_date.into(),
                event.event_start_date.into(),
                event.event_end_date.into(),
                event.status.as_str().to_owned().into(),
                event.link_status.as_str().to_owned().into(),
            ],
        );

        let row = self
            .db
            .query_one(statement)
            .await
            .map_err(db_error)?
            .ok_or_else(|| {
                ScraperError::Database("upsert major_events returned no row".to_owned())
            })?;

        row.try_get::<i32>("", "id")
            .map_err(|err| ScraperError::Database(err.to_string()))
    }

    pub async fn get_recent_external_ids(
        &self,
        event_type: MajorEventType,
        limit: i64,
    ) -> Result<RecentExternalIds, ScraperError> {
        let safe_limit = limit.max(1) as u64;
        let rows = major_event::Entity::find()
            .select_only()
            .column(major_event::Column::ExternalId)
            .column(major_event::Column::PubDate)
            .filter(major_event::Column::EventType.eq(event_type.as_str()))
            .order_by_desc(major_event::Column::PubDate)
            .order_by_desc(major_event::Column::UpdatedAt)
            .limit(safe_limit)
            .into_tuple::<(String, Option<DateTime<Utc>>)>()
            .all(&self.db)
            .await
            .map_err(db_error)?;

        let latest_pub_date = rows.iter().find_map(|(_, pub_date)| *pub_date);
        let external_ids = rows
            .into_iter()
            .map(|(external_id, _)| external_id)
            .filter(|id| !id.is_empty())
            .collect();

        Ok(RecentExternalIds {
            external_ids,
            latest_pub_date,
        })
    }

    pub async fn update_expired_events(&self) -> Result<u64, ScraperError> {
        let today = Utc::now().date_naive();
        let result = major_event::Entity::update_many()
            .col_expr(
                major_event::Column::Status,
                sea_orm::sea_query::Expr::value(MajorEventStatus::Ended.as_str()),
            )
            .col_expr(
                major_event::Column::UpdatedAt,
                sea_orm::sea_query::Expr::value(Utc::now()),
            )
            .filter(major_event::Column::Status.eq(MajorEventStatus::Active.as_str()))
            .filter(
                Condition::any()
                    .add(major_event::Column::EventEndDate.lt(today))
                    .add(
                        Condition::all()
                            .add(major_event::Column::EventEndDate.is_null())
                            .add(major_event::Column::EventStartDate.lt(today)),
                    ),
            )
            .exec(&self.db)
            .await
            .map_err(db_error)?;

        Ok(result.rows_affected)
    }

    pub async fn list_events_needing_link_check(
        &self,
        stale_before: DateTime<Utc>,
        limit: i64,
    ) -> Result<Vec<MajorEvent>, ScraperError> {
        let safe_limit = limit.max(1) as u64;
        let rows = major_event::Entity::find()
            .filter(major_event::Column::Status.eq(MajorEventStatus::Active.as_str()))
            .filter(major_event::Column::Link.ne(""))
            .filter(
                Condition::any()
                    .add(major_event::Column::LinkCheckedAt.is_null())
                    .add(major_event::Column::LinkCheckedAt.lt(stale_before)),
            )
            .order_by_asc(major_event::Column::LinkCheckedAt)
            .order_by_desc(major_event::Column::UpdatedAt)
            .limit(safe_limit)
            .all(&self.db)
            .await
            .map_err(db_error)?;

        Ok(rows.into_iter().map(model_to_major_event).collect())
    }

    pub async fn update_event_link_status(
        &self,
        event_id: i32,
        link_status: MajorEventLinkStatus,
        checked_at: DateTime<Utc>,
    ) -> Result<(), ScraperError> {
        major_event::Entity::update_many()
            .col_expr(
                major_event::Column::LinkStatus,
                sea_orm::sea_query::Expr::value(link_status.as_str()),
            )
            .col_expr(
                major_event::Column::LinkCheckedAt,
                sea_orm::sea_query::Expr::value(checked_at),
            )
            .col_expr(
                major_event::Column::UpdatedAt,
                sea_orm::sea_query::Expr::value(Utc::now()),
            )
            .filter(major_event::Column::Id.eq(event_id))
            .exec(&self.db)
            .await
            .map_err(db_error)?;

        Ok(())
    }
}

#[derive(Debug)]
pub struct RecentExternalIds {
    pub external_ids: Vec<String>,
    pub latest_pub_date: Option<DateTime<Utc>>,
}

fn model_to_major_event(model: major_event::Model) -> MajorEvent {
    MajorEvent {
        id: model.id,
        external_id: model.external_id,
        event_type: parse_event_type(&model.event_type),
        title: model.title,
        link: model.link,
        description: model.description,
        members: model.members.unwrap_or_default(),
        pub_date: model.pub_date,
        event_start_date: model.event_start_date,
        event_end_date: model.event_end_date,
        event_dates: Vec::new(),
        status: parse_event_status(&model.status),
        link_status: parse_link_status(&model.link_status),
        link_checked_at: model.link_checked_at,
        notified_at: model.notified_at,
        notified_week: model.notified_week,
        notified_month: model.notified_month,
        created_at: model.created_at,
        updated_at: model.updated_at,
    }
}

fn parse_event_type(raw: &str) -> MajorEventType {
    match raw {
        "news" => MajorEventType::News,
        _ => MajorEventType::Event,
    }
}

fn parse_event_status(raw: &str) -> MajorEventStatus {
    match raw {
        "ended" => MajorEventStatus::Ended,
        "canceled" => MajorEventStatus::Canceled,
        _ => MajorEventStatus::Active,
    }
}

fn parse_link_status(raw: &str) -> MajorEventLinkStatus {
    match raw {
        "ok" => MajorEventLinkStatus::Ok,
        "failed" => MajorEventLinkStatus::Failed,
        "blocked" => MajorEventLinkStatus::Blocked,
        _ => MajorEventLinkStatus::Unchecked,
    }
}

fn db_error(err: sea_orm::DbErr) -> ScraperError {
    ScraperError::Database(err.to_string())
}
