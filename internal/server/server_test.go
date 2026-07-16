package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/emircanagac/northscope/internal/k8s"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSameOriginAllowsMissingOrigin(t *testing.T) {
	request := &http.Request{
		Host:   "northscope.local",
		Header: http.Header{},
	}

	if !sameOrigin(request) {
		t.Fatal("expected missing Origin header to be allowed")
	}
}

func TestSameOriginAllowsMatchingHost(t *testing.T) {
	request := &http.Request{
		Host: "northscope.local:8080",
		Header: http.Header{
			"Origin": []string{"http://northscope.local:8080"},
		},
	}

	if !sameOrigin(request) {
		t.Fatal("expected matching Origin host to be allowed")
	}
}

func TestSameOriginRejectsCrossOrigin(t *testing.T) {
	request := &http.Request{
		Host: "northscope.local:8080",
		Header: http.Header{
			"Origin": []string{"https://example.com"},
		},
	}

	if sameOrigin(request) {
		t.Fatal("expected cross-origin websocket request to be rejected")
	}
}

func TestMetricsEndpointExposesPrometheusText(t *testing.T) {
	watcher, err := k8s.NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	server := New(":0", watcher, fstest.MapFS{
		"index.html": {Data: []byte("ok")},
	})

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("expected text/plain metrics, got %q", contentType)
	}
	for _, metric := range []string{
		"northscope_ready 0",
		"northscope_snapshot_builds_total 0",
		"northscope_snapshot_build_errors_total 0",
		"northscope_snapshot_publishes_total 0",
		"northscope_snapshot_unchanged_total 0",
		"northscope_websocket_clients 0",
	} {
		if !strings.Contains(string(body), metric) {
			t.Fatalf("expected metrics body to contain %q, got:\n%s", metric, string(body))
		}
	}
}
