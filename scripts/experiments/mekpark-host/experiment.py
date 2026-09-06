# /// script
# requires-python = ">=3.14,<3.15"
# dependencies = ["scikit-learn==1.9.0"]
# ///
"""공개 제목과 DB 표본으로 방송자 모델을 학습하고 고정 holdout에서 비교한다."""

import argparse
from collections import Counter
from datetime import datetime
import hashlib
from importlib.metadata import version
import json
from pathlib import Path
import re
import subprocess
import sys
import unicodedata
import warnings

import joblib
import numpy as np
from sklearn.exceptions import ConvergenceWarning
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import confusion_matrix, f1_score
from sklearn.model_selection import StratifiedGroupKFold
from sklearn.pipeline import make_pipeline
from threadpoolctl import threadpool_limits

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
DOMAIN = ROOT / "hololive/hololive-shared/pkg/domain/mekparkhost"
CUTOFF = datetime.fromisoformat("2026-08-28T00:00:00+09:00")
SEED = 42
C_VALUES = (0.5, 2.0, 8.0)
THRESHOLDS = (0.55, 0.65, 0.75, 0.85, 0.95)
MIN_CV_ACCEPTED = 10
MIN_CV_PRECISION = 0.95


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def require(condition, message):
    if not condition:
        raise ValueError(message)


def index_by_id(cases):
    result = {item["video_id"]: item for item in cases}
    require(len(result) == len(cases), "duplicate video_id in source")
    return result


def normalize(title):
    title = unicodedata.normalize("NFKC", title).lower()
    title = re.sub("[\u200b\u200c\u200d\u2060\ufeff]", "", title)
    return re.sub(r"\s+", " ", re.sub(r"#\s+", "#", title)).strip()


def identity_terms(rules):
    terms = {"赤たん", "ゆいひな"}
    for member in rules["members"]:
        terms.update([member["full_name"], member["given_name"]])
        terms.update(member.get("tags", []))
        terms.update(member.get("series_tokens", []))
    return sorted({normalize(term) for term in terms}, key=lambda term: (-len(term), term))


def remove_identity(title, terms):
    text = normalize(title)
    for term in terms:
        text = text.replace(term, " ")
    text = re.sub(r"#[^\s#【】\[\]/|、。!?！？]+", " ", text)
    text = "".join(char for char in text if unicodedata.category(char) not in {"So", "Sk"} and not 0xFE00 <= ord(char) <= 0xFE0F)
    return re.sub(r"\s+", " ", text).strip()


def parse_timestamp(value):
    if not value:
        return None
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    return parsed if parsed.tzinfo is not None else None


