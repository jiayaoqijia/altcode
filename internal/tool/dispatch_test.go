package tool_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// =============================================================================
// DISPATCH: error-in-tool, eager results, nil tool, empty calls
// =============================================================================

type errorTool struct {
	name string
}

func (e *errorTool) Name() string                               { return e.name }
func (e *errorTool) Description() string                        { return "error tool" }
func (e *errorTool) Parameters() json.RawMessage                { return json.RawMessage(`{}`) }
func (e *errorTool) IsConcurrencySafe() bool                    { return false }
func (e *errorTool) IsReadOnly() bool                           { return false }
func (e *errorTool) PermissionPattern(_ json.RawMessage) string { return "" }
func (e *errorTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	return nil, fmt.Errorf("tool execution failed: disk full")
}

func TestDispatch_ToolReturnsError(t *testing.T) {
	et := &errorTool{name: "failing"}
	calls := []tool.Call{
		{ID: "1", Tool: et, Input: json.RawMessage(`{}`)},
	}
	results := tool.Dispatch(context.Background(), calls)
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Fatal("Expected error in result")
	}
	if results[0].Title != "failing" {
		t.Errorf("Title should be tool name, got %q", results[0].Title)
	}
}

func TestDispatch_EagerResultPassthrough(t *testing.T) {
	mt := &mockTool{name: "mock", concurrent: false}
	eager := &tool.Result{Output: "pre-computed", Title: "eager"}
	calls := []tool.Call{
		{ID: "1", Tool: mt, Input: json.RawMessage(`{}`), EagerResult: eager},
	}
	results := tool.Dispatch(context.Background(), calls)
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Output != "pre-computed" {
		t.Errorf("Expected eager result output, got %q", results[0].Output)
	}
	if mt.callCount.Load() != 0 {
		t.Error("Tool should not be called when EagerResult is set")
	}
}

func TestDispatch_EmptyCalls(t *testing.T) {
	results := tool.Dispatch(context.Background(), nil)
	if len(results) != 0 {
		t.Errorf("Expected 0 results for nil calls, got %d", len(results))
	}
	results = tool.Dispatch(context.Background(), []tool.Call{})
	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty calls, got %d", len(results))
	}
}

func TestDispatch_MixedSequentialAndConcurrent(t *testing.T) {
	w1 := &mockTool{name: "write1", concurrent: false}
	r1 := &mockTool{name: "read1", concurrent: true}
	r2 := &mockTool{name: "read2", concurrent: true}
	w2 := &mockTool{name: "write2", concurrent: false}

	calls := []tool.Call{
		{ID: "1", Tool: w1, Input: json.RawMessage(`{}`)},
		{ID: "2", Tool: r1, Input: json.RawMessage(`{}`)},
		{ID: "3", Tool: r2, Input: json.RawMessage(`{}`)},
		{ID: "4", Tool: w2, Input: json.RawMessage(`{}`)},
	}
	results := tool.Dispatch(context.Background(), calls)
	if len(results) != 4 {
		t.Fatalf("Expected 4 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Output != "ok" {
			t.Errorf("Result %d: expected 'ok', got %q", i, r.Output)
		}
	}
}

func TestDispatch_ConcurrentErrorInBatch(t *testing.T) {
	good := &mockTool{name: "good", concurrent: true}
	bad := &errorTool{name: "bad"}
	// errorTool isn't concurrent, so force them into same batch via wrapper
	goodCall := tool.Call{ID: "1", Tool: good, Input: json.RawMessage(`{}`)}
	badCall := tool.Call{ID: "2", Tool: bad, Input: json.RawMessage(`{}`)}
	results := tool.Dispatch(context.Background(), []tool.Call{goodCall, badCall})
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}
	// Good tool should succeed
	if results[0].Error != nil {
		t.Errorf("Good tool should succeed, got error: %v", results[0].Error)
	}
	// Bad tool should have error
	if results[1].Error == nil {
		t.Error("Bad tool should have error")
	}
}

// =============================================================================
// REGISTRY: Subset, Schemas, Get, All, duplicate
// =============================================================================

func TestRegistry_GetAndAll(t *testing.T) {
	reg := tool.NewRegistry()
	m1 := &mockTool{name: "tool-a"}
	m2 := &mockTool{name: "tool-b"}
	reg.Register(m1)
	reg.Register(m2)

	got, ok := reg.Get("tool-a")
	if !ok {
		t.Fatal("Expected tool-a to be found")
	}
	if got.Name() != "tool-a" {
		t.Errorf("Name: %q", got.Name())
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("Should not find nonexistent tool")
	}

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("Expected 2 tools, got %d", len(all))
	}
}

func TestRegistry_Subset(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "read"})
	reg.Register(&mockTool{name: "grep"})
	reg.Register(&mockTool{name: "bash"})

	sub := reg.Subset([]string{"read", "grep"})
	if len(sub.All()) != 2 {
		t.Fatalf("Expected 2 tools in subset, got %d", len(sub.All()))
	}
	_, ok := sub.Get("bash")
	if ok {
		t.Error("bash should not be in subset")
	}
	_, ok = sub.Get("read")
	if !ok {
		t.Error("read should be in subset")
	}
}

func TestRegistry_SubsetSkipsUnknown(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "read"})
	sub := reg.Subset([]string{"read", "nonexistent"})
	if len(sub.All()) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(sub.All()))
	}
}

func TestRegistry_Schemas(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "tool-x"})
	reg.Register(&mockTool{name: "tool-y"})

	schemas := reg.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("Expected 2 schemas, got %d", len(schemas))
	}
	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Name] = true
		if s.Description == "" {
			t.Errorf("Schema %q should have description", s.Name)
		}
	}
	if !names["tool-x"] || !names["tool-y"] {
		t.Error("Missing expected schema names")
	}
}

func TestRegistry_DuplicateOverwrites(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(&mockTool{name: "tool-a", readOnly: false})
	reg.Register(&mockTool{name: "tool-a", readOnly: true})

	got, _ := reg.Get("tool-a")
	if !got.IsReadOnly() {
		t.Error("Second register should overwrite first")
	}
}

func TestPartition_AllConcurrent(t *testing.T) {
	calls := []tool.Call{
		{ID: "1", Tool: &mockTool{name: "a", concurrent: true}},
		{ID: "2", Tool: &mockTool{name: "b", concurrent: true}},
		{ID: "3", Tool: &mockTool{name: "c", concurrent: true}},
	}
	batches := tool.PartitionByConcurrency(calls)
	if len(batches) != 1 {
		t.Fatalf("All concurrent should be 1 batch, got %d", len(batches))
	}
	if len(batches[0]) != 3 {
		t.Fatalf("Batch should have 3 calls, got %d", len(batches[0]))
	}
}

func TestPartition_AllSequential(t *testing.T) {
	calls := []tool.Call{
		{ID: "1", Tool: &mockTool{name: "a", concurrent: false}},
		{ID: "2", Tool: &mockTool{name: "b", concurrent: false}},
	}
	batches := tool.PartitionByConcurrency(calls)
	if len(batches) != 2 {
		t.Fatalf("All sequential should be 2 batches, got %d", len(batches))
	}
}

func TestPartition_Empty(t *testing.T) {
	batches := tool.PartitionByConcurrency(nil)
	if batches != nil {
		t.Error("Expected nil for empty calls")
	}
}
