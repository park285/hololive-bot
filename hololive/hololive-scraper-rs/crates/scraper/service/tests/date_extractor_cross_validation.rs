use scraper_service::date_extractor::DateExtractor;
use serde::{Deserialize, Serialize};
use std::{
    error::Error,
    fs,
    path::{Path, PathBuf},
};

#[derive(Debug, Deserialize)]
struct FixtureFile {
    schema_version: u32,
    generated_from: String,
    cases: Vec<FixtureCase>,
}

#[derive(Debug, Deserialize)]
struct FixtureCase {
    name: String,
    input_html: Option<String>,
    input_file: Option<String>,
    expected_dates: Vec<String>,
}

#[derive(Debug, Serialize)]
struct CrossValidationOutput {
    fixture_path: String,
    results: Vec<CrossValidationResult>,
}

#[derive(Debug, Serialize)]
struct CrossValidationResult {
    name: String,
    dates: Vec<String>,
}

#[tokio::test]
async fn date_extractor_cross_validation_from_json_fixture() -> Result<(), Box<dyn Error>> {
    let fixture_path = resolve_fixture_path();
    let fixture_body = fs::read_to_string(&fixture_path)?;
    let fixture: FixtureFile = serde_json::from_str(&fixture_body)?;

    assert_eq!(
        fixture.schema_version, 1,
        "unsupported fixture schema_version"
    );
    assert!(
        !fixture.generated_from.trim().is_empty(),
        "fixture generated_from must not be empty"
    );

    let fixture_dir = fixture_path
        .parent()
        .ok_or("fixture path must have parent directory")?;
    let extractor = DateExtractor::new();
    let mut results = Vec::with_capacity(fixture.cases.len());

    for case in fixture.cases {
        assert!(
            !case.name.trim().is_empty(),
            "fixture case name must not be empty"
        );

        let html = resolve_case_html(&case, fixture_dir)?;
        let dates = extractor
            .extract_event_dates(&html)
            .into_iter()
            .map(|date| date.format("%Y-%m-%d").to_string())
            .collect::<Vec<_>>();

        assert_eq!(
            dates, case.expected_dates,
            "cross-validation mismatch for case: {}",
            case.name
        );

        results.push(CrossValidationResult {
            name: case.name,
            dates,
        });
    }

    if let Ok(output_path) = std::env::var("CROSS_VALIDATE_OUTPUT") {
        let output = CrossValidationOutput {
            fixture_path: fixture_path.display().to_string(),
            results,
        };

        let output_body = serde_json::to_string_pretty(&output)?;
        fs::write(output_path, output_body)?;
    }

    Ok(())
}

fn resolve_fixture_path() -> PathBuf {
    if let Ok(path) = std::env::var("CROSS_VALIDATE_FIXTURE") {
        let path = path.trim();
        if !path.is_empty() {
            return PathBuf::from(path);
        }
    }

    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("testdata")
        .join("date_extractor_cross_validation_cases.json")
}

fn resolve_case_html(case: &FixtureCase, fixture_dir: &Path) -> Result<String, Box<dyn Error>> {
    match (&case.input_html, &case.input_file) {
        (Some(html), None) => Ok(html.clone()),
        (None, Some(file_name)) => {
            let path = fixture_dir.join(file_name);
            let fixture_dir = fixture_dir.canonicalize()?;
            let resolved = path.canonicalize()?;
            if !resolved.starts_with(&fixture_dir) {
                return Err(format!(
                    "fixture case input_file escapes fixture directory: {}",
                    file_name
                )
                .into());
            }

            Ok(fs::read_to_string(resolved)?)
        }
        (Some(_), Some(_)) => {
            Err("fixture case must define exactly one of input_html or input_file".into())
        }
        (None, None) => Err("fixture case must define input_html or input_file".into()),
    }
}
