package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoute_Keywords(t *testing.T) {
	tests := []struct {
		input    string
		wantMode Mode
		wantRest string
	}{
		{"$interview add auth", ModeInterview, "add auth"},
		{"$deep-interview what is the plan", ModeInterview, "what is the plan"},
		{"clarify the requirements for auth", ModeInterview, "the requirements for auth"},
		{"$ralph implement the full feature", ModeRalph, "implement the full feature"},
		{"don't stop until the tests pass", ModeRalph, "until the tests pass"},
		{"$plan design the API", ModePlan, "design the API"},
		{"$team run in parallel the tests", ModeExecute, "run in parallel the tests"},
		{"just fix the bug", "", "just fix the bug"},
	}
	for _, tt := range tests {
		mode, rest := Route(tt.input)
		if mode != tt.wantMode {
			t.Errorf("Route(%q) mode = %q, want %q", tt.input, mode, tt.wantMode)
		}
		if rest != tt.wantRest {
			t.Errorf("Route(%q) rest = %q, want %q", tt.input, rest, tt.wantRest)
		}
	}
}

func TestRoute_CaseInsensitive(t *testing.T) {
	mode, _ := Route("$INTERVIEW add feature")
	if mode != ModeInterview {
		t.Errorf("expected interview mode for uppercase, got %q", mode)
	}
}

func TestRoute_NoMatch(t *testing.T) {
	mode, rest := Route("fix the login bug")
	if mode != "" {
		t.Errorf("expected empty mode, got %q", mode)
	}
	if rest != "fix the login bug" {
		t.Errorf("expected unchanged rest, got %q", rest)
	}
}

func TestSaveLoadState(t *testing.T) {
	dir := t.TempDir()
	st := &State{
		Mode:      ModeRalph,
		Phase:     PhaseActive,
		Iteration: 3,
		MaxIter:   10,
	}
	if err := SaveState(dir, st); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadState(dir, ModeRalph)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != PhaseActive {
		t.Errorf("phase = %q, want active", loaded.Phase)
	}
	if loaded.Iteration != 3 {
		t.Errorf("iteration = %d, want 3", loaded.Iteration)
	}
}

func TestClearState(t *testing.T) {
	dir := t.TempDir()
	st := &State{Mode: ModeInterview, Phase: PhaseActive}
	SaveState(dir, st)

	ClearState(dir, ModeInterview)

	_, err := LoadState(dir, ModeInterview)
	if err == nil {
		t.Error("expected error after clear")
	}
}

func TestClearAll(t *testing.T) {
	dir := t.TempDir()
	if err := SaveState(dir, &State{Mode: ModeInterview, Phase: PhaseActive}); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(dir, &State{Mode: ModePlan, Phase: PhaseActive}); err != nil {
		t.Fatal(err)
	}
	stateDir := StateDir(dir)
	if err := os.WriteFile(filepath.Join(stateDir, "note.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ClearAll(dir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "note.txt" {
		t.Fatalf("unexpected remaining entries: %+v", entries)
	}
}

func TestClearAll_MissingStateDir(t *testing.T) {
	dir := t.TempDir()
	if err := ClearAll(dir); err != nil {
		t.Fatal(err)
	}
}

func TestListActive(t *testing.T) {
	dir := t.TempDir()
	SaveState(dir, &State{Mode: ModeRalph, Phase: PhaseActive})
	SaveState(dir, &State{Mode: ModePlan, Phase: PhaseComplete})
	SaveState(dir, &State{Mode: ModeInterview, Phase: PhaseActive})

	active := ListActive(dir)
	if len(active) != 2 {
		t.Errorf("expected 2 active, got %d", len(active))
	}
}

func TestStatusText_Empty(t *testing.T) {
	dir := t.TempDir()
	text := StatusText(dir)
	if text != "No active workflows." {
		t.Errorf("unexpected: %q", text)
	}
}

func TestStateDir(t *testing.T) {
	got := StateDir("/home/user/project")
	want := filepath.Join("/home/user/project", ".altcode", "state")
	if got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
}

func TestInterviewPrompt(t *testing.T) {
	p := InterviewPrompt("add auth")
	if !contains(p, "add auth") {
		t.Error("prompt should contain task")
	}
	if !contains(p, "ambiguity") {
		t.Error("prompt should mention ambiguity")
	}
}

func TestRalphPrompt(t *testing.T) {
	p := RalphPrompt("fix tests", 2, 5)
	if !contains(p, "fix tests") {
		t.Error("prompt should contain task")
	}
	if !contains(p, "2 of 5") {
		t.Error("prompt should contain iteration info")
	}
}

func TestPlanPrompt(t *testing.T) {
	p := PlanPrompt("design API")
	if !contains(p, "design API") {
		t.Error("prompt should contain task")
	}
	if !contains(p, "Do NOT execute") {
		t.Error("prompt should warn against execution")
	}
}

func TestSaveStateCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "nested")
	st := &State{Mode: ModeRalph, Phase: PhasePending}
	if err := SaveState(dir, st); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(StateDir(dir)); err != nil {
		t.Error("state dir not created")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
