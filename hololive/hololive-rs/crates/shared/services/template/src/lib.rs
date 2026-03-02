use std::{
    collections::HashMap,
    sync::{LazyLock, RwLock},
};

use regex::Regex;
use sea_orm::{ColumnTrait, DatabaseConnection, EntityTrait, QueryFilter};
use shared_core::error::SharedError;
use tera::{Context, Tera};

mod notification_template_entity {
    use sea_orm::entity::prelude::*;

    #[derive(Clone, Debug, PartialEq, DeriveEntityModel)]
    #[sea_orm(table_name = "notification_templates")]
    pub struct Model {
        #[sea_orm(primary_key)]
        pub id: i64,
        pub template_key: String,
        pub channel_id: Option<String>,
        pub body: String,
        pub created_at: DateTimeWithTimeZone,
        pub updated_at: DateTimeWithTimeZone,
    }

    #[derive(Copy, Clone, Debug, EnumIter, DeriveRelation)]
    pub enum Relation {}

    impl ActiveModelBehavior for ActiveModel {}
}

pub struct Renderer {
    tera: RwLock<Tera>,
    cache: RwLock<HashMap<String, String>>,
}

impl Renderer {
    pub fn new() -> Self {
        Self {
            tera: RwLock::new(Tera::default()),
            cache: RwLock::new(HashMap::new()),
        }
    }

    pub fn insert_template(&self, template_key: &str, body: &str) -> Result<(), SharedError> {
        self.insert_template_body(template_key, body)?;

        let mut tera = self
            .tera
            .write()
            .map_err(|_| SharedError::Config("tera lock poisoned".to_owned()))?;
        tera.add_raw_template(template_key, body)
            .map_err(|error| SharedError::Config(format!("parse template: {error}")))?;

        Ok(())
    }

    pub fn insert_template_body(&self, template_key: &str, body: &str) -> Result<(), SharedError> {
        let mut cache = self
            .cache
            .write()
            .map_err(|_| SharedError::Config("template cache lock poisoned".to_owned()))?;
        cache.insert(template_key.to_owned(), body.to_owned());

        Ok(())
    }

    pub fn render(&self, template_key: &str, context: &Context) -> Result<String, SharedError> {
        let body = self.template_body(template_key)?;

        let mut tera = self
            .tera
            .write()
            .map_err(|_| SharedError::Config("tera lock poisoned".to_owned()))?;

        if tera.get_template(template_key).is_err() {
            tera.add_raw_template(template_key, &body)
                .map_err(|error| SharedError::Config(format!("parse template: {error}")))?;
        }

        tera.render(template_key, context)
            .map_err(|error| SharedError::Config(format!("render template: {error}")))
    }

    pub fn render_go(
        &self,
        template_key: &str,
        context: &HashMap<String, serde_json::Value>,
    ) -> Result<String, SharedError> {
        let body = self.template_body(template_key)?;
        match try_render_go_template(&body, context) {
            Ok(rendered) => Ok(rendered),
            Err(go_error) => {
                let tera_template = convert_go_template_to_tera(&body);
                let tera_context = Context::from_serialize(context)
                    .map_err(|error| SharedError::Config(format!("build tera context: {error}")))?;
                Tera::one_off(&tera_template, &tera_context, false).map_err(|tera_error| {
                    SharedError::Config(format!(
                        "render go template fallback failed (go={go_error}, tera={tera_error})"
                    ))
                })
            }
        }
    }

    pub fn invalidate_cache(&self) {
        if let Ok(mut cache) = self.cache.write() {
            cache.clear();
        }

        if let Ok(mut tera) = self.tera.write() {
            *tera = Tera::default();
        }
    }

    pub async fn fetch_template_body(
        db: &DatabaseConnection,
        template_key: &str,
        channel_id: Option<&str>,
    ) -> Result<Option<String>, SharedError> {
        let template_key = template_key.trim();
        if template_key.is_empty() {
            return Ok(None);
        }

        if let Some(channel_id) = channel_id.and_then(normalize_channel_id) {
            let override_model = notification_template_entity::Entity::find()
                .filter(notification_template_entity::Column::TemplateKey.eq(template_key))
                .filter(notification_template_entity::Column::ChannelId.eq(channel_id))
                .one(db)
                .await
                .map_err(|error| {
                    SharedError::Database(format!(
                        "fetch notification template override: template_key={template_key}, channel_id={channel_id}: {error}"
                    ))
                })?;

            if let Some(model) = override_model {
                return Ok(Some(model.body));
            }
        }

        let default_model = notification_template_entity::Entity::find()
            .filter(notification_template_entity::Column::TemplateKey.eq(template_key))
            .filter(notification_template_entity::Column::ChannelId.is_null())
            .one(db)
            .await
            .map_err(|error| {
                SharedError::Database(format!(
                    "fetch notification template default: template_key={template_key}: {error}"
                ))
            })?;

        Ok(default_model.map(|model| model.body))
    }

