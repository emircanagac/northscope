package k8s

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/emircanagac/northscope/internal/models"
)

func forbidden(resource string) error {
	return apierrors.NewForbidden(schema.GroupResource{Resource: resource}, "", errors.New("RBAC: access denied"))
}

// toggledReactor fails the matching verb and resource while forbid is set.
func toggledReactor(forbid *atomic.Bool, resource string) clienttesting.ReactionFunc {
	return func(clienttesting.Action) (bool, runtime.Object, error) {
		if forbid.Load() {
			return true, nil, forbidden(resource)
		}
		return false, nil, nil
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buffer := &syncBuffer{}
	previous := log.Writer()
	log.SetOutput(io.MultiWriter(previous, buffer))
	t.Cleanup(func() { log.SetOutput(previous) })
	return buffer
}

func runWatcher(t *testing.T, watcher *Watcher) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh: //nolint:errcheck // Run's error after cancellation is not what this cleanup checks.
		case <-time.After(2 * time.Second):
			t.Error("watcher did not stop after context cancellation")
		}
	})
	return errCh
}

func eventually(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func watchedResource(status models.WatcherStatus, resource string) (models.ResourceHealth, bool) {
	for _, item := range status.Resources {
		if item.Resource == resource {
			return item, true
		}
	}
	return models.ResourceHealth{}, false
}

func TestWatcherReportsForbiddenResourceWhileWaitingForCacheSync(t *testing.T) {
	logs := captureLog(t)
	client := fake.NewSimpleClientset()
	var forbidPods atomic.Bool
	forbidPods.Store(true)
	client.PrependReactor("list", "pods", toggledReactor(&forbidPods, "pods"))

	watcher, err := NewWatcherFromClient(client, time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	watcher.cacheSyncReportInterval = 50 * time.Millisecond
	runWatcher(t, watcher)

	eventually(t, 5*time.Second, "pods to be reported as degraded and unsynced", func() bool {
		output := logs.String()
		return strings.Contains(output, "unsynced: pods (last error: ") && strings.Contains(output, "pods is forbidden: RBAC: access denied)")
	})

	status := watcher.Status()
	if watcher.Ready() || status.Ready {
		t.Fatal("expected watcher to stay unready while pods cannot be listed")
	}
	if status.Status != models.HealthDegraded {
		t.Fatalf("expected degraded status, got %q", status.Status)
	}
	if strings.Join(status.Degraded, ",") != "pods" || strings.Join(status.Unsynced, ",") != "pods" {
		t.Fatalf("expected only pods degraded and unsynced, got degraded=%v unsynced=%v", status.Degraded, status.Unsynced)
	}
	pods, ok := watchedResource(status, "pods")
	if !ok {
		t.Fatalf("expected pods in status resources, got %#v", status.Resources)
	}
	if pods.ErrorCount == 0 || !strings.Contains(pods.LastError, "forbidden") || pods.LastErrorAt == nil || pods.DegradedSince == nil {
		t.Fatalf("expected pods error details, got %#v", pods)
	}
	if pods.LastSuccessAt != nil {
		t.Fatalf("expected pods to have no successful list yet, got %v", pods.LastSuccessAt)
	}
	services, ok := watchedResource(status, "services")
	if !ok || !services.Synced || services.Degraded || services.LastSuccessAt == nil {
		t.Fatalf("expected services to be synced and healthy, got %#v", services)
	}
	if !strings.Contains(logs.String(), "NorthScope watch for pods degraded") {
		t.Fatalf("expected degraded transition to be logged, got:\n%s", logs.String())
	}

	forbidPods.Store(false)
	eventually(t, 10*time.Second, "watcher to become ready after RBAC is fixed", watcher.Ready)
	eventually(t, 5*time.Second, "pods watch to recover", func() bool {
		return watcher.Status().Status == models.HealthOK
	})

	pods, _ = watchedResource(watcher.Status(), "pods")
	if !pods.Synced || pods.Degraded || pods.LastSuccessAt == nil || pods.ErrorCount == 0 {
		t.Fatalf("expected pods to recover and keep its error count, got %#v", pods)
	}
	if latest := watcher.Latest(); latest.Health.Status != models.HealthOK {
		t.Fatalf("expected healthy snapshot after recovery, got %#v", latest.Health)
	}
}

func TestWatcherRunReturnsContextErrorWhileWaitingForCacheSync(t *testing.T) {
	client := fake.NewSimpleClientset()
	var forbidNodes atomic.Bool
	forbidNodes.Store(true)
	client.PrependReactor("list", "nodes", toggledReactor(&forbidNodes, "nodes"))

	watcher, err := NewWatcherFromClient(client, time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if err := watcher.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline error, got %v", err)
	}
}

func TestWatcherPublishesDegradedSnapshotWhileWatchIsForbidden(t *testing.T) {
	client := fake.NewSimpleClientset()
	var forbidWatch atomic.Bool
	forbidWatch.Store(true)
	client.PrependWatchReactor("services", func(clienttesting.Action) (bool, watch.Interface, error) {
		if forbidWatch.Load() {
			return true, nil, forbidden("services")
		}
		return false, nil, nil
	})

	watcher, err := NewWatcherFromClient(client, time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	runWatcher(t, watcher)

	// Listing still works, so the caches sync and readiness is unaffected.
	eventually(t, 5*time.Second, "watcher to become ready", watcher.Ready)
	eventually(t, 5*time.Second, "degraded snapshot", func() bool {
		return watcher.Latest().Health.Status == models.HealthDegraded
	})
	degraded := watcher.Latest()
	if len(degraded.Health.Degraded) != 1 || degraded.Health.Degraded[0].Resource != "services" {
		t.Fatalf("expected services to be degraded, got %#v", degraded.Health)
	}
	if !degraded.Health.Degraded[0].Synced || !strings.Contains(degraded.Health.Degraded[0].Error, "forbidden") {
		t.Fatalf("expected synced services with a forbidden error, got %#v", degraded.Health.Degraded[0])
	}

	// The reflector relists successfully before every failed watch. Those
	// lists must not flip the resource back to healthy.
	eventually(t, 10*time.Second, "a second failed watch", func() bool {
		services, _ := watchedResource(watcher.Status(), "services")
		return services.ErrorCount >= 2
	})
	if latest := watcher.Latest(); latest.Health.Status != models.HealthDegraded || latest.Version != degraded.Version {
		t.Fatalf("expected degraded snapshot v%d to stay current, got v%d %#v", degraded.Version, latest.Version, latest.Health)
	}

	forbidWatch.Store(false)
	eventually(t, 10*time.Second, "healthy snapshot after the watch recovers", func() bool {
		return watcher.Latest().Health.Status == models.HealthOK
	})
	if recovered := watcher.Latest(); recovered.Version <= degraded.Version {
		t.Fatalf("expected recovery to publish a newer snapshot than v%d, got v%d", degraded.Version, recovered.Version)
	}
}

// preferredDiscovery serves optional resources, which FakeDiscovery does not.
type preferredDiscovery struct {
	*fakediscovery.FakeDiscovery
	preferred []*metav1.APIResourceList
}

func (d preferredDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return d.preferred, nil
}

func TestWatcherTracksOptionalDynamicInformers(t *testing.T) {
	client := fake.NewSimpleClientset()
	httpRoutes := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{httpRoutes: "HTTPRouteList"},
	)
	var forbidRoutes atomic.Bool
	forbidRoutes.Store(true)
	dynamicClient.PrependReactor("list", "httproutes", toggledReactor(&forbidRoutes, "httproutes"))

	watcher, err := NewWatcherFromClients(client, dynamicClient, time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	watcher.discovery = preferredDiscovery{
		FakeDiscovery: client.Discovery().(*fakediscovery.FakeDiscovery),
		preferred: []*metav1.APIResourceList{{
			GroupVersion: httpRoutes.GroupVersion().String(),
			APIResources: []metav1.APIResource{{Name: "httproutes", Kind: "HTTPRoute", Namespaced: true}},
		}},
	}
	runWatcher(t, watcher)

	const resource = "httproutes.gateway.networking.k8s.io"
	eventually(t, 5*time.Second, "watcher to become ready", watcher.Ready)
	eventually(t, 5*time.Second, "optional resource to be degraded", func() bool {
		return watcher.Latest().Health.Status == models.HealthDegraded
	})
	routes, ok := watchedResource(watcher.Status(), resource)
	if !ok || !routes.Optional || routes.Synced || !routes.Degraded || routes.ErrorCount == 0 {
		t.Fatalf("expected unsynced degraded optional %s, got %#v", resource, routes)
	}
	if unsynced := watcher.Status().Unsynced; strings.Join(unsynced, ",") != resource {
		t.Fatalf("expected only %s to be unsynced, got %v", resource, unsynced)
	}

	forbidRoutes.Store(false)
	eventually(t, 10*time.Second, "optional resource to recover", func() bool {
		routes, _ := watchedResource(watcher.Status(), resource)
		return routes.Synced && !routes.Degraded
	})
}

func TestInformerHealthTransitions(t *testing.T) {
	health := newInformerHealth()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	health.now = func() time.Time { return now }
	changes := 0
	health.onChange = func() { changes++ }
	synced := false
	health.resources["pods"] = &resourceHealthState{hasSynced: func() bool { return synced }}

	for _, err := range []error{
		io.EOF,
		io.ErrUnexpectedEOF,
		context.Canceled,
		apierrors.NewResourceExpired("too old resource version"),
		apierrors.NewGone("gone"), //nolint:staticcheck // API servers still return 410 Gone for expired watches.
	} {
		health.recordError("pods", err)
	}
	if got := health.snapshotHealth(); got.Status != models.HealthOK || changes != 0 {
		t.Fatalf("expected transient watch closes to be ignored, got %#v after %d changes", got, changes)
	}

	health.recordError("pods", forbidden("pods"))
	degradedAt := now
	now = now.Add(time.Second)
	health.recordError("pods", errors.New("connection refused"))
	got := health.snapshotHealth()
	if got.Status != models.HealthDegraded || len(got.Degraded) != 1 || changes != 1 {
		t.Fatalf("expected one degraded transition, got %#v after %d changes", got, changes)
	}
	if !got.Degraded[0].Since.Equal(degradedAt) || got.Degraded[0].Error != "connection refused" || got.Degraded[0].Synced {
		t.Fatalf("expected degraded since first error with latest message, got %#v", got.Degraded[0])
	}

	synced = true
	health.recordSuccess("pods", false)
	if health.snapshotHealth().Status != models.HealthDegraded || changes != 1 {
		t.Fatal("expected a successful list alone to keep the resource degraded")
	}
	if sameHealth(got, health.snapshotHealth()) {
		t.Fatal("expected a change in sync state to warrant a new snapshot")
	}

	now = now.Add(time.Second)
	health.recordSuccess("pods", true)
	if health.snapshotHealth().Status != models.HealthOK || changes != 2 {
		t.Fatalf("expected a watch start to recover the resource after %d changes", changes)
	}
	resources := health.resourceHealth()
	if len(resources) != 1 || resources[0].ErrorCount != 2 || resources[0].LastSuccessAt == nil || !resources[0].LastSuccessAt.Equal(now) {
		t.Fatalf("expected error count and last success to be kept, got %#v", resources)
	}
	if resources[0].DegradedSince != nil || resources[0].LastError != "connection refused" {
		t.Fatalf("expected recovery to clear degraded time but keep last error, got %#v", resources[0])
	}

	health.recordError("unknown", forbidden("unknown"))
	health.recordSuccess("unknown", true)
	if changes != 2 {
		t.Fatalf("expected untracked resources to be ignored, got %d changes", changes)
	}
}

func TestSameHealthIgnoresErrorText(t *testing.T) {
	since := time.Now()
	left := models.SnapshotHealth{Status: models.HealthDegraded, Degraded: []models.DegradedResource{{Resource: "pods", Since: since, Error: "first"}}}
	right := models.SnapshotHealth{Status: models.HealthDegraded, Degraded: []models.DegradedResource{{Resource: "pods", Since: since, Error: "second"}}}
	if !sameHealth(left, right) {
		t.Fatal("expected changing error text alone not to republish")
	}
	right.Degraded = append(right.Degraded, models.DegradedResource{Resource: "services"})
	if sameHealth(left, right) {
		t.Fatal("expected a newly degraded resource to republish")
	}
	if sameHealth(models.SnapshotHealth{Status: models.HealthOK}, left) {
		t.Fatal("expected a status change to republish")
	}
}

func TestObservedWatchForwardsEventsAndStops(t *testing.T) {
	source := watch.NewFake()
	var observed atomic.Int32
	observedWatch := newObservedWatch(source, func() { observed.Add(1) })

	go func() {
		source.Add(&metav1.PartialObjectMetadata{})
		source.Error(&metav1.Status{Status: metav1.StatusFailure})
	}()
	for _, expected := range []watch.EventType{watch.Added, watch.Error} {
		select {
		case event := <-observedWatch.ResultChan():
			if event.Type != expected {
				t.Fatalf("expected %s event, got %s", expected, event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("expected %s event to be forwarded", expected)
		}
	}
	if got := observed.Load(); got != 1 {
		t.Fatalf("expected only the non-error event to count as success, got %d", got)
	}

	observedWatch.Stop()
	observedWatch.Stop()
	select {
	case _, ok := <-observedWatch.ResultChan():
		if ok {
			t.Fatal("expected result channel to close after Stop")
		}
	case <-time.After(time.Second):
		t.Fatal("expected result channel to close after Stop")
	}
	if !source.IsStopped() {
		t.Fatal("expected Stop to stop the source watch")
	}
}

func TestTypedInformersShareFactoryCaches(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}

	tracked := map[string]bool{}
	for _, resource := range watcher.health.resourceHealth() {
		tracked[resource.Resource] = true
	}
	for _, resource := range []string{"ingresses.networking.k8s.io", "ingressclasses.networking.k8s.io", "services", "endpoints", "pods", "nodes"} {
		if !tracked[resource] {
			t.Fatalf("expected %s to be tracked, got %v", resource, tracked)
		}
	}

	// The generated accessor must return the observed informer, not build a
	// second, untracked one.
	if watcher.factory.InformerFor(&corev1.Pod{}, nil) != watcher.podInformer.Informer() {
		t.Fatal("expected the pod lister to read the observed informer")
	}
}
