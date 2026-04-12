package daemon

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSE_StreamsEvents(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "test",
		Status:          "implementing",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	for i, ev := range []struct{ typ, data string }{
		{"status_change", `{"from":"pending","to":"planning"}`},
		{"agent_output", `{"line":"cloning repo"}`},
		{"status_change", `{"from":"planning","to":"implementing"}`},
	} {
		if err := s.store.AppendEvent(task.ID, ev.typ, ev.data); err != nil {
			t.Fatalf("append event %d: %v", i, err)
		}
	}

	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Get(ts.URL + "/tasks/" + task.ID + "/sse")
	if err != nil {
		t.Fatalf("GET sse: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	var events []string
	deadline := time.After(5 * time.Second)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				events = append(events, line)
				if len(events) >= 3 {
					return
				}
			}
		}
	}()

	select {
	case <-done:
	case <-deadline:
		t.Fatal("timed out waiting for 3 events")
	}

	if len(events) < 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	want := []string{
		"event: status_change",
		"event: agent_output",
		"event: status_change",
	}
	for i, w := range want {
		if events[i] != w {
			t.Errorf("event[%d] = %q, want %q", i, events[i], w)
		}
	}
}

func TestSSE_ReplayFromLastEventID(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "test",
		Status:          "implementing",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	for i := 0; i < 5; i++ {
		data := fmt.Sprintf(`{"step":%d}`, i+1)
		if err := s.store.AppendEvent(task.ID, "progress", data); err != nil {
			t.Fatalf("append event %d: %v", i, err)
		}
	}

	allEvents, err := s.store.ListEvents(task.ID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(allEvents) != 5 {
		t.Fatalf("got %d events, want 5", len(allEvents))
	}
	replayAfter := allEvents[2].ID

	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	req, err := http.NewRequest(
		"GET", ts.URL+"/tasks/"+task.ID+"/sse", nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Last-Event-ID", fmt.Sprintf("%d", replayAfter))

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET sse: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var ids []string
	deadline := time.After(5 * time.Second)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "id: ") {
				ids = append(ids, strings.TrimPrefix(line, "id: "))
				if len(ids) >= 2 {
					return
				}
			}
		}
	}()

	select {
	case <-done:
	case <-deadline:
		t.Fatal("timed out waiting for replayed events")
	}

	if len(ids) < 2 {
		t.Fatalf("got %d event IDs, want 2", len(ids))
	}
	expect4 := fmt.Sprintf("%d", allEvents[3].ID)
	expect5 := fmt.Sprintf("%d", allEvents[4].ID)
	if ids[0] != expect4 {
		t.Errorf("first replayed id = %s, want %s", ids[0], expect4)
	}
	if ids[1] != expect5 {
		t.Errorf("second replayed id = %s, want %s", ids[1], expect5)
	}
}

func TestSSE_StopsOnTerminalStatus(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "test",
		Status:          "merged",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Get(ts.URL + "/tasks/" + task.ID + "/sse")
	if err != nil {
		t.Fatalf("GET sse: %v", err)
	}
	defer resp.Body.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			// Drain all output until connection closes.
		}
	}()

	select {
	case <-done:
		// Connection closed as expected.
	case <-time.After(5 * time.Second):
		t.Fatal("SSE did not close for terminal task within timeout")
	}
}

func TestSSE_Heartbeat(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "test",
		Status:          "implementing",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Get(ts.URL + "/tasks/" + task.ID + "/sse")
	if err != nil {
		t.Fatalf("GET sse: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	deadline := time.After(5 * time.Second)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), ": heartbeat") {
				return
			}
		}
	}()

	select {
	case <-done:
		// Heartbeat received.
	case <-deadline:
		t.Fatal("no heartbeat received within 5 seconds")
	}
}

func TestSSE_TaskNotFound(t *testing.T) {
	s := testServer(t)

	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(ts.URL + "/tasks/nonexistent/sse")
	if err != nil {
		t.Fatalf("GET sse: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}
