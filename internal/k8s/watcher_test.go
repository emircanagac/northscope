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

	metrics := watcher.Metrics()
	if metrics.SnapshotBuildsTotal != 1 {
		t.Fatalf("expected 1 successful build, got %d", metrics.SnapshotBuildsTotal)
	}
	if metrics.SnapshotBuildErrorsTotal != 1 {
		t.Fatalf("expected 1 build error, got %d", metrics.SnapshotBuildErrorsTotal)
	}
	if metrics.SnapshotVersion != 1 {
		t.Fatalf("expected snapshot version 1, got %d", metrics.SnapshotVersion)
	}
	if metrics.SnapshotNodes != 1 {
		t.Fatalf("expected 1 snapshot node, got %d", metrics.SnapshotNodes)
	}
	if metrics.LastSnapshotBuildDurationSeconds < 0 {
		t.Fatalf("expected non-negative build duration, got %f", metrics.LastSnapshotBuildDurationSeconds)
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

func TestWatcherCoalescesBurstEventsIntoSingleSnapshot(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}

	watcher.rebuildDebounce = 20 * time.Millisecond
	atomic.StoreUint32(&watcher.ready, 1)
	watcher.buildSnapshotFunc = func() (models.TopologySnapshot, error) {
		return models.TopologySnapshot{GeneratedAt: time.Now().UTC()}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.runRebuildLoop(ctx)
	}()

	for i := 0; i < 10; i++ {
		watcher.rebuildAndPublishWhenReady()
	}

	deadline := time.After(time.Second)
	for watcher.Metrics().SnapshotBuildsTotal < 1 {
		select {
		case err := <-errCh:
			t.Fatalf("rebuild loop exited early: %v", err)
		case <-deadline:
			t.Fatal("coalesced snapshot was not built")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	time.Sleep(50 * time.Millisecond)
	if builds := watcher.Metrics().SnapshotBuildsTotal; builds != 1 {
		t.Fatalf("expected burst to produce 1 snapshot build, got %d", builds)
	}
}

func TestWatcherSkipsPublishingUnchangedTopology(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}

	atomic.StoreUint32(&watcher.ready, 1)
	watcher.buildSnapshotFunc = func() (models.TopologySnapshot, error) {
		return models.TopologySnapshot{
			GeneratedAt: time.Now().UTC(),
			Inventory:   models.ClusterInventory{Ingresses: 1},
			Nodes:       []models.Node{{ID: "ingress-1"}},
		}, nil
	}

	watcher.rebuildAndPublish()
	watcher.rebuildAndPublish()

	metrics := watcher.Metrics()
	if metrics.SnapshotBuildsTotal != 2 {
		t.Fatalf("expected 2 successful builds, got %d", metrics.SnapshotBuildsTotal)
	}
	if metrics.SnapshotPublishesTotal != 1 {
		t.Fatalf("expected 1 published snapshot, got %d", metrics.SnapshotPublishesTotal)
	}
	if metrics.SnapshotUnchangedTotal != 1 {
		t.Fatalf("expected 1 unchanged snapshot, got %d", metrics.SnapshotUnchangedTotal)
	}
	if metrics.SnapshotVersion != 1 {
		t.Fatalf("expected unchanged topology to keep version 1, got %d", metrics.SnapshotVersion)
	}
}
