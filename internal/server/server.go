package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
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
	mux.HandleFunc("/plugins/configure", s.pluginConfigure)
	mux.HandleFunc("/plugins/save", s.pluginSave)
	mux.HandleFunc("/events", s.events)
	mux.HandleFunc("/actions", s.actions)
	mux.HandleFunc("/workflows", s.workflows)
	mux.HandleFunc("/workflows/edit", s.workflowEdit)
	mux.HandleFunc("/workflows/save", s.workflowSave)
	mux.HandleFunc("/workflows/toggle", s.toggleWorkflow)
	mux.HandleFunc("/logs", s.logs)
	mux.HandleFunc("/log-stream", s.logStream)
	mux.HandleFunc("/logs-sse", s.logsSSE)
	mux.HandleFunc("/events-sse", s.eventsSSE)
	mux.HandleFunc("/actions-sse", s.actionsSSE)
	mux.HandleFunc("/detail", s.detail)
	mux.HandleFunc("/whatsapp/qr", s.whatsappQR)
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

func (s *Server) pluginConfigure(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Redirect(w, r, "/plugins", http.StatusSeeOther)
		return
	}
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		s.render(w, "Configure Plugin", errorHTML(err))
		return
	}
	var plugin map[string]any
	for _, p := range status.ConfiguredPlugins {
		if fmt.Sprint(p["id"]) == id {
			plugin = p
			break
		}
	}
	if plugin == nil {
		s.render(w, "Configure Plugin", template.HTML(`<div class="p-6 bg-red-950 text-red-200 border border-red-900 rounded-2xl">Plugin not found</div>`))
		return
	}

	enabled := ""
	if plugin["enabled"] == true {
		enabled = "checked"
	}

	content := fmt.Sprintf(`<div class="mb-4"><a class="text-sky-400 hover:underline" href="/plugins">← Cancel</a></div>
<form method="post" action="/plugins/save" class="bg-zinc-900 border border-zinc-800 rounded-2xl p-6">
  <input type="hidden" name="id" value="%s">
  <div class="mb-6">
    <h3 class="text-xl font-semibold mb-2">Configure %s</h3>
    <p class="text-zinc-500 text-sm mb-4">Plugin ID: <span class="font-mono">%s</span></p>
    <label class="flex items-center gap-2 cursor-pointer">
      <input type="checkbox" name="enabled" value="true" %s class="w-5 h-5 rounded bg-zinc-950 border-zinc-800 text-emerald-500 focus:ring-emerald-500">
      <span class="font-medium">Enabled</span>
    </label>
  </div>
  <div class="mb-6">
    <label class="block font-medium mb-2">Environment Variables (JSON)</label>
    <p class="text-zinc-500 text-xs mb-2">Provide keys and values as a JSON object.</p>
    <textarea name="env_json" class="w-full h-48 bg-zinc-950 border border-zinc-800 rounded-xl p-4 font-mono text-sm" spellcheck="false">{}</textarea>
  </div>
  <button type="submit" class="px-6 py-2 bg-emerald-600 hover:bg-emerald-500 rounded-lg font-semibold text-white">Save & Restart Plugin</button>
</form>`, esc(id), esc(plugin["name"]), esc(id), enabled)

	s.render(w, "Configure Plugin", template.HTML(content))
}

func (s *Server) pluginSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.FormValue("id")
	enabled := r.FormValue("enabled") == "true"
	envJSON := r.FormValue("env_json")

	var env map[string]string
	if strings.TrimSpace(envJSON) != "" {
		if err := json.Unmarshal([]byte(envJSON), &env); err != nil {
			s.render(w, "Error", template.HTML(fmt.Sprintf(`<div class="p-6 bg-red-950 text-red-200 border border-red-900 rounded-2xl">Invalid Env JSON: %s<br><a href="javascript:history.back()" class="underline mt-4 inline-block">Go back</a></div>`, esc(err))))
			return
		}
	}

	payload := map[string]any{
		"id":      id,
		"enabled": enabled,
		"env":     env,
	}
	body, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.coreURL+"/api/plugins/configure", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var errData map[string]any
		json.NewDecoder(resp.Body).Decode(&errData)
		s.render(w, "Error", template.HTML(fmt.Sprintf(`<div class="p-6 bg-red-950 text-red-200 border border-red-900 rounded-2xl">Core rejected config: %s<br><a href="javascript:history.back()" class="underline mt-4 inline-block">Go back</a></div>`, esc(resp.Status))))
		return
	}

	http.Redirect(w, r, "/plugins", http.StatusSeeOther)
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
	s.render(w, "Workflows", s.workflowsTable(status.Workflows))
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	content := template.HTML(`<div class="flex items-center justify-between mb-4"><h2 class="text-2xl font-semibold">Logs</h2><span class="text-xs text-zinc-500">Live SSE stream with polling fallback</span></div><pre id="logs-output" class="bg-zinc-950 border border-zinc-800 rounded-2xl p-4 overflow-auto text-xs h-[70vh]" hx-get="/log-stream" hx-trigger="load" hx-swap="innerHTML">Loading logs...</pre><script>connectLogs('/logs-sse', 'logs-output');</script>`)
	s.render(w, "Logs", content)
}

