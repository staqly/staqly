"""The hand-written models must match apps/api/openapi.yaml (the contract)."""

from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest
import yaml

from staqly import models

SPEC = Path(__file__).resolve().parents[3] / "apps" / "api" / "openapi.yaml"
pytestmark = pytest.mark.skipif(not SPEC.exists(), reason="openapi.yaml not found (outside the monorepo)")

PAIRS: dict[str, type[models.StaqlyModel]] = {
    "Post": models.Post,
    "PostTarget": models.PostTarget,
    "PostTargetInput": models.PostTargetInput,
    "Reply": models.Reply,
    "ThreadPart": models.ThreadPart,
    "ThreadPartOut": models.ThreadPartOut,
    "PostPage": models.PostPage,
    "PostPreview": models.PostPreview,
    "Tag": models.Tag,
    "WorkItem": models.WorkItem,
    "Collection": models.Collection,
    "Media": models.Media,
    "MediaLibraryItem": models.MediaLibraryItem,
    "MediaPage": models.MediaPage,
    "MediaUse": models.MediaUse,
    "Channel": models.Channel,
    "Provider": models.Provider,
    "ProviderField": models.ProviderField,
    "ProviderLimits": models.ProviderLimits,
    "Schedule": models.Schedule,
    "ScheduleRule": models.ScheduleRule,
    "Job": models.Job,
    "JobPage": models.JobPage,
    "JobStats": models.JobStats,
    "Event": models.Event,
    "EventPage": models.EventPage,
    "Webhook": models.Webhook,
    "Delivery": models.Delivery,
    "Plan": models.Plan,
    "Workspace": models.Workspace,
    "APIKey": models.APIKey,
    "ErrorBody": models.ErrorBody,
    "Alert": models.Alert,
    "AlertDelivery": models.AlertDelivery,
    "WorkspaceExport": models.WorkspaceExport,
    "PurgeResult": models.PurgeResult,
    "Readiness": models.Readiness,
    "Session": models.Session,
    "Billing": models.Billing,
    "BillingURL": models.BillingURL,
    "Analytics": models.Analytics,
    "AnalyticsTotals": models.AnalyticsTotals,
    "AnalyticsMetrics": models.AnalyticsMetrics,
    "ChannelAnalytics": models.ChannelAnalytics,
    "AnalyticsTimeSlot": models.AnalyticsTimeSlot,
    "TelegramConnect": models.TelegramConnect,
    "ProviderCheck": models.ProviderCheck,
    "MediaProject": models.MediaProject,
    "DesignBrand": models.DesignBrand,
    "StockPage": models.StockPage,
    "StockPhoto": models.StockPhoto,
    "AIStatus": models.AIStatus,
    "AIOperation": models.AIOperation,
    "AIJob": models.AIJob,
}


@pytest.fixture(scope="module")
def schemas() -> dict[str, Any]:
    spec = yaml.safe_load(SPEC.read_text())
    result: dict[str, Any] = spec["components"]["schemas"]
    return result


def _props(schema: dict[str, Any], schemas: dict[str, Any]) -> dict[str, Any]:
    if "allOf" in schema:
        out: dict[str, Any] = {}
        for part in schema["allOf"]:
            if "$ref" in part:
                part = schemas[part["$ref"].rsplit("/", 1)[-1]]
            out.update(_props(part, schemas))
        return out
    props: dict[str, Any] = schema.get("properties", {})
    return props


@pytest.mark.parametrize("name", sorted(PAIRS))
def test_model_fields_match_schema(name: str, schemas: dict[str, Any]) -> None:
    model = PAIRS[name]
    props = _props(schemas[name], schemas)
    assert set(model.model_fields) == set(props), f"{name}: model and OpenAPI properties differ"
    for req in schemas[name].get("required", []):
        assert req in model.model_fields


def test_statuses_and_scopes_match(schemas: dict[str, Any]) -> None:
    from typing import get_args

    assert list(get_args(models.PostStatus)) == schemas["PostStatus"]["enum"]
    assert list(get_args(models.TargetStatus)) == schemas["TargetStatus"]["enum"]
    assert list(get_args(models.ChannelStatus)) == schemas["ChannelStatus"]["enum"]
    assert list(get_args(models.Scope)) == schemas["Scope"]["enum"]


