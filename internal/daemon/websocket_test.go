package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebSocket_Returns501(t *testing.T) {
	s := testServer(t)

	ts := httptest.NewServer(s.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ws/some-task-id")
	if err != nil {
		t.Fatalf("GET /ws: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d",
			resp.StatusCode, http.StatusNotImplemented)
	}
}

func TestWebSocket_RouteRegistered(t *testing.T) {
	s := testServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws/task-123", nil)
	s.mux.ServeHTTP(rec, req)

	// A registered route returns 501 (our stub).
	// An unregistered route would return 404 or 405.
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("route not registered: status = %d, want %d",
			rec.Code, http.StatusNotImplemented)
	}
}
