package web

import (
	"net/http"
	"time"
)

// DashboardStore abstracts the data needed by dashboard handlers.
// Task 8 wires the real daemon.Store implementation.
type DashboardStore interface {
	ListTasks() ([]*TaskView, error)
}

// TaskView is the read-model for a single task displayed on the
// dashboard and task-list partials.
type TaskView struct {
	ID              string
	RepoURL         string
	TaskDescription string
	Status          string
	RepoOwner       string
	RepoName        string
	IssueNumber     int
	APICostUSD      float64
	CreatedAt       time.Time
}

// dashboardData carries all fields that dashboard.html and its
// partials reference via .Content.
type dashboardData struct {
	ActiveCount  int
	QueueDepth   int
	Cost24h      float64
	SuccessRate  float64
	Tasks        []*TaskView
	StatusFilter string
}

// kpiData carries the four KPI card values.
type kpiData struct {
	ActiveCount int
	QueueDepth  int
	Cost24h     float64
	SuccessRate float64
}

// taskListData carries the filtered task slice.
type taskListData struct {
	Tasks []*TaskView
}

// activeStatuses lists statuses considered "active".
var activeStatuses = map[string]bool{
	"pending":        true,
	"planning":       true,
	"implementing":   true,
	"reviewing":      true,
	"testing":        true,
	"pr_open":        true,
}

// completedStatuses lists statuses considered "completed".
var completedStatuses = map[string]bool{
	"merged": true,
	"closed": true,
}

// failedStatuses lists statuses considered "failed".
var failedStatuses = map[string]bool{
	"failed":    true,
	"cancelled": true,
}

// dashStore returns the DashboardStore from h.store, if wired.
// Returns nil when the store is not yet a DashboardStore (pre-Task 8).
func (h *WebHandler) dashStore() DashboardStore {
	ds, _ := h.store.(DashboardStore)
	return ds
}

// loadTasks loads all tasks from the store, returning an empty
// slice if the store is not yet wired.
func (h *WebHandler) loadTasks() []*TaskView {
	ds := h.dashStore()
	if ds == nil {
		return nil
	}
	tasks, err := ds.ListTasks()
	if err != nil {
		return nil
	}
	return tasks
}

// filterTasks returns tasks matching the given status filter.
func filterTasks(
	tasks []*TaskView, filter string,
) []*TaskView {
	if filter == "" {
		return tasks
	}
	var set map[string]bool
	switch filter {
	case "active":
		set = activeStatuses
	case "completed":
		set = completedStatuses
	case "failed":
		set = failedStatuses
	default:
		return tasks
	}
	out := make([]*TaskView, 0, len(tasks))
	for _, t := range tasks {
		if set[t.Status] {
			out = append(out, t)
		}
	}
	return out
}

// computeKPI derives the four KPI values from a task slice.
func computeKPI(tasks []*TaskView) kpiData {
	var active, queued, completed, failed int
	var cost float64
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, t := range tasks {
		if activeStatuses[t.Status] {
			active++
		}
		if t.Status == "pending" {
			queued++
		}
		if completedStatuses[t.Status] {
			completed++
		}
		if failedStatuses[t.Status] {
			failed++
		}
		if t.CreatedAt.After(cutoff) {
			cost += t.APICostUSD
		}
	}
	var rate float64
	denom := completed + failed
	if denom > 0 {
		rate = float64(completed) / float64(denom) * 100
	}
	return kpiData{
		ActiveCount: active,
		QueueDepth:  queued,
		Cost24h:     cost,
		SuccessRate: rate,
	}
}

// HandleDashboard renders the full dashboard page.
func (h *WebHandler) HandleDashboard(
	w http.ResponseWriter, r *http.Request,
) {
	sess := GetSession(r)
	tasks := h.loadTasks()
	statusFilter := r.URL.Query().Get("status")
	kpi := computeKPI(tasks)
	filtered := filterTasks(tasks, statusFilter)

	data := PageData{
		Title:   "Dashboard",
		ShowNav: true,
	}
	if sess != nil {
		data.CSRFToken = sess.CSRFToken
		data.User = sess.User
		if sess.User != nil {
			data.IsAdmin = sess.User.IsAdmin
		}
	}
	data.Content = dashboardData{
		ActiveCount:  kpi.ActiveCount,
		QueueDepth:   kpi.QueueDepth,
		Cost24h:      kpi.Cost24h,
		SuccessRate:  kpi.SuccessRate,
		Tasks:        filtered,
		StatusFilter: statusFilter,
	}
	if err := Render(w, h.tmpl, "dashboard", data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// HandlePartialTaskList renders just the task-list partial for
// htmx polling. Accepts an optional ?status= query parameter.
func (h *WebHandler) HandlePartialTaskList(
	w http.ResponseWriter, r *http.Request,
) {
	tasks := h.loadTasks()
	status := r.URL.Query().Get("status")
	filtered := filterTasks(tasks, status)

	td := taskListData{Tasks: filtered}
	err := RenderPartial(w, h.tmpl, "dashboard", "task_list", td)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// HandlePartialKPICards renders just the kpi-cards partial for
// htmx polling.
func (h *WebHandler) HandlePartialKPICards(
	w http.ResponseWriter, r *http.Request,
) {
	tasks := h.loadTasks()
	kpi := computeKPI(tasks)
	err := RenderPartial(w, h.tmpl, "dashboard", "kpi_cards", kpi)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
