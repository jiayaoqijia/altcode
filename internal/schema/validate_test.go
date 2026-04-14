package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func intPtr(n int) *int { return &n }

func TestValidate_Object_Happy(t *testing.T) {
	s := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"name": {Type: "string"},
			"age":  {Type: "number"},
		},
		Required: []string{"name"},
	}
	data := map[string]any{"name": "alice", "age": float64(30)}
	errs := s.Validate(data)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidate_MissingRequired(t *testing.T) {
	s := &Schema{
		Type:     "object",
		Required: []string{"id", "name"},
	}
	data := map[string]any{"name": "bob"}
	errs := s.Validate(data)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0] != `$: missing required field "id"` {
		t.Fatalf("unexpected error: %s", errs[0])
	}
}

func TestValidate_WrongType(t *testing.T) {
	s := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"count": {Type: "number"},
		},
	}
	data := map[string]any{"count": "not-a-number"}
	errs := s.Validate(data)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidate_Array(t *testing.T) {
	s := &Schema{
		Type:  "array",
		Items: &Schema{Type: "string"},
	}
	data := []any{"a", "b", "c"}
	errs := s.Validate(data)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	bad := []any{"a", float64(2)}
	errs = s.Validate(bad)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidate_String_Length(t *testing.T) {
	s := &Schema{
		Type:      "string",
		MinLength: intPtr(3),
		MaxLength: intPtr(10),
	}
	errs := s.Validate("ab")
	if len(errs) != 1 {
		t.Fatalf("expected 1 min-length error, got %d: %v", len(errs), errs)
	}
	errs = s.Validate("hello world")
	if len(errs) != 1 {
		t.Fatalf("expected 1 max-length error, got %d: %v", len(errs), errs)
	}
	errs = s.Validate("hello")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestValidate_Enum(t *testing.T) {
	s := &Schema{
		Type: "string",
		Enum: []any{"red", "green", "blue"},
	}
	errs := s.Validate("green")
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	errs = s.Validate("yellow")
	if len(errs) != 1 {
		t.Fatalf("expected 1 enum error, got %d: %v", len(errs), errs)
	}
}

func TestValidate_Nested(t *testing.T) {
	s := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"address": {
				Type: "object",
				Properties: map[string]*Schema{
					"city": {Type: "string"},
					"zip":  {Type: "number"},
				},
				Required: []string{"city"},
			},
		},
		Required: []string{"address"},
	}
	data := map[string]any{
		"address": map[string]any{
			"city": "SF",
			"zip":  float64(94105),
		},
	}
	errs := s.Validate(data)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	bad := map[string]any{
		"address": map[string]any{"zip": "not-number"},
	}
	errs = s.Validate(bad)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidate_Boolean(t *testing.T) {
	s := &Schema{Type: "boolean"}
	errs := s.Validate(true)
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	errs = s.Validate("true")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidate_NilSchema(t *testing.T) {
	var s *Schema
	errs := s.Validate("anything")
	if errs != nil {
		t.Fatalf("nil schema should return nil, got %v", errs)
	}
}

func TestLoadSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.json")
	content := `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		},
		"required": ["name"]
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Type != "object" {
		t.Fatalf("expected type object, got %s", s.Type)
	}
	if len(s.Required) != 1 || s.Required[0] != "name" {
		t.Fatalf("unexpected required: %v", s.Required)
	}

	// non-existent file
	_, err = LoadSchema(filepath.Join(dir, "nope.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}

	// invalid JSON
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = LoadSchema(badPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateJSON(t *testing.T) {
	s := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"status": {Type: "string"},
		},
		Required: []string{"status"},
	}
	errs, err := s.ValidateJSON(`{"status": "ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}

	errs, err = s.ValidateJSON(`{"other": 1}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}

	// invalid JSON input
	_, err = s.ValidateJSON(`{broken`)
	if err == nil {
		t.Fatal("expected parse error")
	}
}
