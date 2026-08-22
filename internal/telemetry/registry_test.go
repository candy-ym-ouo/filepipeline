package telemetry

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRegistryAggregatesRoutesAndStatuses(t *testing.T) {
	registry := NewRegistryWithBounds([]time.Duration{time.Second, 10 * time.Millisecond, time.Second})
	registry.ObserveHTTP(HTTPObservation{Method: "get", Route: "/api/v1/tasks/123456789", Status: 200, Duration: 5 * time.Millisecond, Bytes: 12})
	registry.ObserveHTTP(HTTPObservation{Method: "GET", Route: "/api/v1/tasks/987654321", Status: 503, Duration: 20 * time.Millisecond, Bytes: 8})
	snapshot := registry.Snapshot()
	if snapshot.Requests != 2 || snapshot.Errors != 1 || snapshot.ResponseBytes != 20 {
		t.Fatalf("unexpected totals: %+v", snapshot)
	}
	if len(snapshot.Routes) != 1 {
		t.Fatalf("routes=%d", len(snapshot.Routes))
	}
	route := snapshot.Routes[0]
	if route.Route != "/api/v1/tasks/{id}" || route.Requests != 2 || route.Errors != 1 {
		t.Fatalf("unexpected route: %+v", route)
	}
	if route.StatusCounts[200] != 1 || route.StatusCounts[503] != 1 {
		t.Fatalf("statuses=%v", route.StatusCounts)
	}
	if len(route.DurationBounds) != 2 || len(route.DurationCounts) != 3 {
		t.Fatalf("histogram bounds=%v counts=%v", route.DurationBounds, route.DurationCounts)
	}
}

func TestRegistryConcurrentUse(t *testing.T) {
	registry := NewRegistry()
	const goroutines = 20
	const observations = 100
	var group sync.WaitGroup
	for worker := 0; worker < goroutines; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < observations; i++ {
				finish := registry.Begin()
				registry.ObserveHTTP(HTTPObservation{Method: "POST", Route: "/api/v1/files", Status: 201, Bytes: 10})
				finish()
				finish()
			}
		}()
	}
	group.Wait()
	snapshot := registry.Snapshot()
	if snapshot.Requests != goroutines*observations || snapshot.Inflight != 0 {
		t.Fatalf("requests=%d inflight=%d", snapshot.Requests, snapshot.Inflight)
	}
	if snapshot.ResponseBytes != goroutines*observations*10 {
		t.Fatalf("bytes=%d", snapshot.ResponseBytes)
	}
}

func TestSnapshotJSONUsesReadableDurations(t *testing.T) {
	registry := NewRegistryWithBounds([]time.Duration{25 * time.Millisecond})
	registry.ObserveHTTP(HTTPObservation{Method: "GET", Route: "/healthz", Status: 200, Duration: 12 * time.Millisecond})
	data, err := registry.Snapshot().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	routes := value["routes"].([]any)
	route := routes[0].(map[string]any)
	if route["duration_total"] != "12ms" || route["duration_max"] != "12ms" {
		t.Fatalf("route=%v", route)
	}
}

func TestPrometheusOutputIsStableAndCumulative(t *testing.T) {
	registry := NewRegistryWithBounds([]time.Duration{10 * time.Millisecond, 100 * time.Millisecond})
	registry.ObserveHTTP(HTTPObservation{Method: "GET", Route: "/healthz", Status: 200, Duration: 5 * time.Millisecond, Bytes: 3})
	registry.ObserveHTTP(HTTPObservation{Method: "GET", Route: "/healthz", Status: 500, Duration: 50 * time.Millisecond, Bytes: 7})
	var output strings.Builder
	if err := registry.Snapshot().WritePrometheus(&output); err != nil {
		t.Fatal(err)
	}
	checks := []string{
		`filepipeline_http_requests_total{method="GET",route="/healthz"} 2`,
		`filepipeline_http_response_bytes_total{method="GET",route="/healthz"} 10`,
		`filepipeline_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.010000"} 1`,
		`filepipeline_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.100000"} 2`,
		`filepipeline_http_responses_total{method="GET",route="/healthz",status="500"} 1`,
	}
	for _, check := range checks {
		if !strings.Contains(output.String(), check) {
			t.Errorf("missing %q in:\n%s", check, output.String())
		}
	}
}