def test_nested_models_match(schemas: dict[str, Any]) -> None:
    assert set(models.Actor.model_fields) == set(schemas["Event"]["properties"]["actor"]["properties"])
    assert set(models.JobPayload.model_fields) == set(schemas["Job"]["properties"]["payload"]["properties"])
    assert set(models.PostPreviewTarget.model_fields) == set(
        schemas["PostPreview"]["properties"]["targets"]["items"]["properties"]
    )
    assert set(models.ProviderFieldOption.model_fields) == set(
        schemas["ProviderField"]["properties"]["options"]["items"]["properties"]
    )
    assert set(models.ProviderCheckItem.model_fields) == set(
        schemas["ProviderCheck"]["properties"]["checks"]["items"]["properties"]
    )
    assert set(models.AIUsage.model_fields) == set(schemas["AIStatus"]["properties"]["usage"]["properties"])
    job = schemas["AIJob"]["properties"]
    assert set(models.AIJobError.model_fields) == set(job["error"]["oneOf"][1]["properties"])
    assert set(models.AIJobResult.model_fields) == set(job["result"]["oneOf"][1]["properties"])
    options = _inline("/v1/channels/{id}/options/{key}", "get", "200")["properties"]["data"]["items"]
    assert set(models.ChannelOption.model_fields) == set(options["properties"])
    assert set(models.JobKindStats.model_fields) == set(
        schemas["JobStats"]["properties"]["by_kind"]["additionalProperties"]["properties"]
    )


def test_analytics_nested_models_match(schemas: dict[str, Any]) -> None:
    def wire(model: type[models.StaqlyModel]) -> set[str]:
        return {f.alias or n for n, f in model.model_fields.items()}

    a = schemas["Analytics"]["properties"]
    top = a["top_posts"]["properties"]["posts"]["items"]
    pairs: list[tuple[type[models.StaqlyModel], dict[str, Any]]] = [
        (models.AnalyticsRange, a["range"]),
        (models.AnalyticsQueue, a["queue"]),
        (models.AnalyticsDay, a["daily"]["items"]),
        (models.AnalyticsSource, a["sources"]["items"]),
        (models.PostingTimes, a["posting_times"]),
        (models.TopPosts, a["top_posts"]),
        (models.TopPost, top),
        (models.TopPostChannel, top["properties"]["channels"]["items"]),
        (models.AnalyticsFailure, a["failures"]["items"]),
        (models.AnalyticsFeedback, a["feedback"]),
        (models.MetricsCoverage, schemas["ChannelAnalytics"]["properties"]["metrics_coverage"]),
    ]
    for model, schema in pairs:
        assert wire(model) == set(schema["properties"]), f"{model.__name__}: model and OpenAPI differ"


def test_plan_limits_match(schemas: dict[str, Any]) -> None:
    assert set(models.PlanLimits.model_fields) == set(schemas["PlanLimits"]["properties"])
    bonus = schemas["Plan"]["properties"]["limit_bonuses"]["items"]["properties"]
    assert {f.alias or n for n, f in models.LimitBonus.model_fields.items()} == set(bonus)


def test_usage_matches(schemas: dict[str, Any]) -> None:
    spec = yaml.safe_load(SPEC.read_text())
    usage = spec["paths"]["/v1/workspace/usage"]["get"]["responses"]["200"]["content"]["application/json"][
        "schema"
    ]
    assert set(models.Usage.model_fields) == set(usage["properties"]["usage"]["properties"])


def _inline(path: str, method: str, status: str) -> dict[str, Any]:
    spec = yaml.safe_load(SPEC.read_text())
    schema: dict[str, Any] = spec["paths"][path][method]["responses"][status]["content"]["application/json"][
        "schema"
    ]
    return schema


@pytest.mark.parametrize(
    ("path", "method", "status", "model"),
    [
        ("/v1/setup", "get", "200", models.SetupStatus),
        ("/v1/device-links", "post", "201", models.DeviceLink),
        ("/v1/device-links/redeem", "post", "201", models.SessionCreated),
        ("/v1/sessions/api-key", "post", "201", models.SessionCreated),
        ("/v1/sessions/recover", "post", "201", models.SessionRecovered),
        ("/v1/recovery-key/rotate", "post", "200", models.RecoveryKey),
        ("/v1/access-token", "get", "200", models.AccessToken),
        ("/v1/access-token/rotate", "post", "200", models.AccessTokenRotated),
        ("/livez", "get", "200", models.Liveness),
        ("/v1/channels/{id}/test", "post", "200", models.ChannelTestResult),
        ("/v1/alerts/{id}/test", "post", "200", models.AlertTestResult),
    ],
)
def test_inline_response_models_match(
    path: str, method: str, status: str, model: type[models.StaqlyModel]
) -> None:
    schema = _inline(path, method, status)
    assert set(model.model_fields) == set(schema["properties"]), f"{path}: model and OpenAPI differ"
    for req in schema.get("required", []):
        assert req in model.model_fields
