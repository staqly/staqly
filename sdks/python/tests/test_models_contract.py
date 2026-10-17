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
    "Item": models.Item,
    "Collection": models.Collection,
    "ActionDef": models.ActionDef,
    "MediaRef": models.MediaRef,
    "Event": models.Event,
    "EventPage": models.EventPage,
    "ItemPage": models.ItemPage,
    "Webhook": models.Webhook,
    "Delivery": models.Delivery,
    "Watcher": models.Watcher,
    "Plan": models.Plan,
    "Workspace": models.Workspace,
    "APIKey": models.APIKey,
    "ErrorBody": models.ErrorBody,
    "NotificationChannel": models.NotificationChannel,
    "NotificationDelivery": models.NotificationDelivery,
    "WorkspaceExport": models.WorkspaceExport,
    "PurgeResult": models.PurgeResult,
    "Readiness": models.Readiness,
    "Session": models.Session,
    "DecisionResult": models.DecisionResult,
    "Billing": models.Billing,
    "BillingURL": models.BillingURL,
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


def test_media_extends_media_ref(schemas: dict[str, Any]) -> None:
    assert set(models.Media.model_fields) == set(_props(schemas["Media"], schemas))


def test_item_statuses_match(schemas: dict[str, Any]) -> None:
    from typing import get_args

    assert list(get_args(models.ItemStatus)) == schemas["ItemStatus"]["enum"]
    assert list(get_args(models.ItemType)) == schemas["ItemType"]["enum"]
    assert list(get_args(models.Scope)) == schemas["Scope"]["enum"]


def test_plan_limits_match(schemas: dict[str, Any]) -> None:
    assert set(models.PlanLimits.model_fields) == set(schemas["Plan"]["properties"]["limits"]["properties"])


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
        ("/livez", "get", "200", models.Liveness),
    ],
)
def test_inline_response_models_match(
    path: str, method: str, status: str, model: type[models.StaqlyModel]
) -> None:
    schema = _inline(path, method, status)
    assert set(model.model_fields) == set(schema["properties"]), f"{path}: model and OpenAPI differ"
    for req in schema.get("required", []):
        assert req in model.model_fields
