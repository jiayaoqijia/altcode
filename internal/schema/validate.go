package schema

import (
	"encoding/json"
	"fmt"
	"os"
)

// Schema represents a JSON Schema for validation.
type Schema struct {
	Type       string             `json:"type"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Required   []string           `json:"required,omitempty"`
	Items      *Schema            `json:"items,omitempty"`
	Enum       []any              `json:"enum,omitempty"`
	MinLength  *int               `json:"minLength,omitempty"`
	MaxLength  *int               `json:"maxLength,omitempty"`
}

// LoadSchema reads and parses a JSON Schema file.
func LoadSchema(path string) (*Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	var s Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}
	return &s, nil
}

// Validate checks a JSON value against the schema.
// Returns a list of validation errors (empty = valid).
func (s *Schema) Validate(data any) []string {
	var errs []string
	s.validate("$", data, &errs)
	return errs
}

func (s *Schema) validate(path string, data any, errs *[]string) {
	if s == nil {
		return
	}
	switch s.Type {
	case "object":
		s.validateObject(path, data, errs)
	case "array":
		s.validateArray(path, data, errs)
	case "string":
		s.validateString(path, data, errs)
	case "number", "integer":
		if _, ok := data.(float64); !ok {
			*errs = append(*errs, fmt.Sprintf(
				"%s: expected number, got %T", path, data))
		}
	case "boolean":
		if _, ok := data.(bool); !ok {
			*errs = append(*errs, fmt.Sprintf(
				"%s: expected boolean, got %T", path, data))
		}
	}
	s.validateEnum(path, data, errs)
}

func (s *Schema) validateObject(
	path string, data any, errs *[]string,
) {
	obj, ok := data.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf(
			"%s: expected object, got %T", path, data))
		return
	}
	for _, req := range s.Required {
		if _, exists := obj[req]; !exists {
			*errs = append(*errs, fmt.Sprintf(
				"%s: missing required field %q", path, req))
		}
	}
	for key, prop := range s.Properties {
		if val, exists := obj[key]; exists {
			prop.validate(path+"."+key, val, errs)
		}
	}
}

func (s *Schema) validateArray(
	path string, data any, errs *[]string,
) {
	arr, ok := data.([]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf(
			"%s: expected array, got %T", path, data))
		return
	}
	if s.Items != nil {
		for i, item := range arr {
			s.Items.validate(
				fmt.Sprintf("%s[%d]", path, i), item, errs)
		}
	}
}

func (s *Schema) validateString(
	path string, data any, errs *[]string,
) {
	str, ok := data.(string)
	if !ok {
		*errs = append(*errs, fmt.Sprintf(
			"%s: expected string, got %T", path, data))
		return
	}
	if s.MinLength != nil && len(str) < *s.MinLength {
		*errs = append(*errs, fmt.Sprintf(
			"%s: string too short (min %d)", path, *s.MinLength))
	}
	if s.MaxLength != nil && len(str) > *s.MaxLength {
		*errs = append(*errs, fmt.Sprintf(
			"%s: string too long (max %d)", path, *s.MaxLength))
	}
}

func (s *Schema) validateEnum(
	path string, data any, errs *[]string,
) {
	if len(s.Enum) == 0 {
		return
	}
	for _, v := range s.Enum {
		if fmt.Sprintf("%v", v) == fmt.Sprintf("%v", data) {
			return
		}
	}
	*errs = append(*errs, fmt.Sprintf(
		"%s: value not in enum %v", path, s.Enum))
}

// ValidateJSON parses a JSON string and validates it.
func (s *Schema) ValidateJSON(jsonStr string) ([]string, error) {
	var data any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return s.Validate(data), nil
}
