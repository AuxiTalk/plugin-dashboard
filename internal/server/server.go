package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"
)

type Server struct {
	httpServer *http.Server
	coreURL    string
}

type Status struct {
	Plugins           []map[string]any `json:"plugins"`
	ConfiguredPlugins []map[string]any `json:"configuredPlugins"`
	Events            []map[string]any `json:"events"`
	Actions           []map[string]any `json:"actions"`
	PendingActions    []map[string]any `json:"pendingActions"`
	Workflows         []map[string]any `json:"workflows"`
}

func NewServer(port string) *Server {
	coreURL := os.Getenv("AUXITALK_CORE_URL")
	if coreURL == "" {
		coreURL = "http://127.0.0.1:8090"
	}

	s := &Server{coreURL: strings.TrimRight(coreURL, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/plugins", s.plugins)
	mux.HandleFunc("/events", s.events)
	mux.HandleFunc("/actions", s.actions)
	mux.HandleFunc("/workflows", s.workflows)
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/actions/approve", s.approve)
	mux.HandleFunc("/actions/reject", s.reject)

	s.httpServer = &http.Server{Addr: ":" + port, Handler: mux}
	return s
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true,"service":"auxitalk-dashboard"}`))
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		s.render(w, "Dashboard", errorHTML(err))
		return
	}
	content := fmt.Sprintf(`
<div class="grid grid-cols-1 md:grid-cols-4 gap-4 mb-8">
  %s
  %s
  %s
  %s
</div>
<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
  <div>%s</div>
  <div>%s</div>
</div>`, statCard("Plugins", len(status.Plugins)), statCard("Events", len(status.Events)), statCard("Actions", len(status.Actions)), statCard("Workflows", len(status.Workflows)), pluginsTable(status.ConfiguredPlugins, status.Plugins), actionsTable(status.Actions))
	s.render(w, "Dashboard", template.HTML(content))
}

func (s *Server) plugins(w http.ResponseWriter, r *http.Request) {
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		s.render(w, "Plugins", errorHTML(err))
		return
	}
	s.render(w, "Plugins", pluginsTable(status.ConfiguredPlugins, status.Plugins))
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		s.render(w, "Events", errorHTML(err))
		return
	}
	s.render(w, "Events", eventsList(status.Events))
}

func (s *Server) actions(w http.ResponseWriter, r *http.Request) {
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		s.render(w, "Actions", errorHTML(err))
		return
	}
	s.render(w, "Actions", actionsTable(status.Actions))
}

func (s *Server) workflows(w http.ResponseWriter, r *http.Request) {
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		s.render(w, "Workflows", errorHTML(err))
		return
	}
	s.render(w, "Workflows", workflowsTable(status.Workflows))
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	s.mutateAction(w, r, "approve")
}

func (s *Server) reject(w http.ResponseWriter, r *http.Request) {
	s.mutateAction(w, r, "deny")
}

func (s *Server) mutateAction(w http.ResponseWriter, r *http.Request, operation string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.coreURL+"/api/actions/"+id+"/"+operation, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		http.Error(w, resp.Status, resp.StatusCode)
		return
	}
	http.Redirect(w, r, "/actions", http.StatusSeeOther)
}

func (s *Server) fetchStatus(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.coreURL+"/api/status", nil)
	if err != nil {
		return Status{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("core status returned %s", resp.Status)
	}
	var status Status
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return Status{}, err
	}
	return status, nil
}

func (s *Server) render(w http.ResponseWriter, title string, content template.HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := map[string]any{"Title": title, "Content": content, "CoreURL": s.coreURL}
	_ = pageTemplate.Execute(w, data)
}

func statCard(label string, value int) template.HTML {
	return template.HTML(fmt.Sprintf(`<div class="rounded-2xl border border-zinc-800 bg-zinc-900 p-6"><div class="text-sm text-zinc-400">%s</div><div class="text-3xl font-bold mt-2">%d</div></div>`, template.HTMLEscapeString(label), value))
}

func pluginsTable(configured []map[string]any, statuses []map[string]any) template.HTML {
	statusByID := map[string]map[string]any{}
	for _, status := range statuses {
		statusByID[fmt.Sprint(status["id"])] = status
	}
	var b bytes.Buffer
	b.WriteString(`<h2 class="text-2xl font-semibold mb-4">Plugins</h2><div class="bg-zinc-900 border border-zinc-800 rounded-2xl overflow-hidden"><table class="w-full text-sm"><thead><tr class="bg-zinc-950"><th class="p-3 text-left">ID</th><th class="p-3 text-left">Name</th><th class="p-3 text-left">Kind</th><th class="p-3 text-left">Enabled</th><th class="p-3 text-left">Running</th><th class="p-3 text-left">Restarts</th><th class="p-3 text-left">Last error</th></tr></thead><tbody>`)
	for _, p := range configured {
		id := fmt.Sprint(p["id"])
		status := statusByID[id]
		running := false
		restarts := 0
		lastError := ""
		if status != nil {
			running = fmt.Sprint(status["running"]) == "true"
			restarts = intFromAny(status["restarts"])
			lastError = fmt.Sprint(status["lastError"])
		}
		b.WriteString(fmt.Sprintf(`<tr class="border-t border-zinc-800"><td class="p-3 font-mono">%s</td><td class="p-3">%s</td><td class="p-3">%s</td><td class="p-3">%v</td><td class="p-3">%v</td><td class="p-3">%d</td><td class="p-3 text-red-400">%s</td></tr>`, esc(id), esc(p["name"]), esc(p["kind"]), p["enabled"], running, restarts, esc(lastError)))
	}
	b.WriteString(`</tbody></table></div>`)
	return template.HTML(b.String())
}

func eventsList(events []map[string]any) template.HTML {
	var b bytes.Buffer
	b.WriteString(`<h2 class="text-2xl font-semibold mb-4">Events</h2><div class="bg-zinc-900 border border-zinc-800 rounded-2xl divide-y divide-zinc-800">`)
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		b.WriteString(fmt.Sprintf(`<div class="p-4"><div class="text-xs text-zinc-500 font-mono">%s</div><div><span class="text-emerald-400">●</span> %s from %s</div></div>`, esc(e["createdAt"]), esc(e["type"]), esc(e["source"])))
	}
	b.WriteString(`</div>`)
	return template.HTML(b.String())
}

func actionsTable(actions []map[string]any) template.HTML {
	var b bytes.Buffer
	b.WriteString(`<h2 class="text-2xl font-semibold mb-4">Actions</h2><div class="bg-zinc-900 border border-zinc-800 rounded-2xl overflow-hidden"><table class="w-full text-sm"><thead><tr class="bg-zinc-950"><th class="p-3 text-left">ID</th><th class="p-3 text-left">Type</th><th class="p-3 text-left">Risk</th><th class="p-3 text-left">Status</th><th class="p-3 text-left">Source</th><th class="p-3 text-left">Controls</th></tr></thead><tbody>`)
	for _, a := range actions {
		id := esc(a["id"])
		controls := "—"
		status := fmt.Sprint(a["status"])
		if status == "requested" || status == "confirmed" {
			controls = fmt.Sprintf(`<form class="inline" method="post" action="/actions/approve?id=%s"><button class="px-3 py-1 rounded bg-emerald-600 hover:bg-emerald-500">Approve</button></form> <form class="inline" method="post" action="/actions/reject?id=%s"><button class="px-3 py-1 rounded bg-red-700 hover:bg-red-600">Reject</button></form>`, id, id)
		}
		b.WriteString(fmt.Sprintf(`<tr class="border-t border-zinc-800"><td class="p-3 font-mono text-xs">%s</td><td class="p-3">%s</td><td class="p-3">%s</td><td class="p-3">%s</td><td class="p-3">%s</td><td class="p-3">%s</td></tr>`, id, esc(a["type"]), esc(a["risk"]), esc(a["status"]), esc(a["source"]), controls))
	}
	b.WriteString(`</tbody></table></div>`)
	return template.HTML(b.String())
}

func workflowsTable(workflows []map[string]any) template.HTML {
	var b bytes.Buffer
	b.WriteString(`<h2 class="text-2xl font-semibold mb-4">Workflows</h2><div class="bg-zinc-900 border border-zinc-800 rounded-2xl overflow-hidden"><table class="w-full text-sm"><thead><tr class="bg-zinc-950"><th class="p-3 text-left">ID</th><th class="p-3 text-left">Name</th><th class="p-3 text-left">Enabled</th><th class="p-3 text-left">Rules</th></tr></thead><tbody>`)
	for _, wf := range workflows {
		rules, _ := wf["rules"].([]any)
		b.WriteString(fmt.Sprintf(`<tr class="border-t border-zinc-800"><td class="p-3 font-mono">%s</td><td class="p-3">%s</td><td class="p-3">%v</td><td class="p-3">%d</td></tr>`, esc(wf["id"]), esc(wf["name"]), wf["enabled"], len(rules)))
	}
	b.WriteString(`</tbody></table></div>`)
	return template.HTML(b.String())
}

func errorHTML(err error) template.HTML {
	return template.HTML(fmt.Sprintf(`<div class="p-6 rounded-2xl border border-red-900 bg-red-950 text-red-200">Core unavailable: %s</div>`, template.HTMLEscapeString(err.Error())))
}

func esc(v any) string {
	return template.HTMLEscapeString(fmt.Sprint(v))
}

func intFromAny(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><script src="https://cdn.tailwindcss.com"></script><title>{{.Title}} - AuxiTalk</title></head>
<body class="bg-zinc-950 text-zinc-100 min-h-screen"><div class="flex"><aside class="w-64 min-h-screen border-r border-zinc-800 bg-zinc-900 p-6"><div class="text-xl font-bold mb-8">AuxiTalk</div><nav class="space-y-2"><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/">Dashboard</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/plugins">Plugins</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/events">Events</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/actions">Actions</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/workflows">Workflows</a></nav><div class="mt-8 text-xs text-zinc-500 break-all">Core: {{.CoreURL}}</div></aside><main class="flex-1 p-8"><div class="flex justify-between items-center mb-8"><h1 class="text-3xl font-semibold">{{.Title}}</h1><a class="px-4 py-2 rounded bg-zinc-800 hover:bg-zinc-700" href="{{.Title}}">Refresh</a></div>{{.Content}}</main></div></body></html>`))
