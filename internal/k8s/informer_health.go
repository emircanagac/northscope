package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"github.com/emircanagac/northscope/internal/models"
)

// informerHealth tracks list and watch outcomes per watched resource, so a
// cache that keeps serving old data after an RBAC, credential, or API server
// failure shows up in logs, metrics, and the UI instead of looking live.
//
// A resource is degraded from its first list or watch error until a watch
// starts or delivers an event again. A successful list alone does not clear
// it: with list but no watch permission the reflector would otherwise flip
// between healthy and degraded on every retry.
type informerHealth struct {
	mu        sync.Mutex
	resources map[string]*resourceHealthState
	now       func() time.Time
	onChange  func()
}

type resourceHealthState struct {
	optional      bool
	hasSynced     cache.InformerSynced
	lastSuccess   time.Time
	lastError     string
	lastErrorAt   time.Time
	errorCount    uint64
	degradedSince time.Time
}

func newInformerHealth() *informerHealth {
	return &informerHealth{
		resources: make(map[string]*resourceHealthState),
		now:       time.Now,
	}
}

// track registers an informer before it starts. Its watch error handler keeps
// client-go's default logging and records the failure.
func (h *informerHealth) track(resource string, informer cache.SharedIndexInformer, optional bool) error {
	h.mu.Lock()
	h.resources[resource] = &resourceHealthState{optional: optional, hasSynced: informer.HasSynced}
	h.mu.Unlock()

	return informer.SetWatchErrorHandlerWithContext(func(ctx context.Context, r *cache.Reflector, err error) {
		cache.DefaultWatchErrorHandler(ctx, r, err)
		h.recordError(resource, err)
	})
}

// listWatch wraps a resource's list and watch calls so that every successful
// list, watch start, and watch event refreshes its last success time.
func (h *informerHealth) listWatch(
	resource string,
	list func(context.Context, metav1.ListOptions) (runtime.Object, error),
	watchResource func(context.Context, metav1.ListOptions) (watch.Interface, error),
) *cache.ListWatch {
	return &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			object, err := list(ctx, options)
			if err != nil {
				return nil, err
			}
			h.recordSuccess(resource, false)
			return object, nil
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			watcher, err := watchResource(ctx, options)
			if err != nil {
				return nil, err
			}
			h.recordSuccess(resource, true)
			return newObservedWatch(watcher, func() { h.recordSuccess(resource, true) }), nil
		},
	}
}

func (h *informerHealth) recordSuccess(resource string, watching bool) {
	h.mu.Lock()
	state, ok := h.resources[resource]
	if !ok {
		h.mu.Unlock()
		return
	}
	state.lastSuccess = h.now()
	recovered := watching && !state.degradedSince.IsZero()
	if recovered {
		state.degradedSince = time.Time{}
	}
	h.mu.Unlock()

	if recovered {
		log.Printf("NorthScope watch for %s recovered", resource)
		h.changed()
	}
}

func (h *informerHealth) recordError(resource string, err error) {
	if err == nil || isTransientWatchClose(err) {
		return
	}

	h.mu.Lock()
	state, ok := h.resources[resource]
	if !ok {
		h.mu.Unlock()
		return
	}
	now := h.now()
	state.errorCount++
	state.lastError = err.Error()
	state.lastErrorAt = now
	degraded := state.degradedSince.IsZero()
	if degraded {
		state.degradedSince = now
	}
	h.mu.Unlock()

	if degraded {
		log.Printf("NorthScope watch for %s degraded; its topology data may be stale or incomplete: %v", resource, err)
		h.changed()
	}
}

func (h *informerHealth) changed() {
	if h.onChange != nil {
		h.onChange()
	}
}

// isTransientWatchClose reports watch endings that client-go recovers from
// with an immediate relist or rewatch and logs only at debug level. Errors
// that persist, such as an unreachable API server, fail the next attempt.
func isTransientWatchClose(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, context.Canceled) ||
		apierrors.IsResourceExpired(err) ||
		apierrors.IsGone(err)
}