func (s *Server) logStream(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.coreURL+"/api/logs", nil)
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
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(template.HTMLEscapeString(fmt.Sprint(payload["content"]))))
}

func (s *Server) eventsSSE(w http.ResponseWriter, r *http.Request) {
	s.proxySSE(w, r, "/api/events/stream")
}

func (s *Server) actionsSSE(w http.ResponseWriter, r *http.Request) {
	s.proxySSE(w, r, "/api/actions/stream")
}

func (s *Server) logsSSE(w http.ResponseWriter, r *http.Request) {
	s.proxySSE(w, r, "/api/logs/stream")
}

func (s *Server) proxySSE(w http.ResponseWriter, r *http.Request, path string) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.coreURL+path, nil)
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
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("type")
	id := r.URL.Query().Get("id")
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		s.render(w, "Detail", errorHTML(err))
		return
	}
	item, title := findDetail(status, kind, id)
	if item == nil {
		s.render(w, "Detail", template.HTML(`<div class="p-6 rounded-2xl border border-zinc-800 bg-zinc-900">Not found</div>`))
		return
	}
	s.render(w, title, detailCard(kind, item))
}

func (s *Server) whatsappQR(w http.ResponseWriter, r *http.Request) {
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	var qrCode string
	for i := len(status.Events) - 1; i >= 0; i-- {
		e := status.Events[i]
		if fmt.Sprint(e["type"]) == "whatsapp.qr" {
			payload, _ := e["payload"].(map[string]any)
			if code, ok := payload["code"].(string); ok {
				qrCode = code
				break
			}
		}
	}
	if qrCode == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="text-zinc-500 text-sm">No QR code pending</div>`))
		return
	}
	png, err := qrcode.Encode(qrCode, qrcode.Medium, 256)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	b64 := base64.StdEncoding.EncodeToString(png)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(fmt.Sprintf(`<img src="data:image/png;base64,%s" class="rounded-xl w-48 h-48 mx-auto">`, b64)))
}

func (s *Server) toggleWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	op := r.URL.Query().Get("op")
	if id == "" || (op != "enable" && op != "disable") {
		http.Error(w, "invalid params", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.coreURL+"/api/workflows/"+id+"/"+op, nil)
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
	http.Redirect(w, r, "/workflows", http.StatusSeeOther)
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
	b.WriteString(`<div class="flex items-center justify-between mb-4"><h2 class="text-2xl font-semibold">Plugins</h2><div class="flex gap-2"><input id="plugins-filter" type="text" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" placeholder="Filter..." onkeyup="filterDataRows('plugins-table')"><select id="plugins-running" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" onchange="filterDataRows('plugins-table')"><option value="">Any runtime</option><option value="true">Running</option><option value="false">Stopped</option></select></div></div><div class="bg-zinc-900 border border-zinc-800 rounded-2xl overflow-hidden" hx-get="/plugins" hx-trigger="every 5s" hx-swap="outerHTML"><table id="plugins-table" data-text-filter="plugins-filter" data-filters="running:plugins-running" class="w-full text-sm"><thead><tr class="bg-zinc-950"><th class="p-3 text-left">ID</th><th class="p-3 text-left">Name</th><th class="p-3 text-left">Kind</th><th class="p-3 text-left">Enabled</th><th class="p-3 text-left">Running</th><th class="p-3 text-left">Restarts</th><th class="p-3 text-left">Last error</th><th class="p-3 text-right">Actions</th></tr></thead><tbody>`)
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
		runningClass := "text-emerald-400"
		if !running {
			runningClass = "text-red-400"
		}
		extra := ""
		if id == "whatsapp" && running {
			extra = `<div class="mt-4" hx-get="/whatsapp/qr" hx-trigger="load, every 5s">Loading QR...</div>`
		}
		b.WriteString(fmt.Sprintf(`<tr data-running="%v" data-enabled="%v" class="border-t border-zinc-800"><td class="p-3"><a class="font-mono text-sky-400 hover:underline" href="/detail?type=plugin&id=%s">%s</a>%s</td><td class="p-3">%s</td><td class="p-3">%s</td><td class="p-3">%v</td><td class="p-3 %s">%v</td><td class="p-3">%d</td><td class="p-3 text-red-400">%s</td><td class="p-3 text-right"><a href="/plugins/configure?id=%s" class="text-sky-400 hover:underline">Configure</a></td></tr>`, running, p["enabled"], esc(id), esc(id), extra, esc(p["name"]), esc(p["kind"]), p["enabled"], runningClass, running, restarts, esc(lastError), esc(id)))
	}
	b.WriteString(`</tbody></table></div>`)
	return template.HTML(b.String())
}

func eventsList(events []map[string]any) template.HTML {
	var b bytes.Buffer
	b.WriteString(`<div class="flex items-center justify-between mb-4"><h2 class="text-2xl font-semibold">Events</h2><div class="flex gap-2"><input id="events-filter" type="text" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" placeholder="Filter..." onkeyup="filterDataRows('events-list')"><input id="events-type" type="text" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" placeholder="Type" onkeyup="filterDataRows('events-list')"><input id="events-source" type="text" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" placeholder="Source" onkeyup="filterDataRows('events-list')"></div></div><div id="events-list" data-text-filter="events-filter" data-filters="type:events-type,source:events-source" class="bg-zinc-900 border border-zinc-800 rounded-2xl divide-y divide-zinc-800" hx-get="/events" hx-trigger="load" hx-swap="outerHTML"></div><script>if(window.eventsSource) window.eventsSource.close(); window.eventsSource = connectListSSE('/events-sse', 'events-list', function(e) { return '<div data-type="'+e.type+'" data-source="'+e.source+'" class="p-4"><div class="text-xs text-zinc-500 font-mono">'+e.createdAt+'</div><div><span class="text-emerald-400">●</span> <a class="text-sky-400 hover:underline" href="/detail?type=event&id='+e.id+'">'+e.type+'</a> from '+e.source+'</div></div>'; });</script>`)
	return template.HTML(b.String())
}

