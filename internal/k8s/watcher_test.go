package k8s

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emircanagac/northscope/internal/models"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWatcherKeepsLastSnapshotWhenBuildFails(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}

	atomic.StoreUint32(&watcher.ready, 1)
	healthySnapshot := models.TopologySnapshot{
		Nodes: []models.Node{{
			ID: "node-1",
			Data: models.NodeData{
				Label: "node-1",
			},
		}},
	}
	buildErr := errors.New("cache lister failed")
	call := 0
	watcher.buildSnapshotFunc = func() (models.TopologySnapshot, error) {
		call++
		if call == 1 {
			return healthySnapshot, nil
		}
		return models.TopologySnapshot{}, buildErr
	}

	watcher.rebuildAndPublish()
	before := watcher.Latest()
	watcher.rebuildAndPublish()
	after := watcher.Latest()

	if before.Version != 1 {
		t.Fatalf("expected first snapshot version 1, got %d", before.Version)
	}
	if after.Version != before.Version {
		t.Fatalf("expected failed rebuild to keep version %d, got %d", before.Version, after.Version)
	}
	if len(after.Nodes) != 1 || after.Nodes[0].ID != "node-1" {
		t.Fatalf("expected failed rebuild to keep last healthy snapshot, got %#v", after.Nodes)
	}
}

func TestWatcherSendsLatestSnapshotToReconnectedSubscriber(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}

	atomic.StoreUint32(&watcher.ready, 1)
	watcher.buildSnapshotFunc = func() (models.TopologySnapshot, error) {
		return models.TopologySnapshot{
			GeneratedAt: time.Now().UTC(),
			Nodes: []models.Node{{
				ID: "ingress-1",
				Data: models.NodeData{
					Label: "ingress-1",
				},
			}},
		}, nil
	}
	watcher.rebuildAndPublish()

	firstUpdates, firstUnsubscribe := watcher.Subscribe(1)
	firstUnsubscribe()

	select {
	case <-firstUpdates:
	case <-time.After(time.Second):
		t.Fatal("expected initial subscriber to receive latest snapshot")
	}

	reconnectedUpdates, reconnectedUnsubscribe := watcher.Subscribe(1)
	defer reconnectedUnsubscribe()

	select {
	case snapshot := <-reconnectedUpdates:
		if snapshot.Version != 1 {
			t.Fatalf("expected reconnected subscriber to receive snapshot version 1, got %d", snapshot.Version)
		}
		if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].ID != "ingress-1" {
			t.Fatalf("expected reconnected subscriber to receive latest snapshot, got %#v", snapshot.Nodes)
		}
	case <-time.After(time.Second):
		t.Fatal("expected reconnected subscriber to receive latest snapshot")
	}
}

func TestWatcherRunStopsOnContextCancel(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Run(ctx)
	}()

	deadline := time.After(2 * time.Second)
	for !watcher.Ready() {
		select {
		case err := <-errCh:
			t.Fatalf("watcher exited before becoming ready: %v", err)
		case <-deadline:
			t.Fatal("watcher did not become ready")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop after context cancellation")
	}
}