def load_rows(rules, terms):
    db = index_by_id(read_json(DOMAIN / "testdata/title_corpus.json")["cases"])
    public_snapshot = read_json(HERE / "public-titles.json")
    public = index_by_id(public_snapshot["cases"])
    times = index_by_id(read_json(HERE / "event-times.json")["cases"])
    reviews = index_by_id(read_json(HERE / "review-labels.json")["cases"])
    require(public_snapshot["complete"], "public channel pagination is incomplete")
    channels = {unit: definition["channel_id"] for unit, definition in rules["units"].items()}
    people = {member["id"]: member["unit"] for member in rules["members"]}
    rows = []
    for video_id in sorted(db.keys() | public.keys()):
        original, current = db.get(video_id), public.get(video_id)
        selected = current or original
        require(selected["channel_id"] == channels[selected["unit"]], "channel is outside experiment scope")
        require(original is None or current is None or original["channel_id"] == current["channel_id"], "source channel mismatch")
        time = times.get(video_id, {})
        candidates = [
            ("youtube_start_timestamp", (current or {}).get("start_timestamp")),
            ("db_started_at", time.get("event_at") if time.get("time_source") == "started_at" else None),
            ("youtube_publish_date", (current or {}).get("publish_date")),
            ("db_" + time.get("time_source", "missing"), time.get("event_at")),
        ]
        event_at, time_source = None, "missing_exact_time"
        for source, value in candidates:
            if parsed := parse_timestamp(value):
                event_at, time_source = parsed, source
                break
        text = selected["title"]
        rows.append({
            "video_id": video_id, "unit": selected["unit"], "channel_id": selected["channel_id"],
            "title": text, "db_title": original["title"] if original else None,
            "sources": (["db"] if original else []) + (["public_youtube"] if current else []),
            "public_tabs": current["tabs"] if current else [],
            "event_at": event_at, "time_source": time_source,
            "group": selected["unit"] + ":" + re.sub(r"\W+", "", remove_identity(text, terms)),
        })

    # 실제 Go 판별기의 출력을 사용하며 Python에 룰을 다시 구현하지 않는다.
    command = ["go", "run", "./hololive/hololive-shared/pkg/domain/mekparkhost/cmd/classify-titles"]
    request = [{key: row[key] for key in ("video_id", "channel_id", "title")} for row in rows]
    baseline = subprocess.run(command, cwd=ROOT, input=json.dumps(request, ensure_ascii=False), text=True,
                              capture_output=True, check=True, timeout=120)
    classifications = index_by_id(json.loads(baseline.stdout))
    require(classifications.keys() == {row["video_id"] for row in rows}, "baseline output ID mismatch")
    for row in rows:
        video_id = row["video_id"]
        found = classifications[video_id]
        require(found["unit"] == row["unit"], "baseline unit mismatch")
        row["rule_hosts"] = found["hosts"]
        row["rule_guests"] = found["guests"]
        row["evaluation_only"] = video_id in reviews
        row["label"] = None
        if video_id in reviews:
            review = reviews[video_id]
            row["scope"] = review["scope"]
            row["label_basis"] = review["basis"]
            if review["scope"] == "single":
                row["label"] = review["host"]
        elif video_id in db and len(db[video_id]["hosts"]) == 1:
            row["label"] = db[video_id]["hosts"][0]
            row["scope"] = "single"
            row["label_basis"] = "existing_title_review_weak_label"
        elif len(found["hosts"]) == 1:
            row["label"] = found["hosts"][0]
            row["scope"] = "single"
            row["label_basis"] = "public_title_rule_weak_label"
        else:
            row["scope"] = "multiple" if len(found["hosts"]) > 1 else "unknown"
            row["label_basis"] = "no_single_host_label"
        if row["label"]:
            require(people.get(row["label"]) == row["unit"], "label is outside the channel unit")
    require(set(reviews) <= {row["video_id"] for row in rows}, "review has a missing video")
    return rows, {
        "db_videos": len(db), "public_videos": len(public), "public_new_videos": len(public.keys() - db.keys()),
        "db_only_videos": sorted(db.keys() - public.keys()), "merged_videos": len(rows),
        "public_observed_at": public_snapshot["observed_at"], "public_tabs": public_snapshot["tabs"],
        "missing_exact_times": [row["video_id"] for row in rows if row["event_at"] is None],
        "changed_db_title_ids": [row["video_id"] for row in rows if row["db_title"] is not None and row["db_title"] != row["title"]],
        "scopes": dict(sorted(Counter(row["scope"] for row in rows).items())),
        "weak_label_warning": "Title-derived and rule-derived labels are not independent video-verified ground truth.",
    }


def split_rows(rows):
    eligible = [row for row in rows if row["label"] and row["event_at"] and not row["evaluation_only"]]
    train = [row for row in eligible if row["event_at"] < CUTOFF]
    holdout = [row for row in eligible if row["event_at"] >= CUTOFF]
    review = [row for row in rows if row["evaluation_only"]]
    reserved_groups = {row["group"] for row in holdout + review}
    purged = [row["video_id"] for row in train if row["group"] in reserved_groups]
    train = [row for row in train if row["group"] not in reserved_groups]
    require(train and holdout, "empty temporal train or holdout split")
    require(not {row["video_id"] for row in train} & {row["video_id"] for row in holdout + review}, "video leakage")
    require(not {row["group"] for row in train} & reserved_groups, "normalized title leakage")
    return train, holdout, review, purged


def pipeline(c_value):
    return make_pipeline(
        TfidfVectorizer(analyzer="char", ngram_range=(2, 5), sublinear_tf=True, lowercase=False, max_features=12000),
        LogisticRegression(C=c_value, class_weight="balanced", max_iter=2000, random_state=SEED),
    )


