#!/usr/bin/env python3
"""Validate Goose ACP fixture against the pinned ACP v1 JSON Schema.

Requires: pip3 install jsonschema
Pinned source: agent-client-protocol schema-v1.19.0, schema/v1/schema.json
"""

import json, sys, os
from jsonschema import validate, ValidationError, SchemaError

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
SCHEMA_PATH = os.path.join(SCRIPT_DIR, "acp-v1-schema.json")
FIXTURE_PATH = os.path.join(SCRIPT_DIR, "goose-acp-protocol.json")

def load_json(path):
    with open(path) as f:
        return json.load(f)

def validate_instance(instance, schema, defs, label):
    """Validate instance against a named schema definition."""
    if isinstance(schema, str) and schema.startswith("#/$defs/"):
        schema = defs[schema[len("#/$defs/"):]]

    # Resolve all $ref in the schema before validation
    resolved = resolve_refs(schema, defs)

    try:
        validate(instance=instance, schema=resolved)
        return []
    except ValidationError as e:
        return [f"{label}: {e.message} (at {'/'.join(str(p) for p in e.absolute_path)})"]

def resolve_refs(schema, defs):
    """Recursively resolve $ref pointers in a schema."""
    if isinstance(schema, dict):
        if "$ref" in schema:
            ref = schema["$ref"]
            if ref.startswith("#/$defs/"):
                name = ref[len("#/$defs/"):]
                if name in defs:
                    resolved = resolve_refs(defs[name], defs)
                    # Merge any other keys alongside $ref
                    rest = {k: v for k, v in schema.items() if k != "$ref"}
                    if rest:
                        resolved = {**resolved, **rest}
                    return resolved
            return schema
        return {k: resolve_refs(v, defs) for k, v in schema.items()}
    elif isinstance(schema, list):
        return [resolve_refs(item, defs) for item in schema]
    return schema

def main():
    schema_doc = load_json(SCHEMA_PATH)
    fixture = load_json(FIXTURE_PATH)
    defs = schema_doc.get("$defs", {})
    all_errors = []
    examples = fixture.get("examples", {})

    # ── Positive validations ──

    # 1. PermissionOption
    opt = examples.get("permission_option", {}).get("example")
    if opt:
        all_errors.extend(validate_instance(opt, defs["PermissionOption"], defs, "permission_option.example"))

    # 2. RequestPermissionRequest (unwrap JSON-RPC envelope)
    req = examples.get("request_permission", {}).get("request", {})
    if "params" in req:
        all_errors.extend(validate_instance(req["params"], defs["RequestPermissionRequest"], defs, "request_permission.request.params"))

    # 3. RequestPermissionResponse — selected outcome
    res_sel = examples.get("request_permission", {}).get("response_selected", {})
    if "result" in res_sel:
        all_errors.extend(validate_instance(res_sel["result"], defs["RequestPermissionResponse"], defs, "request_permission.response_selected.result"))

    # 4. RequestPermissionResponse — cancelled outcome
    res_canc = examples.get("request_permission", {}).get("response_cancelled", {})
    if "result" in res_canc:
        all_errors.extend(validate_instance(res_canc["result"], defs["RequestPermissionResponse"], defs, "request_permission.response_cancelled.result"))

    # 5. SessionUpdate (tool_call_update) — validate params.update, not params
    su = examples.get("session_update_tool_call", {}).get("notification", {})
    if "params" in su and "update" in su["params"]:
        all_errors.extend(validate_instance(su["params"]["update"], defs["SessionUpdate"], defs, "session_update_tool_call.notification.params.update"))

    # ── StopReason validation ──
    sr_values = set()
    for o in defs.get("StopReason", {}).get("oneOf", []):
        if "const" in o:
            sr_values.add(o["const"])
    stop_reason = examples.get("stop_reason", {})
    for val in stop_reason.get("_values", []):
        if val not in sr_values:
            all_errors.append(f"stop_reason._values: '{val}' is not in ACP v1 StopReason enum (valid: {sorted(sr_values)})")
    if "Other(String)" in stop_reason.get("_values", []):
        all_errors.append("stop_reason._values: 'Other(String)' is not an ACP v1 StopReason value. Remove it.")

    # ── Wire format checks (examples section only — _wire_traceability may reference Rust names) ──
    examples_json = json.dumps(fixture.get("examples", {}))
    if "option_id" in examples_json:
        all_errors.append("Fixture examples contain snake_case 'option_id' — must use camelCase 'optionId'")
    if '"toolCallId"' not in examples_json:
        all_errors.append("Fixture examples missing 'toolCallId' (wire field name for tool call identity)")

    # ── Negative regression tests ──
    neg_tests = [
        ({"optionId": "x", "name": "x", "kind": "NOT_A_KIND"}, defs["PermissionOption"], "negative: invalid permission kind"),
        ({"optionId": "x", "name": "x"}, defs["PermissionOption"], "negative: missing required 'kind'"),
        ({"outcome": {"outcome": "invented", "optionId": "x"}}, defs["RequestPermissionResponse"], "negative: unknown outcome discriminator"),
        ({"outcome": {"outcome": "selected"}}, defs["RequestPermissionResponse"], "negative: selected outcome missing optionId"),
        ({"sessionUpdate": "invented", "toolCallId": "x"}, defs["SessionUpdate"], "negative: unknown sessionUpdate discriminator"),
        ({"option_id": "x", "name": "x", "kind": "allow_once"}, defs["PermissionOption"], "negative: snake_case option_id instead of optionId"),
    ]
    for instance, schema, label in neg_tests:
        errs = validate_instance(instance, schema, defs, label)
        if not errs:
            all_errors.append(f"{label}: FAILED — invalid fixture was accepted (validator fail-open)")

    if all_errors:
        print(f"VALIDATION FAILED: {len(all_errors)} error(s)")
        for e in all_errors:
            print(f"  - {e}")
        sys.exit(1)
    else:
        print(f"VALIDATION PASSED ({len(neg_tests)} negative regression tests all correctly rejected)")
        sys.exit(0)

if __name__ == "__main__":
    main()