func actionsTable(actions []map[string]any) template.HTML {
	var b bytes.Buffer
	b.WriteString(`<div class="flex items-center justify-between mb-4"><h2 class="text-2xl font-semibold">Actions</h2><div class="flex gap-2"><input id="actions-filter" type="text" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" placeholder="Filter..." onkeyup="filterDataRows('actions-table')"><select id="actions-status" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" onchange="filterDataRows('actions-table')"><option value="">Any status</option><option value="requested">Requested</option><option value="allowed">Allowed</option><option value="denied">Denied</option><option value="executed">Executed</option><option value="failed">Failed</option></select><select id="actions-risk" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" onchange="filterDataRows('actions-table')"><option value="">Any risk</option><option value="low">Low</option><option value="medium">Medium</option><option value="high">High</option></select></div></div><div class="bg-zinc-900 border border-zinc-800 rounded-2xl overflow-hidden"><table data-text-filter="actions-filter" data-filters="status:actions-status,risk:actions-risk" class="w-full text-sm"><thead><tr class="bg-zinc-950"><th class="p-3 text-left">ID</th><th class="p-3 text-left">Type</th><th class="p-3 text-left">Risk</th><th class="p-3 text-left">Status</th><th class="p-3 text-left">Source</th><th class="p-3 text-left">Controls</th></tr></thead><tbody id="actions-table" hx-get="/actions" hx-trigger="load" hx-swap="innerHTML"></tbody></table></div><script>if(window.actionsSource) window.actionsSource.close(); window.actionsSource = connectListSSE('/actions-sse', 'actions-table', function(a) { let controls = '—'; if (a.status === 'requested' || a.status === 'confirmed') controls = '<form class="inline" method="post" action="/actions/approve?id='+a.id+'"><button class="px-3 py-1 rounded bg-emerald-600 hover:bg-emerald-500">Approve</button></form> <form class="inline" method="post" action="/actions/reject?id='+a.id+'"><button class="px-3 py-1 rounded bg-red-700 hover:bg-red-600">Reject</button></form>'; return '<tr data-status="'+a.status+'" data-risk="'+a.risk+'" class="border-t border-zinc-800"><td class="p-3 font-mono text-xs"><a class="text-sky-400 hover:underline" href="/detail?type=action&id='+a.id+'">'+a.id+'</a></td><td class="p-3">'+a.type+'</td><td class="p-3">'+a.risk+'</td><td class="p-3">'+a.status+'</td><td class="p-3">'+a.source+'</td><td class="p-3">'+controls+'</td></tr>'; });</script>`)
	return template.HTML(b.String())
}