def model_predictions(model, texts):
    features = model[0].transform(texts)
    probabilities = model[1].predict_proba(features)
    indices = probabilities.argmax(axis=1)
    return model.classes_[indices], probabilities.max(axis=1), np.asarray(features.getnnz(axis=1))


def choose_threshold(predicted, scores, nonzero, truth):
    curve = []
    threshold = None
    for value in THRESHOLDS:
        accepted = (scores >= value) & (nonzero > 0)
        count = int(accepted.sum())
        correct = int(((predicted == truth) & accepted).sum())
        precision = correct / count if count else None
        curve.append({"threshold": value, "accepted": count, "correct": correct, "precision": precision})
        if threshold is None and count >= MIN_CV_ACCEPTED and precision >= MIN_CV_PRECISION:
            threshold = value
    return threshold, curve


def fit_unit(rows, unit, transform):
    selected = [row for row in rows if row["unit"] == unit]
    texts = np.asarray([transform(row["title"]) for row in selected])
    truth = np.asarray([row["label"] for row in selected])
    groups = np.asarray([row["group"] for row in selected])
    labels = sorted(set(truth))
    require(len(labels) == 3, f"{unit} needs all three classes")
    folds = list(StratifiedGroupKFold(n_splits=3, shuffle=True, random_state=SEED).split(texts, truth, groups))
    for train_index, valid_index in folds:
        require(not set(groups[train_index]) & set(groups[valid_index]), "CV group leakage")
        require(set(truth[train_index]) == set(labels), "class missing from training fold")
    candidates = []
    for c_value in C_VALUES:
        predicted = np.empty(len(truth), dtype=object)
        scores, nonzero = np.zeros(len(truth)), np.zeros(len(truth), dtype=int)
        for train_index, valid_index in folds:
            model = pipeline(c_value).fit(texts[train_index], truth[train_index])
            prediction, score, nnz = model_predictions(model, texts[valid_index])
            predicted[valid_index], scores[valid_index], nonzero[valid_index] = prediction, score, nnz
        candidates.append({"c": c_value, "f1": float(f1_score(truth, predicted, average="macro", zero_division=0)),
                           "predicted": predicted, "scores": scores, "nonzero": nonzero})
    best = max(candidates, key=lambda candidate: (candidate["f1"], -candidate["c"]))
    threshold, curve = choose_threshold(best["predicted"], best["scores"], best["nonzero"], truth)
    model = pipeline(best["c"]).fit(texts, truth)
    return model, threshold, {
        "train_count": len(selected), "class_counts": dict(sorted(Counter(truth).items())),
        "cv_candidates": [{"C": candidate["c"], "macro_f1": round(candidate["f1"], 8)} for candidate in candidates],
        "selected_C": best["c"], "threshold": threshold, "threshold_cv_curve": curve,
        "feature_count": len(model[0].vocabulary_),
        "folds": [{"train_ids": [selected[i]["video_id"] for i in train_index],
                   "validation_ids": [selected[i]["video_id"] for i in valid_index]} for train_index, valid_index in folds],
    }


def predict_rows(models, thresholds, rows, transform):
    predictions = {}
    for unit, model in models.items():
        selected = [row for row in rows if row["unit"] == unit]
        if not selected:
            continue
        predicted, scores, nonzero = model_predictions(model, [transform(row["title"]) for row in selected])
        for row, label, score, nnz in zip(selected, predicted, scores, nonzero, strict=True):
            threshold = thresholds[unit]
            predictions[row["video_id"]] = {
                "predicted_host": str(label), "uncalibrated_score": round(float(score), 8), "nonzero_features": int(nnz),
                "accepted": threshold is not None and float(score) >= threshold and int(nnz) > 0,
            }
    return predictions


