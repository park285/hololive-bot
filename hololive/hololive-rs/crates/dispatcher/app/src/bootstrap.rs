use anyhow::{Context, Result};
use sea_orm::DatabaseConnection;
use shared_template::Renderer;
use tracing::{info, warn};

use crate::render::{ALARM_LIVE_STARTED_TEMPLATE_KEY, ALARM_NOTIFICATION_TEMPLATE_KEY};

const DEFAULT_ALARM_NOTIFICATION_TEMPLATE: &str = "⏰ {{ .ChannelName }} 방송 예정
{{- if .ScheduledTimeKST}}
🔔 {{ .ScheduledTimeKST }}에 시작합니다
{{- else}}
🔔 곧 시작합니다!
{{- end}}
{{- if .ScheduleMessage}}
📅 {{ .ScheduleMessage }}
{{- end}}
📺 {{ .Title }}
🔗 {{ .URL }}";

const DEFAULT_ALARM_LIVE_STARTED_TEMPLATE: &str = "🔔 {{ .ChannelName }} 방송 시작됨
{{- if .ScheduledTimeKST}}
⏰ {{ .ScheduledTimeKST }}에 시작했습니다
{{- else}}
⏰ 방금 시작했습니다
{{- end}}
📺 {{ .Title }}
🔗 {{ .URL }}";

pub(crate) async fn build_renderer(db: Option<&DatabaseConnection>) -> Result<Renderer> {
    let renderer = Renderer::new();

    load_template(
        &renderer,
        db,
        ALARM_NOTIFICATION_TEMPLATE_KEY,
        DEFAULT_ALARM_NOTIFICATION_TEMPLATE,
    )
    .await?;

    load_template(
        &renderer,
        db,
        ALARM_LIVE_STARTED_TEMPLATE_KEY,
        DEFAULT_ALARM_LIVE_STARTED_TEMPLATE,
    )
    .await?;

    Ok(renderer)
}

async fn load_template(
    renderer: &Renderer,
    db: Option<&DatabaseConnection>,
    template_key: &str,
    fallback_template: &str,
) -> Result<()> {
    if let Some(db) = db {
        match Renderer::fetch_template_body(db, template_key, None).await {
            Ok(Some(body)) => {
                renderer
                    .insert_template_body(template_key, &body)
                    .with_context(|| format!("insert db template: {template_key}"))?;
                info!(template_key, "dispatcher template loaded from database");
                return Ok(());
            }
            Ok(None) => {
                warn!(
                    template_key,
                    "dispatcher template not found in database; fallback to default template"
                );
            }
            Err(error) => {
                warn!(
                    error = %error,
                    template_key,
                    "dispatcher template fetch failed; fallback to default template"
                );
            }
        }
    }

    renderer
        .insert_template_body(template_key, fallback_template)
        .with_context(|| format!("insert fallback template: {template_key}"))?;
    Ok(())
}
