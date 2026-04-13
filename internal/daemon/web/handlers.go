package web

import (
	"net/http"
	"strings"
	"time"
)

// DashboardStore abstracts the data needed by dashboard handlers.
// Task 8 wires the real daemon.Store implementation.
type DashboardStore interface {
	ListTasks() ([]*TaskView, error)
}

// TaskView is the read-model for a single task displayed on the
// dashboard, task-list partials, and detail page.
type TaskView struct {
	ID              string
	RepoURL         string
	TaskDescription string
	Status          string
	RepoOwner       string
	RepoName        string
	IssueNumber     int
	APICostUSD      float64
	PRNumber        int
	PRURL           string
	Duration        string
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

// phaseBarData carries data for the phase_bar partial.
type phaseBarData struct {
	Phases       []string
	CurrentPhase string
}

// detailContentData carries all fields for detail.html rendering.
type detailContentData struct {
	Task      *TaskView
	IsActive  bool
	PhaseData phaseBarData
	Events    []*EventView
	CSRFToken string
}

// defaultPhases is the ordered pipeline for the phase bar.
var defaultPhases = []string{
	"planning", "implementing", "reviewing", "testing", "pr_open",
}

// HandleTaskDetail renders the task detail page.
func (h *WebHandler) HandleTaskDetail(
	w http.ResponseWriter, r *http.Request,
) {
	taskID := r.PathValue("id")
	sess := GetSession(r)

	es := h.eventStore()
	if es == nil {
		http.Error(w, "store not configured", http.StatusInternalServerError)
		return
	}

	task, err := es.GetTask(taskID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	events, _ := es.ListEvents(taskID, 0)

	active := activeStatuses[task.Status]
	csrf := ""
	if sess != nil {
		csrf = sess.CSRFToken
	}

	content := detailContentData{
		Task:     task,
		IsActive: active,
		PhaseData: phaseBarData{
			Phases:       defaultPhases,
			CurrentPhase: task.Status,
		},
		Events:    events,
		CSRFToken: csrf,
	}

	data := PageData{
		Title:   "Task Detail",
		ShowNav: true,
	}
	if sess != nil {
		data.CSRFToken = sess.CSRFToken
		data.User = sess.User
		if sess.User != nil {
			data.IsAdmin = sess.User.IsAdmin
		}
	}
	data.Content = content

	if err := Render(w, h.tmpl, "detail", data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- New Task page ---

// RepoStore abstracts repository listing for the new-task form.
// Task 8 wires the real implementation.
type RepoStore interface {
	ListRepos() ([]string, error)
}

// IssueView is the read-model for a GitHub issue shown in the
// repo-issue dropdown.
type IssueView struct {
	Number int
	Title  string
}

// IssueStore abstracts issue listing per repo.
// Task 8 wires the real implementation.
type IssueStore interface {
	ListIssues(repo string) ([]*IssueView, error)
}

// newTaskData carries fields for new.html rendering.
type newTaskData struct {
	Repos []string
}

// repoIssuesData carries fields for the repo_issues partial.
type repoIssuesData struct {
	Issues []*IssueView
}

// HandleNewTask renders the new task creation form.
func (h *WebHandler) HandleNewTask(
	w http.ResponseWriter, r *http.Request,
) {
	sess := GetSession(r)

	var repos []string
	if rs, ok := h.store.(RepoStore); ok {
		repos, _ = rs.ListRepos()
	}

	data := PageData{
		Title:   "New Task",
		ShowNav: true,
	}
	if sess != nil {
		data.CSRFToken = sess.CSRFToken
		data.User = sess.User
		if sess.User != nil {
			data.IsAdmin = sess.User.IsAdmin
		}
	}
	data.Content = newTaskData{Repos: repos}

	if err := Render(w, h.tmpl, "new", data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// HandlePartialRepoIssues returns the repo_issues partial for the
// selected repository. Called via htmx when the repo dropdown changes.
func (h *WebHandler) HandlePartialRepoIssues(
	w http.ResponseWriter, r *http.Request,
) {
	repo := r.URL.Query().Get("repo")

	var issues []*IssueView
	if repo != "" {
		if is, ok := h.store.(IssueStore); ok {
			issues, _ = is.ListIssues(repo)
		}
	}

	rd := repoIssuesData{Issues: issues}
	err := RenderPartial(w, h.tmpl, "new", "repo_issues", rd)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- PR Tracker page ---

// PRView is the read-model for a PR row in the tracker table.
type PRView struct {
	RepoOwner       string
	RepoName        string
	PRNumber        int
	PRURL           string
	TaskDescription string
	Status          string
	APICostUSD      float64
	CreatedAt       time.Time
}

// prPageData carries fields for prs.html rendering.
type prPageData struct {
	PRs          []*PRView
	StatusFilter string
}

// prStatusMatch returns true if a PR's status matches the filter.
func prStatusMatch(status, filter string) bool {
	if filter == "" {
		return true
	}
	return strings.EqualFold(status, filter)
}

// extractPRs builds PRView slices from tasks that have a PR URL.
func extractPRs(
	tasks []*TaskView, filter string,
) []*PRView {
	var prs []*PRView
	for _, t := range tasks {
		if t.PRURL == "" {
			continue
		}
		if !prStatusMatch(t.Status, filter) {
			continue
		}
		prs = append(prs, &PRView{
			RepoOwner:       t.RepoOwner,
			RepoName:        t.RepoName,
			PRNumber:        t.PRNumber,
			PRURL:           t.PRURL,
			TaskDescription: t.TaskDescription,
			Status:          t.Status,
			APICostUSD:      t.APICostUSD,
			CreatedAt:       t.CreatedAt,
		})
	}
	return prs
}

// HandlePRs renders the PR tracker page.
func (h *WebHandler) HandlePRs(
	w http.ResponseWriter, r *http.Request,
) {
	sess := GetSession(r)
	tasks := h.loadTasks()
	statusFilter := r.URL.Query().Get("status")
	prs := extractPRs(tasks, statusFilter)

	data := PageData{
		Title:   "PR Tracker",
		ShowNav: true,
	}
	if sess != nil {
		data.CSRFToken = sess.CSRFToken
		data.User = sess.User
		if sess.User != nil {
			data.IsAdmin = sess.User.IsAdmin
		}
	}
	data.Content = prPageData{
		PRs:          prs,
		StatusFilter: statusFilter,
	}

	if err := Render(w, h.tmpl, "prs", data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// HandlePartialPRList renders the pr_list partial for htmx polling.
func (h *WebHandler) HandlePartialPRList(
	w http.ResponseWriter, r *http.Request,
) {
	tasks := h.loadTasks()
	status := r.URL.Query().Get("status")
	prs := extractPRs(tasks, status)

	pd := prPageData{PRs: prs, StatusFilter: status}
	err := RenderPartial(w, h.tmpl, "prs", "pr_list", pd)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- Settings page ---

// settingsData carries all fields for settings.html rendering.
type settingsData struct {
	GitHubLogin   string
	AvatarURL     string
	Orgs          []string
	DailyCap      float64
	PerTaskCap    float64
	MaxConcurrent int
	DefaultModel  string
	AllowedUsers  []string
	AllowedOrgs   []string
}

// HandleSettings renders the settings page. Non-admins see a
// read-only view; admin status is conveyed via PageData.IsAdmin.
func (h *WebHandler) HandleSettings(
	w http.ResponseWriter, r *http.Request,
) {
	sess := GetSession(r)

	sd := settingsData{
		AllowedUsers: h.cfg.AllowedUsers,
		AllowedOrgs:  h.cfg.AllowedOrgs,
	}
	if sess != nil && sess.User != nil {
		sd.GitHubLogin = sess.User.Login
		sd.AvatarURL = sess.User.AvatarURL
		sd.Orgs = sess.User.Orgs
	}

	data := PageData{
		Title:   "Settings",
		ShowNav: true,
	}
	if sess != nil {
		data.CSRFToken = sess.CSRFToken
		data.User = sess.User
		if sess.User != nil {
			data.IsAdmin = sess.User.IsAdmin
		}
	}
	data.Content = sd

	if err := Render(w, h.tmpl, "settings", data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
