package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestDashboard(t *testing.T, core http.Handler) (*Server, func()) {
	t.Helper()
	coreServer := httptest.NewServer(core)
	t.Setenv("AUXITALK_CORE_URL", coreServer.URL)
	dashboard := NewServer("0")
	return dashboard, coreServer.Close
}

func fakeStatus() Status {
	return Status{
		Plugins:           []map[string]any{{"id": "openai", "running": true, "restarts": 0}},
		ConfiguredPlugins: []map[string]any{{"id": "openai", "name": "OpenAI", "kind": "ai", "enabled": true}},
		Events:            []map[string]any{{"id": "event-1", "type": "message.received", "source": "test", "createdAt": "now"}},
		Actions:           []map[string]any{{"id": "action-1", "type": "test.action", "risk": "low", "status": "requested", "source": "test"}},
		Workflows:         []map[string]any{{"id": "workflow-1", "name": "Workflow", "enabled": true, "rules": []any{map[string]any{"id": "rule-1"}}}},
	}
}

func TestDashboardHealth(t *testing.T) {
	dashboard, closeCore := newTestDashboard(t, http.NewServeMux())
	defer closeCore()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	dashboard.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auxitalk-dashboard") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestDashboardIndexRendersCoreStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fakeStatus())
	})
	dashboard, closeCore := newTestDashboard(t, mux)
	defer closeCore()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	dashboard.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Dashboard", "OpenAI", "Actions", "Workflows"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q, got %s", want, body)
		}
	}
}

func TestDashboardApproveProxiesToCore(t *testing.T) {
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/actions/action-1/approve", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	})
	dashboard, closeCore := newTestDashboard(t, mux)
	defer closeCore()

	req := httptest.NewRequest(http.MethodPost, "/actions/approve?id=action-1", nil)
	rec := httptest.NewRecorder()
	dashboard.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if !called {
		t.Fatal("expected core approve endpoint to be called")
	}
}

func TestDashboardWorkflowSaveReloadsCore(t *testing.T) {
	reloaded := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Status{Workflows: []map[string]any{}})
	})
	mux.HandleFunc("/api/workflows/reload", func(w http.ResponseWriter, r *http.Request) {
		reloaded = true
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode reload payload: %v", err)
		}
		if _, ok := payload["workflows"]; !ok {
			t.Fatalf("expected workflows payload: %+v", payload)
		}
		w.WriteHeader(http.StatusOK)
	})
	dashboard, closeCore := newTestDashboard(t, mux)
	defer closeCore()

	form := strings.NewReader(`json={"id":"workflow-1","enabled":true,"rules":[{"id":"rule-1","enabled":true,"trigger":{"eventType":"test"},"actions":[{"type":"emit_event","risk":"low"}]}]}`)
	req := httptest.NewRequest(http.MethodPost, "/workflows/save", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	dashboard.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !reloaded {
		t.Fatal("expected workflow reload endpoint to be called")
	}
}