func (s *Server) workflowEdit(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	var workflow map[string]any
	if id != "" {
		status, err := s.fetchStatus(r.Context())
		if err != nil {
			s.render(w, "Edit Workflow", errorHTML(err))
			return
		}
		for _, wf := range status.Workflows {
			if fmt.Sprint(wf["id"]) == id {
				workflow = wf
				break
			}
		}
	} else {
		workflow = map[string]any{
			"id":      fmt.Sprintf("workflow-%d", time.Now().Unix()),
			"name":    "New Workflow",
			"enabled": true,
			"rules": []map[string]any{{
				"id":      "rule-1",
				"enabled": true,
				"trigger": map[string]any{
					"eventType": "message.received",
					"conditions": []map[string]any{{
						"field":    "payload.text",
						"operator": "equals",
						"value":    "hello",
					}},
				},
				"actions": []map[string]any{{
					"type": "message.reply",
					"risk": "low",
					"payload": map[string]any{
						"text": "Hello there, {{event.source}}!",
					},
				}},
			}},
		}
	}
	data, _ := json.MarshalIndent(workflow, "", "  ")
	content := fmt.Sprintf(`<div class="mb-4"><a class="text-sky-400 hover:underline" href="/workflows">← Cancel</a></div><form method="post" action="/workflows/save" onsubmit="try{JSON.parse(this.json.value);return true;}catch(e){alert('Invalid JSON: '+e.message);return false;}"><textarea name="json" class="w-full h-[60vh] bg-zinc-950 border border-zinc-800 rounded-2xl p-4 font-mono text-sm mb-4" spellcheck="false">%s</textarea><button type="submit" class="px-6 py-2 bg-emerald-600 hover:bg-emerald-500 rounded-lg font-semibold">Save Workflow</button></form>`, template.HTMLEscapeString(string(data)))
	s.render(w, "Edit Workflow", template.HTML(content))
}

func (s *Server) workflowSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonData := r.FormValue("json")
	var workflow map[string]any
	if err := json.Unmarshal([]byte(jsonData), &workflow); err != nil {
		s.render(w, "Error", template.HTML(fmt.Sprintf(`<div class="p-6 bg-red-950 text-red-200 border border-red-900 rounded-2xl">Invalid JSON: %s<br><a href="javascript:history.back()" class="underline mt-4 inline-block">Go back</a></div>`, esc(err))))
		return
	}

	// Fetch all workflows to append/replace
	status, err := s.fetchStatus(r.Context())
	if err != nil {
		s.render(w, "Error", errorHTML(err))
		return
	}

	workflows := status.Workflows
	id := fmt.Sprint(workflow["id"])
	found := false
	for i, wf := range workflows {
		if fmt.Sprint(wf["id"]) == id {
			workflows[i] = workflow
			found = true
			break
		}
	}
	if !found {
		workflows = append(workflows, workflow)
	}

	payload := map[string]any{"workflows": workflows}
	body, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.coreURL+"/api/workflows/reload", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var errData map[string]any
		json.NewDecoder(resp.Body).Decode(&errData)
		s.render(w, "Error", template.HTML(fmt.Sprintf(`<div class="p-6 bg-red-950 text-red-200 border border-red-900 rounded-2xl">Core rejected workflow: %s<br><a href="javascript:history.back()" class="underline mt-4 inline-block">Go back</a></div>`, esc(resp.Status))))
		return
	}

	http.Redirect(w, r, "/workflows", http.StatusSeeOther)
}

