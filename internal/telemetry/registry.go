package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var defaultLatencyBounds = []time.Duration{
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	5 * time.Second,
}

type HTTPObservation struct {
	Method   string
	Route    string
	Status   int
	Duration time.Duration
	Bytes    int64
}

type RouteSnapshot struct {
	Method         string          `json:"method"`
	Route          string          `json:"route"`
	Requests       uint64          `json:"requests"`
	Errors         uint64          `json:"errors"`
	ResponseBytes  uint64          `json:"response_bytes"`
	DurationTotal  time.Duration   `json:"duration_total"`
	DurationMax    time.Duration   `json:"duration_max"`
	DurationBounds []time.Duration `json:"duration_bounds"`
	DurationCounts []uint64        `json:"duration_counts"`
	StatusCounts   map[int]uint64  `json:"status_counts"`
}

type Snapshot struct {
	StartedAt     time.Time       `json:"started_at"`
	CapturedAt    time.Time       `json:"captured_at"`
	Uptime        time.Duration   `json:"uptime"`
	Inflight      int64           `json:"inflight"`
	Requests      uint64          `json:"requests"`
	Errors        uint64          `json:"errors"`
	ResponseBytes uint64          `json:"response_bytes"`
	Routes        []RouteSnapshot `json:"routes"`
}

type routeKey struct {
	method string
	route  string
}

type routeMetrics struct {
	requests      uint64
	errors        uint64
	responseBytes uint64
	durationTotal time.Duration
	durationMax   time.Duration
	durationCount []uint64
	statusCount   map[int]uint64
}

type Registry struct {
	mu            sync.RWMutex
	startedAt     time.Time
	now           func() time.Time
	latencyBounds []time.Duration
	inflight      int64
	requests      uint64
	errors        uint64
	responseBytes uint64
	routes        map[routeKey]*routeMetrics
}

func NewRegistry() *Registry {
	return NewRegistryWithBounds(defaultLatencyBounds)
}

func NewRegistryWithBounds(bounds []time.Duration) *Registry {
	clean := normalizeBounds(bounds)
	return &Registry{
		startedAt:     time.Now().UTC(),
		now:           time.Now,
		latencyBounds: clean,
		routes:        make(map[routeKey]*routeMetrics),
	}
}

func normalizeBounds(bounds []time.Duration) []time.Duration {
	clean := append([]time.Duration(nil), bounds...)
	sort.Slice(clean, func(i, j int) bool { return clean[i] < clean[j] })
	result := clean[:0]
	for _, bound := range clean {
		if bound <= 0 || len(result) > 0 && result[len(result)-1] == bound {
			continue
		}
		result = append(result, bound)
	}
	return append([]time.Duration(nil), result...)
}

func (r *Registry) Begin() func() {
	if r == nil {
		return func() {}
	}
	r.mu.Lock()
	r.inflight++
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			if r.inflight > 0 {
				r.inflight--
			}
			r.mu.Unlock()
		})
	}
}

func (r *Registry) ObserveHTTP(observation HTTPObservation) {
	if r == nil {
		return
	}
	method := strings.ToUpper(strings.TrimSpace(observation.Method))
	if method == "" {
		method = "UNKNOWN"
	}
	route := NormalizeRoute(observation.Route)
	status := observation.Status
	if status < 100 || status > 599 {
		status = 500
	}
	duration := observation.Duration
	if duration < 0 {
		duration = 0
	}
	bytes := observation.Bytes
	if bytes < 0 {
		bytes = 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests++
	r.responseBytes += uint64(bytes)
	if status >= 500 {
		r.errors++
	}
	key := routeKey{method: method, route: route}
	metrics := r.routes[key]
	if metrics == nil {
		metrics = &routeMetrics{
			durationCount: make([]uint64, len(r.latencyBounds)+1),
			statusCount:   make(map[int]uint64),
		}
		r.routes[key] = metrics
	}
	metrics.requests++
	metrics.responseBytes += uint64(bytes)
	metrics.durationTotal += duration
	if duration > metrics.durationMax {
		metrics.durationMax = duration
	}
	if status >= 500 {
		metrics.errors++
	}
	metrics.statusCount[status]++
	bucket := sort.Search(len(r.latencyBounds), func(i int) bool {
		return duration <= r.latencyBounds[i]
	})
	metrics.durationCount[bucket]++
}

func (r *Registry) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.now().UTC()
	snapshot := Snapshot{
		StartedAt:     r.startedAt,
		CapturedAt:    now,
		Uptime:        now.Sub(r.startedAt),
		Inflight:      r.inflight,
		Requests:      r.requests,
		Errors:        r.errors,
		ResponseBytes: r.responseBytes,
		Routes:        make([]RouteSnapshot, 0, len(r.routes)),
	}
	for key, metrics := range r.routes {
		statuses := make(map[int]uint64, len(metrics.statusCount))
		for status, count := range metrics.statusCount {
			statuses[status] = count
		}
		snapshot.Routes = append(snapshot.Routes, RouteSnapshot{
			Method:         key.method,
			Route:          key.route,
			Requests:       metrics.requests,
			Errors:         metrics.errors,
			ResponseBytes:  metrics.responseBytes,
			DurationTotal:  metrics.durationTotal,
			DurationMax:    metrics.durationMax,
			DurationBounds: append([]time.Duration(nil), r.latencyBounds...),
			DurationCounts: append([]uint64(nil), metrics.durationCount...),
			StatusCounts:   statuses,
		})
	}
	sort.Slice(snapshot.Routes, func(i, j int) bool {
		if snapshot.Routes[i].Route == snapshot.Routes[j].Route {
			return snapshot.Routes[i].Method < snapshot.Routes[j].Method
		}
		return snapshot.Routes[i].Route < snapshot.Routes[j].Route
	})
	return snapshot
}

