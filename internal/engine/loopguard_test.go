package engine

import (
	"strings"
	"testing"
)

func TestLoopGuard_SoftCapTripsAfterRepeatedCalls(t *testing.T) {
	g := NewLoopGuard()
	input := []byte(`{"file_path":"/tmp/x"}`)

	// First 3 calls should pass (< softCap).
	for i := 1; i <= 3; i++ {
		looped, _ := g.Check("read", input)
		if looped {
			t.Fatalf("call %d: tripped early at softCap", i)
		}
	}
	// 4th call should trip.
	looped, msg := g.Check("read", input)
	if !looped {
		t.Errorf("4th identical call should trip the guard")
	}
	if !strings.Contains(msg, "Loop guard") {
		t.Errorf("guard message missing marker: %q", msg)
	}
}

func TestLoopGuard_DistinctInputsDoNotShareCount(t *testing.T) {
	g := NewLoopGuard()
	for i := 0; i < 5; i++ {
		// Different input each time.
		input := []byte(`{"file_path":"/tmp/file` + string(rune('a'+i)) + `"}`)
		looped, _ := g.Check("read", input)
		if looped {
			t.Errorf("distinct inputs at i=%d falsely tripped", i)
		}
	}
}

func TestLoopGuard_DistinctToolsDoNotShareCount(t *testing.T) {
	g := NewLoopGuard()
	input := []byte(`{"x":1}`)
	for _, tool := range []string{"read", "write", "edit", "bash", "grep"} {
		for i := 0; i < 3; i++ {
			looped, _ := g.Check(tool, input)
			if looped {
				t.Errorf("tool=%s i=%d tripped while still under softCap", tool, i)
			}
		}
	}
}

func TestLoopGuard_HardCapHaltsAfterConsecutiveErrors(t *testing.T) {
	g := NewLoopGuard()

	// First 7 errors don't halt (hardCap=8).
	for i := 0; i < 7; i++ {
		g.RecordResult(true)
	}
	if halt, _ := g.AgentShouldHalt(); halt {
		t.Errorf("halted at %d errors, want >= 8", 7)
	}

	// 8th error should halt.
	g.RecordResult(true)
	halt, msg := g.AgentShouldHalt()
	if !halt {
		t.Errorf("8 consecutive errors should halt")
	}
	if !strings.Contains(msg, "consecutive tool errors") {
		t.Errorf("halt message missing marker: %q", msg)
	}
}

func TestLoopGuard_SuccessResetsErrorCounter(t *testing.T) {
	g := NewLoopGuard()
	for i := 0; i < 7; i++ {
		g.RecordResult(true)
	}
	g.RecordResult(false) // single success
	for i := 0; i < 7; i++ {
		g.RecordResult(true)
	}
	if halt, _ := g.AgentShouldHalt(); halt {
		t.Errorf("halt after success reset is wrong (only 7 errors since reset)")
	}
}

func TestLoopGuard_ResetClearsAll(t *testing.T) {
	g := NewLoopGuard()
	for i := 0; i < 5; i++ {
		g.Check("read", []byte("x"))
	}
	for i := 0; i < 8; i++ {
		g.RecordResult(true)
	}

	g.Reset()

	if looped, _ := g.Check("read", []byte("x")); looped {
		t.Error("Reset should have cleared call counts")
	}
	if halt, _ := g.AgentShouldHalt(); halt {
		t.Error("Reset should have cleared error counter")
	}
}

func TestLoopGuard_NilSafe(t *testing.T) {
	var g *LoopGuard // nil
	// Should not panic.
	if looped, _ := g.Check("read", []byte("x")); looped {
		t.Error("nil guard should never trip")
	}
	g.RecordResult(true)
	if halt, _ := g.AgentShouldHalt(); halt {
		t.Error("nil guard should never halt")
	}
	g.Reset() // no panic
}

func TestLoopGuard_ConcurrentSafe(t *testing.T) {
	g := NewLoopGuard()
	const n = 100
	done := make(chan struct{}, n*2)
	for i := 0; i < n; i++ {
		go func(i int) {
			g.Check("bash", []byte(string(rune(i))))
			done <- struct{}{}
		}(i)
		go func() {
			g.RecordResult(false)
			done <- struct{}{}
		}()
	}
	for i := 0; i < n*2; i++ {
		<-done
	}
}
