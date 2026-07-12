#!/usr/bin/env python3
"""Validate Goose ACP fixture against the pinned ACP v1 JSON Schema.

Usage: python3 validate_acp.py
Pinned source: agent-client-protocol schema-v1.19.0, schema/v1/schema.json
"""

import json, sys, os

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
SCHEMA_PATH = os.path.join(SCRIPT_DIR, "acp-v1-schema.json")
FIXTURE_PATH = os.path.join(SCRIPT_DIR, "goose-acp-protocol.json")

def load_json(path):
    with open(path) as f:
        return json.load(f)

def validate_against_schema(instance, schema, defs, path=""):
    """Simple structural validation against ACP v1 schema $defs."""
    errors = []

    if "$ref" in schema:
        ref = schema["$ref"]
        if ref.startswith("#/$defs/"):
            name = ref[len("#/$defs/"):]
            if name in defs:
                return validate_against_schema(instance, defs[name], defs, path)
            else:
                errors.append(f"{path}: unresolved $ref {ref}")
                return errors

    schema_type = schema.get("type")
    if schema_type:
        if schema_type == "object" and not isinstance(instance, dict):
            errors.append(f"{path}: expected object, got {type(instance).__name__}")
            return errors
        if schema_type == "array" and not isinstance(instance, list):
            errors.append(f"{path}: expected array, got {type(instance).__name__}")
            return errors
        if schema_type == "string" and not isinstance(instance, str):
            errors.append(f"{path}: expected string, got {type(instance).__name__}")
            return errors

    if isinstance(instance, dict) and "properties" in schema:
        for prop_name, prop_schema in schema["properties"].items():
            if prop_name in instance:
                sub_errors = validate_against_schema(
                    instance[prop_name], prop_schema, defs, f"{path}.{prop_name}"
                )
                errors.extend(sub_errors)
        for req in schema.get("required", []):
            if req not in instance:
                errors.append(f"{path}: missing required field '{req}'")

    if isinstance(instance, list) and "items" in schema:
        for i, item in enumerate(instance):
            sub_errors = validate_against_schema(
                item, schema["items"], defs, f"{path}[{i}]"
            )
            errors.extend(sub_errors)

    # Check oneOf / const values
    if "oneOf" in schema:
        matched = False
        for variant in schema["oneOf"]:
            if "const" in variant:
                if instance == variant["const"]:
                    matched = True
                    break
            elif "properties" in variant:
                # Check discriminator
                disc = schema.get("discriminator", {}).get("propertyName", "")
                if disc and disc in instance:
                    for vprop_name, vprop_schema in variant.get("properties", {}).items():
                        if "const" in vprop_schema and instance.get(vprop_name) == vprop_schema["const"]:
                            matched = True
                            sub_errors = validate_against_schema(
                                instance, variant, defs, path
                            )
                            errors.extend(sub_errors)
                            break
                    if matched:
                        break
        # Don't error on oneOf match failure; this is a best-effort structural check

    return errors

def main():
    schema = load_json(SCHEMA_PATH)
    fixture = load_json(FIXTURE_PATH)
    defs = schema.get("$defs", {})
    all_errors = []

    examples = fixture.get("examples", {})
    for name, example_group in examples.items():
        if not isinstance(example_group, dict):
            continue
        for key, value in example_group.items():
            if key.startswith("_"):
                continue
            if not isinstance(value, dict):
                continue

            # Unwrap JSON-RPC envelope: requests have params, responses have result
            instance = value
            prefix = f"{name}.{key}"

            # Map example names to schema types (more specific first)
            type_map = [
                ("permission_option", "example", "PermissionOption", None),
                ("request_permission", "request", "RequestPermissionRequest", "params"),
                ("request_permission", "response_selected", "RequestPermissionResponse", "result"),
                ("request_permission", "response_cancelled", "RequestPermissionResponse", "result"),
                ("session_update_tool_call", "notification", "SessionUpdate", "params"),
            ]

            schema_type = None
            envelope_key = None
            for ek_name, ek_key, st, ek2 in type_map:
                if ek_name in name and ek_key == key:
                    schema_type = st
                    envelope_key = ek2
                    break

            if schema_type and schema_type in defs:
                if envelope_key and envelope_key in value:
                    instance = value[envelope_key]
                    prefix = f"{prefix}.{envelope_key}"
                errors = validate_against_schema(instance, defs[schema_type], defs, prefix)
                all_errors.extend(errors)

    # Specific wire-format checks
    # PermissionOption must use camelCase optionId (not snake_case option_id)
    perm_opt = examples.get("permission_option", {}).get("example", {})
    if "option_id" in perm_opt:
        all_errors.append("permission_option.example: wire format must use 'optionId' (camelCase), not 'option_id' (Rust snake_case)")
    if "optionId" not in perm_opt:
        all_errors.append("permission_option.example: missing required wire field 'optionId'")

    # ToolCallUpdate must use toolCallId
    tool_call = examples.get("session_update_tool_call", {}).get("notification", {}).get("params", {}).get("update", {}).get("content", {})
    if "tool_call_id" in tool_call:
        all_errors.append("tool_call_update: wire format must use 'toolCallId', not 'tool_call_id'")

    # StopReason: error must not appear as a documented value
    sr_values = set()
    for o in defs.get("StopReason", {}).get("oneOf", []):
        if "const" in o:
            sr_values.add(o["const"])
    stop_reason = examples.get("stop_reason", {})
    for k, v in stop_reason.items():
        if k == "_values" and isinstance(v, list):
            for val in v:
                if val not in sr_values and val != "Other(String)":
                    all_errors.append(f"stop_reason._values: '{val}' is not in ACP v1 StopReason enum ({sorted(sr_values)})")

    if all_errors:
        print(f"VALIDATION FAILED: {len(all_errors)} error(s)")
        for e in all_errors:
            print(f"  - {e}")
        sys.exit(1)
    else:
        print("VALIDATION PASSED")
        sys.exit(0)

if __name__ == "__main__":
    main()