func (s Snapshot) MarshalJSON() ([]byte, error) {
	type routeJSON struct {
		Method         string         `json:"method"`
		Route          string         `json:"route"`
		Requests       uint64         `json:"requests"`
		Errors         uint64         `json:"errors"`
		ResponseBytes  uint64         `json:"response_bytes"`
		DurationTotal  string         `json:"duration_total"`
		DurationMax    string         `json:"duration_max"`
		DurationBounds []string       `json:"duration_bounds"`
		DurationCounts []uint64       `json:"duration_counts"`
		StatusCounts   map[int]uint64 `json:"status_counts"`
	}
	type snapshotJSON struct {
		StartedAt     time.Time   `json:"started_at"`
		CapturedAt    time.Time   `json:"captured_at"`
		Uptime        string      `json:"uptime"`
		Inflight      int64       `json:"inflight"`
		Requests      uint64      `json:"requests"`
		Errors        uint64      `json:"errors"`
		ResponseBytes uint64      `json:"response_bytes"`
		Routes        []routeJSON `json:"routes"`
	}
	value := snapshotJSON{
		StartedAt: s.StartedAt, CapturedAt: s.CapturedAt, Uptime: s.Uptime.String(),
		Inflight: s.Inflight, Requests: s.Requests, Errors: s.Errors,
		ResponseBytes: s.ResponseBytes, Routes: make([]routeJSON, 0, len(s.Routes)),
	}
	for _, route := range s.Routes {
		bounds := make([]string, len(route.DurationBounds))
		for i, bound := range route.DurationBounds {
			bounds[i] = bound.String()
		}
		value.Routes = append(value.Routes, routeJSON{
			Method: route.Method, Route: route.Route, Requests: route.Requests,
			Errors: route.Errors, ResponseBytes: route.ResponseBytes,
			DurationTotal: route.DurationTotal.String(), DurationMax: route.DurationMax.String(),
			DurationBounds: bounds, DurationCounts: route.DurationCounts,
			StatusCounts: route.StatusCounts,
		})
	}
	return json.Marshal(value)
}

func (s Snapshot) WritePrometheus(w io.Writer) error {
	lines := []string{
		"# HELP filepipeline_uptime_seconds Process uptime in seconds.",
		"# TYPE filepipeline_uptime_seconds gauge",
		fmt.Sprintf("filepipeline_uptime_seconds %s", formatSeconds(s.Uptime)),
		"# HELP filepipeline_http_inflight_requests Current in-flight HTTP requests.",
		"# TYPE filepipeline_http_inflight_requests gauge",
		fmt.Sprintf("filepipeline_http_inflight_requests %d", s.Inflight),
		"# HELP filepipeline_http_requests_total Completed HTTP requests.",
		"# TYPE filepipeline_http_requests_total counter",
	}
	for _, route := range s.Routes {
		labels := fmt.Sprintf("method=%q,route=%q", escapeLabel(route.Method), escapeLabel(route.Route))
		lines = append(lines, fmt.Sprintf("filepipeline_http_requests_total{%s} %d", labels, route.Requests))
	}
	lines = append(lines,
		"# HELP filepipeline_http_response_bytes_total HTTP response bytes.",
		"# TYPE filepipeline_http_response_bytes_total counter",
	)
	for _, route := range s.Routes {
		labels := fmt.Sprintf("method=%q,route=%q", escapeLabel(route.Method), escapeLabel(route.Route))
		lines = append(lines, fmt.Sprintf("filepipeline_http_response_bytes_total{%s} %d", labels, route.ResponseBytes))
	}
	lines = append(lines,
		"# HELP filepipeline_http_request_duration_seconds HTTP request duration.",
		"# TYPE filepipeline_http_request_duration_seconds histogram",
	)
	for _, route := range s.Routes {
		labels := fmt.Sprintf("method=%q,route=%q", escapeLabel(route.Method), escapeLabel(route.Route))
		cumulative := uint64(0)
		for i, bound := range route.DurationBounds {
			cumulative += route.DurationCounts[i]
			lines = append(lines, fmt.Sprintf("filepipeline_http_request_duration_seconds_bucket{%s,le=%q} %d", labels, formatSeconds(bound), cumulative))
		}
		cumulative += route.DurationCounts[len(route.DurationCounts)-1]
		lines = append(lines,
			fmt.Sprintf("filepipeline_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d", labels, cumulative),
			fmt.Sprintf("filepipeline_http_request_duration_seconds_sum{%s} %s", labels, formatSeconds(route.DurationTotal)),
			fmt.Sprintf("filepipeline_http_request_duration_seconds_count{%s} %d", labels, route.Requests),
		)
		statuses := make([]int, 0, len(route.StatusCounts))
		for status := range route.StatusCounts {
			statuses = append(statuses, status)
		}
		sort.Ints(statuses)
		for _, status := range statuses {
			lines = append(lines, fmt.Sprintf("filepipeline_http_responses_total{%s,status=%q} %d", labels, strconv.Itoa(status), route.StatusCounts[status]))
		}
	}
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}

func formatSeconds(duration time.Duration) string {
	return strconv.FormatFloat(duration.Seconds(), 'f', 6, 64)
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}