def score_predictions(rows, predictions, labels):
    require(all(row["label"] for row in rows), "unlabeled rows must not enter accuracy")
    truth = [row["label"] for row in rows]
    predicted = [predictions[row["video_id"]]["predicted_host"] for row in rows]
    accepted = [predictions[row["video_id"]]["accepted"] for row in rows]
    correct = sum(expected == actual for expected, actual in zip(truth, predicted, strict=True))
    accepted_correct = sum(ok and expected == actual for expected, actual, ok in zip(truth, predicted, accepted, strict=True))
    count = len(rows)
    matrix_labels = labels + (["__abstain__"] if "__abstain__" in predicted else [])
    matrix = confusion_matrix(truth, predicted, labels=matrix_labels).tolist() if count else []
    require(sum(sum(line) for line in matrix) == count, "confusion matrix dropped predictions")
    return {
        "n": count, "correct": correct, "accuracy": round(correct / count, 8) if count else None,
        "macro_f1": round(float(f1_score(truth, predicted, labels=labels, average="macro", zero_division=0)), 8) if count else None,
        "accepted": sum(accepted), "accepted_correct": accepted_correct,
        "coverage": round(sum(accepted) / count, 8) if count else None,
        "selective_precision": round(accepted_correct / sum(accepted), 8) if any(accepted) else None,
        "confusion_labels": matrix_labels, "confusion_matrix": matrix,
        "macro_f1_labels": labels,
        "errors": [{"video_id": row["video_id"], "title": row["title"], "expected": row["label"], **predictions[row["video_id"]]}
                   for row in rows if predictions[row["video_id"]]["predicted_host"] != row["label"]],
    }


def serializable_row(row):
    return {**row, "event_at": row["event_at"].isoformat() if row["event_at"] else None}


