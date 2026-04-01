package tool_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/altcode-ai/altcode/internal/tool"
)

type mockTool struct {
	name       string
	concurrent bool
	readOnly   bool
	callCount  atomic.Int32
}

func (m *mockTool) Name() string                                  { return m.name }
func (m *mockTool) Description() string                           { return "mock tool" }
func (m *mockTool) Parameters() json.RawMessage                   { return json.RawMessage(`{"type":"object"}`) }
func (m *mockTool) IsConcurrencySafe() bool                       { return m.concurrent }
func (m *mockTool) IsReadOnly() bool                              { return m.readOnly }
func (m *mockTool) PermissionPattern(_ json.RawMessage) string    { return m.name + ":*" }
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	m.callCount.Add(1)
	return &tool.Result{Output: "ok", Title: m.name}, nil
}

func TestPartitionByConcurrency(t *testing.T) {
	read1 := &mockTool{name: "read1", concurrent: true}
	read2 := &mockTool{name: "read2", concurrent: true}
	write1 := &mockTool{name: "write1", concurrent: false}
	read3 := &mockTool{name: "read3", concurrent: true}

	calls := []tool.Call{
		{ID: "1", Tool: read1},
		{ID: "2", Tool: read2},
		{ID: "3", Tool: write1},
		{ID: "4", Tool: read3},
	}

	batches := tool.PartitionByConcurrency(calls)
	if len(batches) != 3 {
		t.Fatalf("Expected 3 batches, got %d", len(batches))
	}
	if len(batches[0]) != 2 {
		t.Fatalf("Expected first batch size 2, got %d", len(batches[0]))
	}
	if len(batches[1]) != 1 {
		t.Fatalf("Expected second batch size 1, got %d", len(batches[1]))
	}
	if len(batches[2]) != 1 {
		t.Fatalf("Expected third batch size 1, got %d", len(batches[2]))
	}
}

func TestDispatchConcurrent(t *testing.T) {
	read1 := &mockTool{name: "read1", concurrent: true}
	read2 := &mockTool{name: "read2", concurrent: true}

	calls := []tool.Call{
		{ID: "1", Tool: read1, Input: json.RawMessage(`{}`)},
		{ID: "2", Tool: read2, Input: json.RawMessage(`{}`)},
	}

	results := tool.Dispatch(context.Background(), calls)
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}
	if read1.callCount.Load() != 1 {
		t.Fatal("read1 should be called once")
	}
	if read2.callCount.Load() != 1 {
		t.Fatal("read2 should be called once")
	}
}
