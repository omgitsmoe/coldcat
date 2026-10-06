package httpapi

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func openAPIDocument(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../doc/openapi.json")
	if err != nil {
		t.Fatal(err)
	}

	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	return document
}

func resolveReference(t *testing.T, document map[string]any, ref string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(ref, "#/") {
		t.Fatalf("nonlocal reference: %s", ref)
	}

	var value any = document
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("invalid reference: %s", ref)
		}

		value, ok = object[part]
		if !ok {
			t.Fatalf("missing reference: %s", ref)
		}
	}

	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("reference is not an object: %s", ref)
	}

	return result
}

func assertResponseSchema(t *testing.T, document map[string]any, schema map[string]any, value any) {
	t.Helper()
	if ref, ok := schema["$ref"].(string); ok {
		assertResponseSchema(t, document, resolveReference(t, document, ref), value)
		return
	}

	var kinds []any
	switch kind := schema["type"].(type) {
	case string:
		kinds = []any{kind}
	case []any:
		kinds = kind
	default:
		t.Fatal("response schema must declare a type")
	}

	valid := false
	for _, kind := range kinds {
		switch kind {
		case "null":
			valid = valid || value == nil
		case "string":
			_, ok := value.(string)
			valid = valid || ok
		case "boolean":
			_, ok := value.(bool)
			valid = valid || ok
		case "object":
			_, ok := value.(map[string]any)
			valid = valid || ok
		case "array":
			_, ok := value.([]any)
			valid = valid || ok
		default:
			t.Fatalf("unsupported response schema type: %v", kind)
		}
	}

	if !valid {
		t.Fatalf("value %#v does not match schema %v", value, schema)
	}

	if constant, ok := schema["const"]; ok && !reflect.DeepEqual(constant, value) {
		t.Fatalf("constant mismatch: %v != %v", value, constant)
	}

	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, item := range enum {
			found = found || reflect.DeepEqual(item, value)
		}

		if !found {
			t.Fatalf("value %v outside enum %v", value, enum)
		}
	}

	switch typed := value.(type) {
	case string:
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(typed) {
			t.Fatalf("%q does not match %s", typed, pattern)
		}

		if schema["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, typed); err != nil {
				t.Fatal(err)
			}
		}
	case map[string]any:
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatal("object schema missing properties")
		}

		for _, required := range schema["required"].([]any) {
			if _, ok := typed[required.(string)]; !ok {
				t.Fatalf("missing required property %s", required)
			}
		}

		for key, item := range typed {
			property, ok := properties[key].(map[string]any)
			if !ok {
				t.Fatalf("undocumented response property: %s", key)
			}

			assertResponseSchema(t, document, property, item)
		}
	case []any:
		for _, item := range typed {
			assertResponseSchema(t, document, schema["items"].(map[string]any), item)
		}
	}
}

func TestOpenAPIReferencesAndExamples(t *testing.T) {
	document := openAPIDocument(t)
	if document["openapi"] != "3.1.0" {
		t.Fatal("wrong OpenAPI version")
	}

	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			if ref, ok := typed["$ref"].(string); ok {
				resolveReference(t, document, ref)
			}

			if schema, ok := typed["schema"].(map[string]any); ok {
				if example, ok := typed["example"]; ok {
					assertResponseSchema(t, document, schema, example)
				}

				if examples, ok := typed["examples"].(map[string]any); ok {
					for _, example := range examples {
						assertResponseSchema(t, document, schema, example.(map[string]any)["value"])
					}
				}
			}

			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(document)
}

func TestSnapshotListOpenAPIContract(t *testing.T) {
	document := openAPIDocument(t)
	paths := document["paths"].(map[string]any)
	operation := paths["/api/v1/disks/{id}/snapshots"].(map[string]any)["get"].(map[string]any)
	var names []string
	for _, item := range operation["parameters"].([]any) {
		parameter := item.(map[string]any)
		if ref, ok := parameter["$ref"].(string); ok {
			parameter = resolveReference(t, document, ref)
		}
		names = append(names, parameter["name"].(string))
	}
	if !reflect.DeepEqual(names, []string{"id", "limit", "cursor"}) {
		t.Fatalf("snapshot list parameters: %v", names)
	}
	responses := operation["responses"].(map[string]any)
	for _, status := range []string{"200", "400", "404", "405", "409", "500", "503"} {
		if _, ok := responses[status]; !ok {
			t.Fatalf("missing response %s", status)
		}
	}
	response := responses["200"].(map[string]any)
	media := response["content"].(map[string]any)["application/json"].(map[string]any)
	if media["schema"].(map[string]any)["$ref"] != "#/components/schemas/SnapshotPage" {
		t.Fatal("snapshot list response must reference SnapshotPage")
	}
}