// resourceHealth returns every tracked resource sorted by name.
func (h *informerHealth) resourceHealth() []models.ResourceHealth {
	type entry struct {
		name  string
		state resourceHealthState
	}

	h.mu.Lock()
	entries := make([]entry, 0, len(h.resources))
	for name, state := range h.resources {
		entries = append(entries, entry{name: name, state: *state})
	}
	h.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	resources := make([]models.ResourceHealth, 0, len(entries))
	for _, item := range entries {
		state := item.state
		resources = append(resources, models.ResourceHealth{
			Resource:      item.name,
			Optional:      state.optional,
			Synced:        state.hasSynced(),
			Degraded:      !state.degradedSince.IsZero(),
			DegradedSince: optionalTime(state.degradedSince),
			LastSuccessAt: optionalTime(state.lastSuccess),
			LastError:     state.lastError,
			LastErrorAt:   optionalTime(state.lastErrorAt),
			ErrorCount:    state.errorCount,
		})
	}
	return resources
}

// unsynced returns the required resources whose initial list has not
// completed.
func (h *informerHealth) unsynced() []models.ResourceHealth {
	var unsynced []models.ResourceHealth
	for _, resource := range h.resourceHealth() {
		if !resource.Optional && !resource.Synced {
			unsynced = append(unsynced, resource)
		}
	}
	return unsynced
}

func (h *informerHealth) snapshotHealth() models.SnapshotHealth {
	health := models.SnapshotHealth{Status: models.HealthOK}
	for _, resource := range h.resourceHealth() {
		if !resource.Degraded {
			continue
		}
		health.Status = models.HealthDegraded
		health.Degraded = append(health.Degraded, models.DegradedResource{
			Resource: resource.Resource,
			Synced:   resource.Synced,
			Since:    *resource.DegradedSince,
			Error:    resource.LastError,
		})
	}
	return health
}

// sameHealth compares the parts of snapshot health that warrant publishing a
// new snapshot. Error text alone can change on every retry and is ignored.
func sameHealth(left, right models.SnapshotHealth) bool {
	if left.Status != right.Status || len(left.Degraded) != len(right.Degraded) {
		return false
	}
	for i := range left.Degraded {
		if left.Degraded[i].Resource != right.Degraded[i].Resource || left.Degraded[i].Synced != right.Degraded[i].Synced {
			return false
		}
	}
	return true
}

func degradedResourceNames(health models.SnapshotHealth) []string {
	names := make([]string, 0, len(health.Degraded))
	for _, resource := range health.Degraded {
		names = append(names, resource.Resource)
	}
	return names
}

func describeUnsynced(resources []models.ResourceHealth) string {
	parts := make([]string, 0, len(resources))
	for _, resource := range resources {
		if resource.LastError == "" {
			parts = append(parts, resource.Resource)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (last error: %s)", resource.Resource, resource.LastError))
	}
	return strings.Join(parts, ", ")
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

// observedWatch forwards watch events and reports every non-error event as
// proof that the watch is still delivering data.
type observedWatch struct {
	source   watch.Interface
	result   chan watch.Event
	done     chan struct{}
	stopOnce sync.Once
	observe  func()
}

func newObservedWatch(source watch.Interface, observe func()) *observedWatch {
	w := &observedWatch{
		source:  source,
		result:  make(chan watch.Event),
		done:    make(chan struct{}),
		observe: observe,
	}
	go w.forward()
	return w
}

func (w *observedWatch) forward() {
	defer close(w.result)
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.source.ResultChan():
			if !ok {
				return
			}
			if event.Type != watch.Error {
				w.observe()
			}
			select {
			case w.result <- event:
			case <-w.done:
				return
			}
		}
	}
}

func (w *observedWatch) Stop() {
	w.stopOnce.Do(func() {
		close(w.done)
		w.source.Stop()
	})
}

func (w *observedWatch) ResultChan() <-chan watch.Event {
	return w.result
}
