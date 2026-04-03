package cost

import (
	"math"
	"strings"
	"sync"
	"testing"
)

func TestNewTracker(t *testing.T) {
	tr := NewTracker()
	if tr == nil {
		t.Fatal("NewTracker returned nil")
	}
	if len(tr.pricing) == 0 {
		t.Fatal("expected default pricing to be loaded")
	}
}

func TestRecordTurn_KnownModel(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("claude-sonnet", 1000, 500)

	turns := tr.Turns()
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	tc := turns[0]
	if tc.InputTokens != 1000 || tc.OutputTokens != 500 {
		t.Errorf("tokens: got %d/%d, want 1000/500", tc.InputTokens, tc.OutputTokens)
	}
	// claude-sonnet: $3/M in, $15/M out
	// 1000 * 3/1M + 500 * 15/1M = 0.003 + 0.0075 = 0.0105
	expected := 0.0105
	if math.Abs(tc.CostUSD-expected) > 1e-9 {
		t.Errorf("cost: got %f, want %f", tc.CostUSD, expected)
	}
}

func TestRecordTurn_FallbackPricing(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("unknown-model", 1_000_000, 1_000_000)

	// fallback: $1/M in, $3/M out = $1 + $3 = $4
	if math.Abs(tr.TotalCost()-4.0) > 1e-9 {
		t.Errorf("fallback cost: got %f, want 4.0", tr.TotalCost())
	}
}

func TestRecordTurn_PrefixMatch(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("claude-sonnet-4-20250514", 1_000_000, 0)

	// Should match "claude-sonnet" prefix: $3/M in
	if math.Abs(tr.TotalCost()-3.0) > 1e-9 {
		t.Errorf("prefix match cost: got %f, want 3.0", tr.TotalCost())
	}
}

func TestRecordTurn_FreeModel(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("qwen", 100_000, 50_000)

	if tr.TotalCost() != 0.0 {
		t.Errorf("qwen cost should be 0, got %f", tr.TotalCost())
	}
}

func TestTotalTokens(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("claude-haiku", 100, 50)
	tr.RecordTurn("claude-haiku", 200, 75)

	in, out := tr.TotalTokens()
	if in != 300 || out != 125 {
		t.Errorf("total tokens: got %d/%d, want 300/125", in, out)
	}
}

func TestTotalCost_MultipleTurns(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("claude-haiku", 1_000_000, 0)  // $0.25
	tr.RecordTurn("claude-haiku", 0, 1_000_000)   // $1.25
	expected := 1.50
	if math.Abs(tr.TotalCost()-expected) > 1e-9 {
		t.Errorf("total cost: got %f, want %f", tr.TotalCost(), expected)
	}
}

func TestSummary_Format(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("gpt-4", 1234, 567)

	s := tr.Summary()
	if !strings.Contains(s, "1 turns") {
		t.Errorf("summary should mention 1 turn: %q", s)
	}
	if !strings.Contains(s, "1,234 in") {
		t.Errorf("summary should format input tokens: %q", s)
	}
	if !strings.Contains(s, "567 out") {
		t.Errorf("summary should format output tokens: %q", s)
	}
	if !strings.Contains(s, "$") {
		t.Errorf("summary should include cost: %q", s)
	}
}

func TestSummary_Empty(t *testing.T) {
	tr := NewTracker()
	s := tr.Summary()
	if !strings.Contains(s, "0 turns") {
		t.Errorf("empty summary: %q", s)
	}
}

func TestSetPricing(t *testing.T) {
	tr := NewTracker()
	tr.SetPricing("custom-model", ModelPricing{
		InputPerMillion: 10.0, OutputPerMillion: 20.0,
	})
	tr.RecordTurn("custom-model", 1_000_000, 1_000_000)

	expected := 30.0
	if math.Abs(tr.TotalCost()-expected) > 1e-9 {
		t.Errorf("custom pricing cost: got %f, want %f", tr.TotalCost(), expected)
	}
}

func TestDeepseekPricing(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("deepseek", 1_000_000, 1_000_000)

	// $0.07 + $0.80 = $0.87
	expected := 0.87
	if math.Abs(tr.TotalCost()-expected) > 1e-9 {
		t.Errorf("deepseek cost: got %f, want %f", tr.TotalCost(), expected)
	}
}

func TestConcurrentRecordTurn(t *testing.T) {
	tr := NewTracker()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.RecordTurn("claude-haiku", 100, 50)
		}()
	}
	wg.Wait()

	turns := tr.Turns()
	if len(turns) != 100 {
		t.Errorf("expected 100 turns, got %d", len(turns))
	}
	in, out := tr.TotalTokens()
	if in != 10000 || out != 5000 {
		t.Errorf("concurrent totals: %d/%d, want 10000/5000", in, out)
	}
}

func TestFormatInt(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
	}
	for _, tt := range tests {
		got := formatInt(tt.input)
		if got != tt.want {
			t.Errorf("formatInt(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGPT54Pricing(t *testing.T) {
	tr := NewTracker()
	tr.RecordTurn("gpt-5.4", 1_000_000, 1_000_000)

	// $2 + $8 = $10
	expected := 10.0
	if math.Abs(tr.TotalCost()-expected) > 1e-9 {
		t.Errorf("gpt-5.4 cost: got %f, want %f", tr.TotalCost(), expected)
	}
}
