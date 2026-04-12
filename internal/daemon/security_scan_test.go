package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestParseTrufflehogOutput(t *testing.T) {
	input := `{"SourceMetadata":{"Data":{"Filesystem":{"file":"main.go","line":10}}},"DetectorName":"AWS"}
{"SourceMetadata":{"Data":{"Filesystem":{"file":"config.yaml","line":3}}},"DetectorName":"GitHub"}`

	findings := parseTrufflehogOutput([]byte(input))
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].Tool != "trufflehog" {
		t.Errorf("tool = %q, want trufflehog", findings[0].Tool)
	}
	if findings[0].File != "main.go" {
		t.Errorf("file = %q, want main.go", findings[0].File)
	}
	if findings[0].Line != 10 {
		t.Errorf("line = %d, want 10", findings[0].Line)
	}
	if findings[0].Severity != "critical" {
		t.Errorf("severity = %q, want critical", findings[0].Severity)
	}
	if findings[1].RuleID != "GitHub" {
		t.Errorf("rule_id = %q, want GitHub", findings[1].RuleID)
	}
}

func TestParseSemgrepOutput(t *testing.T) {
	envelope := map[string]any{
		"results": []map[string]any{
			{
				"check_id": "python.lang.security.eval",
				"path":     "app.py",
				"start":    map[string]any{"line": 42},
				"extra": map[string]any{
					"message":  "eval is dangerous",
					"severity": "ERROR",
				},
			},
			{
				"check_id": "generic.secrets.password",
				"path":     "config.py",
				"start":    map[string]any{"line": 7},
				"extra": map[string]any{
					"message":  "hardcoded password",
					"severity": "WARNING",
				},
			},
		},
	}
	data, _ := json.Marshal(envelope)

	findings := parseSemgrepOutput(data)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].Tool != "semgrep" {
		t.Errorf("tool = %q, want semgrep", findings[0].Tool)
	}
	if findings[0].Severity != "high" {
		t.Errorf("severity = %q, want high (ERROR)", findings[0].Severity)
	}
	if findings[1].Severity != "medium" {
		t.Errorf("severity = %q, want medium (WARNING)",
			findings[1].Severity)
	}
}

func TestRunSecurityScan_MockedTools(t *testing.T) {
	thOutput := `{"SourceMetadata":{"Data":{"Filesystem":{"file":"leak.go","line":1}}},"DetectorName":"PrivateKey"}`
	sgOutput, _ := json.Marshal(map[string]any{
		"results": []map[string]any{
			{
				"check_id": "go.lang.eval",
				"path":     "eval.go",
				"start":    map[string]any{"line": 5},
				"extra": map[string]any{
					"message":  "unsafe eval",
					"severity": "INFO",
				},
			},
		},
	})

	runner := func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		switch name {
		case "trufflehog":
			return []byte(thOutput), nil
		case "semgrep":
			return sgOutput, nil
		default:
			return nil, fmt.Errorf("unknown: %s", name)
		}
	}

	result, err := RunSecurityScan(
		context.Background(), "/tmp/repo", runner)
	if err != nil {
		t.Fatalf("RunSecurityScan: %v", err)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(result.Findings))
	}
	// trufflehog finding is critical -> Passed should be false.
	if result.Passed {
		t.Error("expected Passed=false with critical finding")
	}
}
