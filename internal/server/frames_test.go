package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/emircanagac/northscope/internal/k8s"
	"github.com/emircanagac/northscope/internal/k8s/synthetic"
	"github.com/emircanagac/northscope/internal/models"
)

func TestSnapshotFramesEncodeEachSnapshotOnce(t *testing.T) {
	var frames snapshotFrames
	v2 := models.TopologySnapshot{Instance: "a", Version: 2}

	first, err := frames.frame(v2)
	if err != nil {
		t.Fatalf("encode v2: %v", err)
	}
	again, err := frames.frame(v2)
	if err != nil {
		t.Fatalf("encode v2 again: %v", err)
	}
	if first != again {
		t.Fatal("expected the same snapshot to reuse its prepared frame")
	}

	// A slow client that still drains v1 must not evict the newer frame.
	older, err := frames.frame(models.TopologySnapshot{Instance: "a", Version: 1})
	if err != nil {
		t.Fatalf("encode v1: %v", err)
	}
	if older == first {
		t.Fatal("expected an older snapshot to get its own frame")
	}
	if cached, _ := frames.frame(v2); cached != first {
		t.Fatal("expected an older snapshot to keep the newer frame cached")
	}

	// Another instance (a restart or replica) starts its versions again.
	restarted, err := frames.frame(models.TopologySnapshot{Instance: "b", Version: 1})
	if err != nil {
		t.Fatalf("encode restarted instance: %v", err)
	}
	if cached, _ := frames.frame(models.TopologySnapshot{Instance: "b", Version: 1}); cached != restarted {
		t.Fatal("expected a new instance's snapshot to replace the cached frame")
	}
}

func TestWebSocketSendsCompressedSnapshotsToEveryClient(t *testing.T) {
	watcher, err := k8s.NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
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
	for !watcher.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("watcher did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	server := New(":0", watcher, fstest.MapFS{"index.html": {Data: []byte("ok")}}, "test")
	httpServer := httptest.NewServer(server.httpServer.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	want := watcher.Latest()
	for _, compression := range []bool{true, false} {
		dialer := websocket.Dialer{EnableCompression: compression}
		conn, response, err := dialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("compression %t: connect websocket: %v", compression, err)
		}
		extensions := response.Header.Get("Sec-WebSocket-Extensions")
		if compression != strings.Contains(extensions, "permessage-deflate") {
			_ = conn.Close()
			t.Fatalf("compression %t: unexpected negotiated extensions %q", compression, extensions)
		}

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var got models.TopologySnapshot
		if err := conn.ReadJSON(&got); err != nil {
			_ = conn.Close()
			t.Fatalf("compression %t: read snapshot: %v", compression, err)
		}
		_ = conn.Close()
		if got.Instance != want.Instance || got.Version != want.Version || !got.GeneratedAt.Equal(want.GeneratedAt) {
			t.Fatalf("compression %t: got snapshot %s v%d, want %s v%d", compression, got.Instance, got.Version, want.Instance, want.Version)
		}
	}
}

func TestTopologyEndpointIsNotCached(t *testing.T) {
	watcher, err := k8s.NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	response, _ := serve(t, newTestServer(t, watcher), "/api/topology")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if cacheControl := response.Header.Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("expected no-store cache control, got %q", cacheControl)
	}
}

func TestWebSocketSendsDeltasOnlyToClientsThatAskForThem(t *testing.T) {
	cluster := synthetic.New(synthetic.Small)
	client := fake.NewSimpleClientset(cluster.Objects()...)
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
	for !watcher.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("watcher did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	server := newTestServer(t, watcher)
	httpServer := httptest.NewServer(server.httpServer.Handler)
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"

	dial := func(path string) *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL+path, nil)
		if err != nil {
			t.Fatalf("connect %q: %v", path, err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	read := func(conn *websocket.Conn) map[string]any {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatalf("read message: %v", err)
		}
		return message
	}
	deltaClient := dial("?delta=1")
	plainClient := dial("")
	first := read(deltaClient)
	if _, typed := first["type"]; typed || read(plainClient)["version"] != first["version"] {
		t.Fatalf("expected both clients to start with the same full snapshot, got %v", first["type"])
	}

	pod := cluster.Pods[len(cluster.Pods)-1].DeepCopy()
	pod.Status.Conditions[1].Status = corev1.ConditionFalse
	if _, err := client.CoreV1().Pods(pod.Namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update pod: %v", err)
	}

	delta := read(deltaClient)
	if delta["type"] != "delta" || delta["baseVersion"] != first["version"] {
		t.Fatalf("expected a delta from v%v, got type %v base %v", first["version"], delta["type"], delta["baseVersion"])
	}
	upserts, _ := delta["upsertNodes"].([]any)
	if len(upserts) == 0 || len(upserts) > 5 {
		t.Fatalf("expected the changed Pod and the few nodes that depend on it, got %d", len(upserts))
	}
	if full := read(plainClient); full["type"] != nil || full["version"] != delta["version"] {
		t.Fatalf("expected the plain client to get the full snapshot v%v, got %v", delta["version"], full["version"])
	}

	_, metrics := serve(t, server, "/metrics")
	for _, want := range []string{
		`northscope_websocket_messages_total{type="delta"} 1`,
		`northscope_websocket_messages_total{type="snapshot"} 3`,
		`northscope_snapshot_deltas_total 1`,
		`northscope_snapshot_build_seconds_bucket{le="+Inf"} 2`,
		`northscope_watch_events_total{resource="pods",event="update"} 1`,
	} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("expected %q in metrics:\n%s", want, metrics)
		}
	}
	if strings.Contains(metrics, "northscope_snapshot_encoded_bytes 0\n") {
		t.Fatal("expected the encoded snapshot size to be reported")
	}
}
