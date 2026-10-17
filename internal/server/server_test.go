package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	"github.com/emircanagac/northscope/internal/k8s"
	"github.com/emircanagac/northscope/internal/models"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func newTestServer(t *testing.T, watcher *k8s.Watcher) *Server {
	t.Helper()
	return New(":0", watcher, fstest.MapFS{
		"index.html": {Data: []byte("ok")},
	}, "test")
}

func serve(t *testing.T, server *Server, target string) (*http.Response, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	response := recorder.Result()
	t.Cleanup(func() { _ = response.Body.Close() })
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s response body: %v", target, err)
	}
	return response, string(body)
}

// runDegradedWatcher starts a watcher whose Services can be listed but not
// watched, so it becomes ready with a degraded Services cache.
func runDegradedWatcher(t *testing.T) *k8s.Watcher {
	t.Helper()
	client := fake.NewSimpleClientset()
	client.PrependWatchReactor("services", func(clienttesting.Action) (bool, watch.Interface, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "services"}, "", errors.New("RBAC: access denied"))
	})
	watcher, err := k8s.NewWatcherFromClient(client, time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = watcher.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	deadline := time.Now().Add(5 * time.Second)
	for !watcher.Ready() || watcher.Status().Status != models.HealthDegraded {
		if time.Now().After(deadline) {
			t.Fatalf("watcher did not become ready and degraded: %#v", watcher.Status())
		}
		time.Sleep(10 * time.Millisecond)
	}
	return watcher
}

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
	}, "test")

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer func() { _ = response.Body.Close() }()
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

func TestVersionEndpointExposesRuntimeBuildVersion(t *testing.T) {
	watcher, err := k8s.NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	server := New(":0", watcher, fstest.MapFS{
		"index.html": {Data: []byte("ok")},
	}, "v9.8.7")

	request := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("expected application/json, got %q", contentType)
	}
	if cacheControl := response.Header.Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("expected no-store cache control, got %q", cacheControl)
	}
	if got := strings.TrimSpace(string(body)); got != `{"version":"v9.8.7"}` {
		t.Fatalf("unexpected version response: %s", got)
	}
}

func TestWebSocketDisconnectRemovesSubscriberWithoutTopologyUpdate(t *testing.T) {
	watcher, err := k8s.NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	server := New(":0", watcher, fstest.MapFS{
		"index.html": {Data: []byte("ok")},
	}, "test")
	httpServer := httptest.NewServer(server.httpServer.Handler)
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("connect websocket: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for watcher.Metrics().WebsocketSubscribers != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := watcher.Metrics().WebsocketSubscribers; got != 1 {
		t.Fatalf("expected one websocket subscriber, got %d", got)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close websocket: %v", err)
	}
	deadline = time.Now().Add(time.Second)
	for watcher.Metrics().WebsocketSubscribers != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := watcher.Metrics().WebsocketSubscribers; got != 0 {
		t.Fatalf("expected disconnected subscriber to be removed, got %d", got)
	}
}

func TestStatusEndpointReportsSyncingResources(t *testing.T) {
	watcher, err := k8s.NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	response, body := serve(t, newTestServer(t, watcher), "/api/status")

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if cacheControl := response.Header.Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("expected no-store cache control, got %q", cacheControl)
	}
	var status models.WatcherStatus
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, body)
	}
	if status.Status != models.HealthSyncing || status.Ready || status.GeneratedAt != nil {
		t.Fatalf("expected syncing status without a snapshot, got %#v", status)
	}
	if len(status.Resources) == 0 || len(status.Unsynced) != len(status.Resources) {
		t.Fatalf("expected every resource to be unsynced before Run, got %#v", status)
	}
}

func TestDegradedWatchKeepsReadyzPassingAndIsReported(t *testing.T) {
	server := newTestServer(t, runDegradedWatcher(t))

	response, _ := serve(t, server, "/readyz")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected degraded watches to keep /readyz at 204, got %d", response.StatusCode)
	}

	response, body := serve(t, server, "/readyz?verbose")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected verbose readyz 200, got %d:\n%s", response.StatusCode, body)
	}
	for _, line := range []string{
		"[+]informer-sync ok",
		"[+]topology-snapshot ok (v1)",
		"[!]watch-health degraded: services",
		"readyz check passed",
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("expected verbose readyz to contain %q, got:\n%s", line, body)
		}
	}

	_, body = serve(t, server, "/api/status")
	var status models.WatcherStatus
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, body)
	}
	if status.Status != models.HealthDegraded || !status.Ready || strings.Join(status.Degraded, ",") != "services" {
		t.Fatalf("expected ready status with degraded services, got %#v", status)
	}

	_, body = serve(t, server, "/api/topology")
	var snapshot models.TopologySnapshot
	if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
		t.Fatalf("decode topology: %v\n%s", err, body)
	}
	if snapshot.Health.Status != models.HealthDegraded || len(snapshot.Health.Degraded) != 1 || snapshot.Health.Degraded[0].Resource != "services" {
		t.Fatalf("expected snapshot health to name services, got %#v", snapshot.Health)
	}

	_, body = serve(t, server, "/metrics")
	for _, metric := range []string{
		`northscope_informer_synced{resource="services"} 1`,
		`northscope_informer_degraded{resource="services"} 1`,
		`northscope_informer_degraded{resource="pods"} 0`,
		`northscope_watch_errors_total{resource="services"} `,
		`northscope_watch_errors_total{resource="pods"} 0`,
		`northscope_last_watch_success_timestamp_seconds{resource="pods"} `,
		"# TYPE northscope_watch_errors_total counter",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("expected metrics body to contain %q, got:\n%s", metric, body)
		}
	}
	if strings.Contains(body, `northscope_watch_errors_total{resource="services"} 0`) {
		t.Fatalf("expected services watch errors to be counted, got:\n%s", body)
	}
	if strings.Contains(body, "northscope_snapshot_generated_timestamp_seconds 0\n") {
		t.Fatalf("expected snapshot generation time to be set, got:\n%s", body)
	}
}

func TestVerboseReadyzFailsBeforeFirstSnapshot(t *testing.T) {
	watcher, err := k8s.NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	response, body := serve(t, newTestServer(t, watcher), "/readyz?verbose")

	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.StatusCode)
	}
	for _, line := range []string{"[-]informer-sync waiting for: ", "[-]topology-snapshot not generated yet", "readyz check failed"} {
		if !strings.Contains(body, line) {
			t.Fatalf("expected verbose readyz to contain %q, got:\n%s", line, body)
		}
	}
}