func (s *Server) workflowsTable(workflows []map[string]any) template.HTML {
	var b bytes.Buffer
	b.WriteString(`<div class="flex items-center justify-between mb-4"><h2 class="text-2xl font-semibold">Workflows</h2><div class="flex gap-2"><a href="/workflows/edit" class="px-4 py-2 bg-emerald-600 hover:bg-emerald-500 rounded-lg text-sm text-white font-medium">+ New Workflow</a><input id="workflows-filter" type="text" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" placeholder="Filter..." onkeyup="filterDataRows('workflows-table')"><select id="workflows-enabled" class="px-3 py-1 bg-zinc-800 rounded-lg text-sm" onchange="filterDataRows('workflows-table')"><option value="">Any state</option><option value="true">Enabled</option><option value="false">Disabled</option></select></div></div><div class="bg-zinc-900 border border-zinc-800 rounded-2xl overflow-hidden"><table id="workflows-table" data-text-filter="workflows-filter" data-filters="enabled:workflows-enabled" class="w-full text-sm"><thead><tr class="bg-zinc-950"><th class="p-3 text-left">ID</th><th class="p-3 text-left">Name</th><th class="p-3 text-left">Enabled</th><th class="p-3 text-left">Rules</th><th class="p-3 text-right">Actions</th></tr></thead><tbody>`)
	for _, wf := range workflows {
		rules, _ := wf["rules"].([]any)
		id := fmt.Sprint(wf["id"])
		b.WriteString(fmt.Sprintf(`<tr data-enabled="%v" class="border-t border-zinc-800"><td class="p-3 font-mono"><a class="text-sky-400 hover:underline" href="/detail?type=workflow&id=%s">%s</a></td><td class="p-3">%s</td><td class="p-3"><form class="inline" method="post" action="/workflows/toggle?id=%s&op=%s"><button class="px-2 py-1 rounded bg-zinc-800 hover:bg-zinc-700">%v</button></form></td><td class="p-3">%d</td><td class="p-3 text-right"><a href="/workflows/edit?id=%s" class="text-sky-400 hover:underline">Edit</a></td></tr>`, wf["enabled"], esc(id), esc(id), esc(wf["name"]), esc(id), toggleOp(wf["enabled"]), wf["enabled"], len(rules), esc(id)))
	}
	b.WriteString(`</tbody></table></div>`)
	return template.HTML(b.String())
}

func toggleOp(enabled any) string {
	if enabled == true || fmt.Sprint(enabled) == "true" {
		return "disable"
	}
	return "enable"
}

func findDetail(status Status, kind string, id string) (map[string]any, string) {
	switch kind {
	case "plugin":
		for _, item := range status.ConfiguredPlugins {
			if fmt.Sprint(item["id"]) == id {
				for _, runtimeStatus := range status.Plugins {
					if fmt.Sprint(runtimeStatus["id"]) == id {
						item["status"] = runtimeStatus
						break
					}
				}
				return item, "Plugin " + id
			}
		}
	case "event":
		for _, item := range status.Events {
			if fmt.Sprint(item["id"]) == id {
				return item, "Event " + id
			}
		}
	case "action":
		for _, item := range status.Actions {
			if fmt.Sprint(item["id"]) == id {
				return item, "Action " + id
			}
		}
	case "workflow":
		for _, item := range status.Workflows {
			if fmt.Sprint(item["id"]) == id {
				return item, "Workflow " + id
			}
		}
	}
	return nil, "Detail"
}

func detailCard(kind string, item map[string]any) template.HTML {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		data = []byte(fmt.Sprint(item))
	}
	back := "/"
	switch kind {
	case "plugin":
		back = "/plugins"
	case "event":
		back = "/events"
	case "action":
		back = "/actions"
	case "workflow":
		back = "/workflows"
	}
	var b bytes.Buffer
	b.WriteString(fmt.Sprintf(`<div class="mb-4"><a class="text-sky-400 hover:underline" href="%s">← Back</a></div><div class="grid grid-cols-1 lg:grid-cols-2 gap-6">`, back))
	b.WriteString(`<div class="bg-zinc-900 border border-zinc-800 rounded-2xl overflow-hidden"><div class="p-4 border-b border-zinc-800 font-semibold">Summary</div><dl class="divide-y divide-zinc-800">`)
	for _, key := range preferredDetailKeys(kind) {
		if value, ok := item[key]; ok {
			b.WriteString(fmt.Sprintf(`<div class="grid grid-cols-3 gap-4 p-4"><dt class="text-zinc-400">%s</dt><dd class="col-span-2 break-all">%s</dd></div>`, esc(key), esc(value)))
		}
	}
	b.WriteString(`</dl></div>`)
	b.WriteString(fmt.Sprintf(`<pre class="bg-zinc-950 border border-zinc-800 rounded-2xl p-4 overflow-auto text-xs">%s</pre></div>`, template.HTMLEscapeString(string(data))))
	return template.HTML(b.String())
}

