package daemon

import (
	"bytes"
	"context"
	"encoding/json"
)

// SecurityResult holds findings from static analysis.
type SecurityResult struct {
	Findings []SecurityFinding `json:"findings"`
	Passed   bool              `json:"passed"`
}

// SecurityFinding is a single issue detected by a scanner.
type SecurityFinding struct {
	Tool     string `json:"tool"`     // "semgrep", "trufflehog"
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"` // "critical","high","medium","low"
	Message  string `json:"message"`
	RuleID   string `json:"rule_id"`
}

// RunSecurityScan runs trufflehog + semgrep in workDir.
// Critical/high findings set Passed=false.
func RunSecurityScan(
	ctx context.Context, workDir string, run cmdRunner,
) (*SecurityResult, error) {
	result := &SecurityResult{Passed: true}

	// Trufflehog -- secret scanning.
	out, err := run(ctx, workDir,
		"trufflehog", "filesystem", workDir, "--json")
	if err == nil {
		result.Findings = append(
			result.Findings, parseTrufflehogOutput(out)...)
	}

	// Semgrep -- SAST.
	out, err = run(ctx, workDir,
		"semgrep", "scan", "--json", "--quiet", workDir)
	if err == nil {
		result.Findings = append(
			result.Findings, parseSemgrepOutput(out)...)
	}

	for _, f := range result.Findings {
		if f.Severity == "critical" || f.Severity == "high" {
			result.Passed = false
			break
		}
	}
	return result, nil
}

// parseTrufflehogOutput extracts findings from trufflehog JSON
// lines output. Each line is a separate JSON object.
func parseTrufflehogOutput(output []byte) []SecurityFinding {
	var findings []SecurityFinding

	type thFinding struct {
		SourceMetadata struct {
			Data struct {
				Filesystem struct {
					File string `json:"file"`
					Line int    `json:"line"`
				} `json:"Filesystem"`
			} `json:"Data"`
		} `json:"SourceMetadata"`
		DetectorName string `json:"DetectorName"`
	}

	dec := json.NewDecoder(bytes.NewReader(output))
	for dec.More() {
		var f thFinding
		if err := dec.Decode(&f); err != nil {
			continue
		}
		findings = append(findings, SecurityFinding{
			Tool:     "trufflehog",
			File:     f.SourceMetadata.Data.Filesystem.File,
			Line:     f.SourceMetadata.Data.Filesystem.Line,
			Severity: "critical",
			Message:  "secret detected: " + f.DetectorName,
			RuleID:   f.DetectorName,
		})
	}
	return findings
}

// parseSemgrepOutput extracts findings from semgrep --json output.
func parseSemgrepOutput(output []byte) []SecurityFinding {
	var envelope struct {
		Results []struct {
			CheckID string `json:"check_id"`
			Path    string `json:"path"`
			Start   struct {
				Line int `json:"line"`
			} `json:"start"`
			Extra struct {
				Message  string `json:"message"`
				Severity string `json:"severity"`
			} `json:"extra"`
		} `json:"results"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil
	}
	findings := make([]SecurityFinding, 0, len(envelope.Results))
	for _, r := range envelope.Results {
		sev := normalizeSeverity(r.Extra.Severity)
		findings = append(findings, SecurityFinding{
			Tool:     "semgrep",
			File:     r.Path,
			Line:     r.Start.Line,
			Severity: sev,
			Message:  r.Extra.Message,
			RuleID:   r.CheckID,
		})
	}
	return findings
}

func normalizeSeverity(s string) string {
	switch s {
	case "ERROR":
		return "high"
	case "WARNING":
		return "medium"
	case "INFO":
		return "low"
	default:
		return "medium"
	}
}
