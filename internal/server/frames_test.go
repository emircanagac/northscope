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

	"github.com/emircanagac/northscope/internal/k8s"
	"github.com/emircanagac/northscope/internal/models"
	"k8s.io/client-go/kubernetes/fake"
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