def run(output):
    warnings.filterwarnings("error", category=ConvergenceWarning)
    rules = read_json(DOMAIN / "rules.json")
    terms = identity_terms(rules)
    rows, audit = load_rows(rules, terms)
    train, holdout, reviewed, purged = split_rows(rows)
    labels = sorted(member["id"] for member in rules["members"])
    units = sorted(rules["units"])
    output.mkdir(parents=True, exist_ok=True)
    model_dir = output / "models"
    model_dir.mkdir(exist_ok=True)
    audit.update({
        "train_count": len(train), "holdout_count": len(holdout), "purged_train_ids": purged,
        "train_ids": [row["video_id"] for row in train], "holdout_ids": [row["video_id"] for row in holdout],
        "reviewed_evaluation_only_ids": [row["video_id"] for row in reviewed],
        "holdout_by_unit": dict(sorted(Counter(row["unit"] for row in holdout).items())),
    })
    majority = {unit: Counter(row["label"] for row in train if row["unit"] == unit).most_common(1)[0][0] for unit in units}
    rule_predictions = {row["video_id"]: {"predicted_host": row["rule_hosts"][0] if len(row["rule_hosts"]) == 1 else "__abstain__",
                                          "accepted": len(row["rule_hosts"]) == 1} for row in rows}
    majority_predictions = {row["video_id"]: {"predicted_host": majority[row["unit"]], "accepted": True} for row in rows}
    report = {
        "schema_version": 1,
        "protocol": {"cutoff": CUTOFF.isoformat(), "seed": SEED, "C_candidates": C_VALUES,
                     "min_cv_accepted_per_unit": MIN_CV_ACCEPTED, "min_cv_precision": MIN_CV_PRECISION,
                     "ablation": "remove known names, personal/series tags, remaining hashtags and emoji; synthetic stress test",
                     "deployment": "offline_experiment_only"},
        "environment": {"python": sys.version.split()[0], **{name: version(name) for name in ("scikit-learn", "numpy", "scipy", "joblib", "threadpoolctl")}},
        "audit": audit,
        "baselines": {"rules_full_title": score_predictions(holdout, rule_predictions, labels),
                      "majority_by_unit": score_predictions(holdout, majority_predictions, labels)},
        "models": {},
    }
    transforms = {"full_title": normalize, "identity_removed": lambda title: remove_identity(title, terms)}
    challenge = [row for row in rows if row["evaluation_only"] or len(row["rule_hosts"]) != 1]
    for variant, transform in transforms.items():
        models, thresholds, summaries = {}, {}, {}
        for unit in units:
            model, threshold, summary = fit_unit(train, unit, transform)
            models[unit], thresholds[unit], summaries[unit] = model, threshold, summary
            artifact = model_dir / f"{variant}-{unit}.joblib"
            joblib.dump(model, artifact, compress=3)
            restored = joblib.load(artifact)
            probe = [transform(row["title"]) for row in holdout if row["unit"] == unit]
            np.testing.assert_allclose(model.predict_proba(probe), restored.predict_proba(probe), rtol=0, atol=0)
        predictions = predict_rows(models, thresholds, rows, transform)
        reviewed_single = [row for row in reviewed if row["label"]]
        report["models"][variant] = {
            "units": summaries,
            "holdout": score_predictions(holdout, predictions, labels),
            "reviewed_single": score_predictions(reviewed_single, predictions, labels),
            "challenge": [{"video_id": row["video_id"], "title": row["title"], "scope": row["scope"],
                           "expected": row["label"], "basis": row["label_basis"], **predictions[row["video_id"]]} for row in challenge],
            "holdout_predictions": [{"video_id": row["video_id"], "expected": row["label"], **predictions[row["video_id"]]} for row in holdout],
        }
        headline_keys = ("n", "correct", "accuracy", "macro_f1", "accepted", "coverage", "selective_precision")
        print(json.dumps({"variant": variant,
                          "holdout": {key: report["models"][variant]["holdout"][key] for key in headline_keys},
                          "reviewed_single": {key: report["models"][variant]["reviewed_single"][key] for key in headline_keys}}, ensure_ascii=False), flush=True)
    report["baselines"]["rules_reviewed_single"] = score_predictions([row for row in reviewed if row["label"]], rule_predictions, labels)
    input_paths = [Path(__file__).resolve(), HERE / "public-titles.json", HERE / "event-times.json", HERE / "review-labels.json",
                   HERE / "experiment.py.lock", DOMAIN / "rules.json", DOMAIN / "identify.go", DOMAIN / "testdata/title_corpus.json",
                   DOMAIN / "cmd/classify-titles/main.go"]
    report["input_sha256"] = {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in input_paths}
    report["model_sha256"] = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(model_dir.glob("*.joblib"))}
    (output / "dataset.json").write_text(json.dumps([serializable_row(row) for row in rows], ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    (output / "results.json").write_text(json.dumps(report, ensure_ascii=False, indent=2, sort_keys=True, allow_nan=False) + "\n", encoding="utf-8")
    (model_dir / "input-contract.json").write_text(json.dumps({"identity_terms": terms, "rules": rules, "protocol": report["protocol"],
                                                               "environment": report["environment"], "input_sha256": report["input_sha256"]},
                                                              ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({"saved": str(output), "train": len(train), "holdout": len(holdout), "merged": len(rows), "public_new": audit["public_new_videos"]}), flush=True)


def predict_saved(output, unit, title):
    contract = read_json(output / "models/input-contract.json")
    report = read_json(output / "results.json")
    require(unit in contract["rules"]["units"], "unknown unit")
    require(report["environment"]["scikit-learn"] == version("scikit-learn"), "model library version mismatch")
    row = {"video_id": "input", "unit": unit, "title": title}
    result = {}
    for variant in ("full_title", "identity_removed"):
        artifact = output / "models" / f"{variant}-{unit}.joblib"
        require(hashlib.sha256(artifact.read_bytes()).hexdigest() == report["model_sha256"][artifact.name], "model hash mismatch")
        model = joblib.load(artifact)
        transform = normalize if variant == "full_title" else lambda text: remove_identity(text, contract["identity_terms"])
        threshold = report["models"][variant]["units"][unit]["threshold"]
        result[variant] = predict_rows({unit: model}, {unit: threshold}, [row], transform)["input"]
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".tmp/mekpark-host-ml")
    parser.add_argument("--predict", help="저장된 실험 모델로 제목 하나의 후보를 확인한다.")
    parser.add_argument("--unit", choices=("unit-b", "achrora"))
    arguments = parser.parse_args()
    with threadpool_limits(limits=1):
        if arguments.predict is not None:
            require(arguments.unit is not None, "--predict requires --unit")
            predict_saved(arguments.output.resolve(), arguments.unit, arguments.predict)
        else:
            run(arguments.output.resolve())
