package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstrumentRecordsPatternStatusAndBytes(t *testing.T) {
	registry := NewRegistry()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
	})
	handler := Instrument(registry, mux)
	request := httptest.NewRequest(http.MethodGet, "/tasks/2d650703-1c00-4f6f-b3f3-572376a431f4", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d", response.Code)
	}
	snapshot := registry.Snapshot()
	if len(snapshot.Routes) != 1 {
		t.Fatalf("routes=%d", len(snapshot.Routes))
	}
	route := snapshot.Routes[0]
	if route.Route != "/tasks/{id}" || route.ResponseBytes != 8 || route.StatusCounts[202] != 1 {
		t.Fatalf("route=%+v", route)
	}
}

func TestInstrumentRecordsPanicsAfterRecoveryMiddleware(t *testing.T) {
	registry := NewRegistry()
	recovered := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer func() {
			if recover() != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		panic("boom")
	})
	response := httptest.NewRecorder()
	Instrument(registry, recovered).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if registry.Snapshot().Errors != 1 {
		t.Fatalf("snapshot=%+v", registry.Snapshot())
	}
}

func TestNormalizeRoute(t *testing.T) {
	cases := map[string]string{
		"":                               "/",
		"api/v1/tasks/123456789":         "/api/v1/tasks/{id}",
		"/api/v1/tasks/abc?verbose=true": "/api/v1/tasks/abc",
		"/tasks/2d650703-1c00-4f6f-b3f3-572376a431f4/": "/tasks/{id}",
		"/api/v1/tasks/{id}":                           "/api/v1/tasks/{id}",
	}
	for input, expected := range cases {
		if actual := NormalizeRoute(input); actual != expected {
			t.Errorf("NormalizeRoute(%q)=%q want %q", input, actual, expected)
		}
	}
}

func TestTelemetryHandlers(t *testing.T) {
	registry := NewRegistry()
	registry.ObserveHTTP(HTTPObservation{Method: "GET", Route: "/healthz", Status: 200})

	jsonResponse := httptest.NewRecorder()
	JSONHandler(registry).ServeHTTP(jsonResponse, httptest.NewRequest(http.MethodGet, "/metrics.json", nil))
	if jsonResponse.Code != http.StatusOK || !strings.Contains(jsonResponse.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("json response=%v", jsonResponse.Result())
	}
	var snapshot map[string]any
	if err := json.Unmarshal(jsonResponse.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["requests"] != float64(1) {
		t.Fatalf("snapshot=%v", snapshot)
	}

	metricsResponse := httptest.NewRecorder()
	PrometheusHandler(registry).ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsResponse.Code != http.StatusOK || !strings.Contains(metricsResponse.Body.String(), "filepipeline_http_requests_total") {
		t.Fatalf("metrics=%s", metricsResponse.Body.String())
	}
}