    fn template_body(&self, template_key: &str) -> Result<String, SharedError> {
        let body = {
            let cache = self
                .cache
                .read()
                .map_err(|_| SharedError::Config("template cache lock poisoned".to_owned()))?;
            cache.get(template_key).cloned()
        };

        let Some(body) = body else {
            return Err(SharedError::NotFound(format!(
                "template not found: {template_key}"
            )));
        };

        Ok(body)
    }
}

fn try_render_go_template(
    body: &str,
    context: &HashMap<String, serde_json::Value>,
) -> Result<String, SharedError> {
    if !body.is_ascii() {
        return Err(SharedError::Config(
            "skip gtmpl renderer for non-ascii template".to_owned(),
        ));
    }

    let go_value = json_object_to_go_value(context);
    let rendered = std::panic::catch_unwind(|| {
        gtmpl_ng::template(body, go_value).map_err(|error| error.to_string())
    })
    .map_err(|_| {
        SharedError::Config("render go template panic (likely utf8 lexer boundary)".to_owned())
    })?;

    rendered.map_err(SharedError::Config)
}

fn json_object_to_go_value(context: &HashMap<String, serde_json::Value>) -> gtmpl_ng::Value {
    let mapped = context
        .iter()
        .map(|(key, value)| (key.clone(), json_value_to_go_value(value)))
        .collect::<HashMap<String, gtmpl_ng::Value>>();
    mapped.into()
}

fn json_value_to_go_value(value: &serde_json::Value) -> gtmpl_ng::Value {
    match value {
        serde_json::Value::Null => String::new().into(),
        serde_json::Value::Bool(v) => (*v).into(),
        serde_json::Value::Number(v) => {
            if let Some(num) = v.as_i64() {
                num.into()
            } else if let Some(num) = v.as_u64() {
                num.into()
            } else if let Some(num) = v.as_f64() {
                num.into()
            } else {
                String::new().into()
            }
        }
        serde_json::Value::String(v) => v.clone().into(),
        serde_json::Value::Array(items) => items
            .iter()
            .map(json_value_to_go_value)
            .collect::<Vec<gtmpl_ng::Value>>()
            .into(),
        serde_json::Value::Object(map) => map
            .iter()
            .map(|(key, value)| (key.clone(), json_value_to_go_value(value)))
            .collect::<HashMap<String, gtmpl_ng::Value>>()
            .into(),
    }
}

static GO_TPL_IF: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"\{\{\s*-?\s*if\s+\.([A-Za-z0-9_.]+)\s*-?\s*\}\}")
        .unwrap_or_else(|e| unreachable!("static regex: {e}"))
});
static GO_TPL_ELSE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"\{\{\s*-?\s*else\s*-?\s*\}\}")
        .unwrap_or_else(|e| unreachable!("static regex: {e}"))
});
static GO_TPL_END: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"\{\{\s*-?\s*end\s*-?\s*\}\}").unwrap_or_else(|e| unreachable!("static regex: {e}"))
});
static GO_TPL_VAR: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"\{\{\s*-?\s*\.([A-Za-z0-9_.]+)\s*-?\s*\}\}")
        .unwrap_or_else(|e| unreachable!("static regex: {e}"))
});

fn convert_go_template_to_tera(body: &str) -> String {
    let step1 = GO_TPL_IF.replace_all(body, "{% if $1 %}");
    let step2 = GO_TPL_ELSE.replace_all(&step1, "{% else %}");
    let step3 = GO_TPL_END.replace_all(&step2, "{% endif %}");
    GO_TPL_VAR.replace_all(&step3, "{{ $1 }}").to_string()
}

impl Default for Renderer {
    fn default() -> Self {
        Self::new()
    }
}

fn normalize_channel_id(channel_id: &str) -> Option<&str> {
    let normalized = channel_id.trim();
    if normalized.is_empty() {
        None
    } else {
        Some(normalized)
    }
}