func preferredDetailKeys(kind string) []string {
	switch kind {
	case "plugin":
		return []string{"id", "name", "kind", "enabled", "manifest", "status"}
	case "event":
		return []string{"id", "type", "source", "sessionId", "createdAt", "payload"}
	case "action":
		return []string{"id", "type", "risk", "status", "source", "sessionId", "createdAt", "payload"}
	case "workflow":
		return []string{"id", "name", "enabled", "rules"}
	default:
		return []string{"id", "type", "name", "status"}
	}
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
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><script src="https://cdn.tailwindcss.com"></script><script src="https://unpkg.com/htmx.org@1.9.12"></script><title>{{.Title}} - AuxiTalk</title>
<script>
function filterTable(input, tableId) {
  const filter = input.value.toLowerCase();
  const rows = document.getElementById(tableId).getElementsByTagName('tr');
  for (let i = 1; i < rows.length; i++) {
    const txt = rows[i].textContent.toLowerCase();
    rows[i].style.display = txt.indexOf(filter) > -1 ? '' : 'none';
  }
}
function filterCards(input, containerId) {
  const filter = input.value.toLowerCase();
  const cards = document.getElementById(containerId).children;
  for (let i = 0; i < cards.length; i++) {
    const txt = cards[i].textContent.toLowerCase();
    cards[i].style.display = txt.indexOf(filter) > -1 ? '' : 'none';
  }
}
function filterDataRows(containerId) {
  const container = document.getElementById(containerId);
  const textInput = document.getElementById(container.dataset.textFilter);
  const text = textInput ? textInput.value.toLowerCase() : '';
  const filterSpec = (container.dataset.filters || '').split(',').filter(Boolean).map((part) => part.split(':'));
  const rows = container.tagName === 'TABLE' ? container.tBodies[0].rows : container.children;
  for (let i = 0; i < rows.length; i++) {
    const row = rows[i];
    let visible = row.textContent.toLowerCase().indexOf(text) > -1;
    for (const [key, inputId] of filterSpec) {
      const input = document.getElementById(inputId);
      const value = input ? input.value.toLowerCase() : '';
      if (value && String(row.dataset[key] || '').toLowerCase().indexOf(value) === -1) visible = false;
    }
    row.style.display = visible ? '' : 'none';
  }
}
function connectLogs(url, targetId) {
  if (!window.EventSource) return;
  const target = document.getElementById(targetId);
  const source = new EventSource(url);
  source.onmessage = function(event) {
    try { target.textContent = JSON.parse(event.data); } catch (_) { target.textContent = event.data; }
    target.scrollTop = target.scrollHeight;
  };
  source.onerror = function() { source.close(); };
}
function connectListSSE(url, targetId, renderFn) {
  if (!window.EventSource) return;
  const target = document.getElementById(targetId);
  const source = new EventSource(url);
  source.onmessage = function(event) {
    try {
      const items = JSON.parse(event.data);
      if (Array.isArray(items)) {
        let html = '';
        for (let i = items.length - 1; i >= Math.max(0, items.length - 50); i--) {
          html += renderFn(items[i]);
        }
        target.innerHTML = html;
        filterDataRows(targetId);
      }
    } catch (_) {}
  };
  return source;
}
</script>
</head>
<body class="bg-zinc-950 text-zinc-100 min-h-screen"><div class="flex"><aside class="w-64 min-h-screen border-r border-zinc-800 bg-zinc-900 p-6"><div class="text-xl font-bold mb-8">AuxiTalk</div><nav class="space-y-2"><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/">Dashboard</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/plugins">Plugins</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/events">Events</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/actions">Actions</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/workflows">Workflows</a><a class="block px-3 py-2 rounded hover:bg-zinc-800" href="/logs">Logs</a></nav><div class="mt-8 text-xs text-zinc-500 break-all">Core: {{.CoreURL}}</div></aside><main class="flex-1 p-8"><div class="flex justify-between items-center mb-8"><h1 class="text-3xl font-semibold">{{.Title}}</h1><a class="px-4 py-2 rounded bg-zinc-800 hover:bg-zinc-700" href="{{.Title}}">Refresh</a></div>{{.Content}}</main></div></body></html>`))
