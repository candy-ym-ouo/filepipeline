package telemetry

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += int64(written)
	return written, err
}

func (w *responseRecorder) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *responseRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func Instrument(registry *Registry, next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	if registry == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		finish := registry.Begin()
		wrapped := &responseRecorder{ResponseWriter: w}
		defer func() {
			finish()
			status := wrapped.status
			if status == 0 {
				status = http.StatusOK
			}
			registry.ObserveHTTP(HTTPObservation{
				Method: r.Method, Route: RouteName(r), Status: status,
				Duration: time.Since(started), Bytes: wrapped.bytes,
			})
		}()
		next.ServeHTTP(wrapped, r)
	})
}

func RouteName(r *http.Request) string {
	if r == nil {
		return "/"
	}
	return NormalizeRoute(r.URL.Path)
}

func NormalizeRoute(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if index := strings.IndexAny(path, "?#"); index >= 0 {
		path = path[:index]
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "" || strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			continue
		}
		if looksLikeIdentifier(part) {
			parts[i] = "{id}"
		}
	}
	result := strings.Join(parts, "/")
	if len(result) > 1 {
		result = strings.TrimSuffix(result, "/")
	}
	return result
}

func looksLikeIdentifier(value string) bool {
	if len(value) >= 24 && isHexOrDash(value) {
		return true
	}
	if len(value) >= 8 {
		if _, err := strconv.ParseUint(value, 10, 64); err == nil {
			return true
		}
	}
	return false
}

func isHexOrDash(value string) bool {
	hexCount := 0
	for _, char := range value {
		switch {
		case char >= '0' && char <= '9', char >= 'a' && char <= 'f', char >= 'A' && char <= 'F':
			hexCount++
		case char == '-':
		default:
			return false
		}
	}
	return hexCount >= 20
}

func PrometheusHandler(registry *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if registry == nil {
			http.Error(w, "telemetry registry unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := registry.Snapshot().WritePrometheus(w); err != nil {
			return
		}
	})
}

func JSONHandler(registry *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if registry == nil {
			http.Error(w, `{"code":"ERR_TELEMETRY_UNAVAILABLE"}`, http.StatusServiceUnavailable)
			return
		}
		data, err := registry.Snapshot().MarshalJSON()
		if err != nil {
			http.Error(w, `{"code":"ERR_TELEMETRY_ENCODE"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(append(data, '\n'))
	})
}
