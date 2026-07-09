package k8s

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	discoveryinformers "k8s.io/client-go/informers/discovery/v1"
	networkinginformers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/emircanagac/northscope/internal/models"
)

const defaultResyncPeriod = 10 * time.Minute
const optionalResourceRefreshInterval = 2 * time.Minute

type Watcher struct {
	client        kubernetes.Interface
	discovery     discovery.DiscoveryInterface
	dynamicClient dynamic.Interface
	resyncPeriod  time.Duration

	factory               informers.SharedInformerFactory
	ingressInformer       networkinginformers.IngressInformer
	ingressClassInformer  networkinginformers.IngressClassInformer
	serviceInformer       coreinformers.ServiceInformer
	endpointInformer      coreinformers.EndpointsInformer
	endpointSliceInformer discoveryinformers.EndpointSliceInformer
	podInformer           coreinformers.PodInformer

	mu          sync.RWMutex
	version     int64
	latest      models.TopologySnapshot
	subscribers map[chan models.TopologySnapshot]struct{}
	ready       uint32

	snapshotBuildsTotal       uint64
	snapshotBuildErrorsTotal  uint64
	lastSnapshotBuildDuration time.Duration

	nodeListWarningOnce                  sync.Once
	optionalResourceDiscoveryWarningOnce sync.Once
	optionalResourceMu                   sync.Mutex
	optionalResourceLastRefresh          time.Time
	optionalResourceGVRs                 map[schema.GroupVersionResource]struct{}
	optionalResourceCache                []ExternalResource

	buildSnapshotFunc func() (models.TopologySnapshot, error)
}

type WatcherMetrics struct {
	Ready                            bool
	SnapshotVersion                  int64
	SnapshotNodes                    int
	SnapshotEdges                    int
	SnapshotBuildsTotal              uint64
	SnapshotBuildErrorsTotal         uint64
	LastSnapshotBuildDurationSeconds float64
	WebsocketSubscribers             int
}

func NewWatcher(config *rest.Config) (*Watcher, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return NewWatcherFromClients(client, dynamicClient, defaultResyncPeriod)
}

func NewWatcherFromClient(client kubernetes.Interface, resyncPeriod time.Duration) (*Watcher, error) {
	return NewWatcherFromClients(client, nil, resyncPeriod)
}

func NewWatcherFromClients(client kubernetes.Interface, dynamicClient dynamic.Interface, resyncPeriod time.Duration) (*Watcher, error) {
	if resyncPeriod == 0 {
		resyncPeriod = defaultResyncPeriod
	}

	factory := informers.NewSharedInformerFactory(client, resyncPeriod)
	w := &Watcher{
		client:                client,
		discovery:             client.Discovery(),
		dynamicClient:         dynamicClient,
		resyncPeriod:          resyncPeriod,
		factory:               factory,
		ingressInformer:       factory.Networking().V1().Ingresses(),
		ingressClassInformer:  factory.Networking().V1().IngressClasses(),
		serviceInformer:       factory.Core().V1().Services(),
		endpointInformer:      factory.Core().V1().Endpoints(),
		endpointSliceInformer: factory.Discovery().V1().EndpointSlices(),
		podInformer:           factory.Core().V1().Pods(),
		subscribers:           make(map[chan models.TopologySnapshot]struct{}),
	}

	if err := w.registerHandlers(); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *Watcher) Run(ctx context.Context) error {
	w.factory.Start(ctx.Done())

	if ok := cache.WaitForCacheSync(
		ctx.Done(),
		w.ingressInformer.Informer().HasSynced,
		w.ingressClassInformer.Informer().HasSynced,
		w.serviceInformer.Informer().HasSynced,
		w.endpointInformer.Informer().HasSynced,
		w.endpointSliceInformer.Informer().HasSynced,
		w.podInformer.Informer().HasSynced,
	); !ok {
		return fmt.Errorf("kubernetes informer cache sync failed")
	}

	log.Printf("NorthScope Kubernetes caches synced")
	atomic.StoreUint32(&w.ready, 1)
	w.rebuildAndPublish()

	ticker := time.NewTicker(w.resyncPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.rebuildAndPublish()
		}
	}
}

func (w *Watcher) Latest() models.TopologySnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.latest
}

func (w *Watcher) Ready() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return atomic.LoadUint32(&w.ready) == 1 && !w.latest.GeneratedAt.IsZero()
}

func (w *Watcher) Metrics() WatcherMetrics {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return WatcherMetrics{
		Ready:                            atomic.LoadUint32(&w.ready) == 1 && !w.latest.GeneratedAt.IsZero(),
		SnapshotVersion:                  w.version,
		SnapshotNodes:                    len(w.latest.Nodes),
		SnapshotEdges:                    len(w.latest.Edges),
		SnapshotBuildsTotal:              w.snapshotBuildsTotal,
		SnapshotBuildErrorsTotal:         w.snapshotBuildErrorsTotal,
		LastSnapshotBuildDurationSeconds: w.lastSnapshotBuildDuration.Seconds(),
		WebsocketSubscribers:             len(w.subscribers),
	}
}

func (w *Watcher) Subscribe(buffer int) (<-chan models.TopologySnapshot, func()) {
	if buffer < 1 {
		buffer = 1
	}

	ch := make(chan models.TopologySnapshot, buffer)

	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	latest := w.latest
	w.mu.Unlock()

	if latest.GeneratedAt.IsZero() {
		return ch, func() { w.unsubscribe(ch) }
	}

	ch <- latest
	return ch, func() { w.unsubscribe(ch) }
}

func (w *Watcher) registerHandlers() error {
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { w.rebuildAndPublishWhenReady() },
		UpdateFunc: func(oldObj, newObj interface{}) { w.rebuildAndPublishWhenReady() },
		DeleteFunc: func(obj interface{}) { w.rebuildAndPublishWhenReady() },
	}

	if _, err := w.ingressInformer.Informer().AddEventHandler(handler); err != nil {
		return err
	}
	if _, err := w.ingressClassInformer.Informer().AddEventHandler(handler); err != nil {
		return err
	}
	if _, err := w.serviceInformer.Informer().AddEventHandler(handler); err != nil {
		return err
	}
	if _, err := w.endpointInformer.Informer().AddEventHandler(handler); err != nil {
		return err
	}
	if _, err := w.endpointSliceInformer.Informer().AddEventHandler(handler); err != nil {
		return err
	}
	if _, err := w.podInformer.Informer().AddEventHandler(handler); err != nil {
		return err
	}

	return nil
}

func (w *Watcher) rebuildAndPublishWhenReady() {
	if atomic.LoadUint32(&w.ready) == 0 {
		return
	}
	w.rebuildAndPublish()
}

func (w *Watcher) rebuildAndPublish() {
	started := time.Now()
	snapshot, err := w.nextSnapshot()
	if err != nil {
		w.mu.Lock()
		w.snapshotBuildErrorsTotal++
		w.lastSnapshotBuildDuration = time.Since(started)
		w.mu.Unlock()
		log.Printf("build topology snapshot failed: %v", err)
		return
	}

	w.mu.Lock()
	firstSnapshot := w.latest.GeneratedAt.IsZero()
	w.snapshotBuildsTotal++
	w.lastSnapshotBuildDuration = time.Since(started)
	w.version++
	snapshot.Version = w.version
	w.latest = snapshot

	for ch := range w.subscribers {
		select {
		case ch <- snapshot:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- snapshot
		}
	}
	w.mu.Unlock()

	if firstSnapshot {
		log.Printf(
			"NorthScope ready: snapshot v%d, %d nodes, %d edges",
			snapshot.Version,
			len(snapshot.Nodes),
			len(snapshot.Edges),
		)
	}
}

func (w *Watcher) nextSnapshot() (models.TopologySnapshot, error) {
	if w.buildSnapshotFunc != nil {
		return w.buildSnapshotFunc()
	}
	return w.buildSnapshot()
}

func (w *Watcher) buildSnapshot() (models.TopologySnapshot, error) {
	ingresses, err := w.ingressInformer.Lister().List(labels.Everything())
	if err != nil {
		return models.TopologySnapshot{}, err
	}
	ingressClasses, err := w.ingressClassInformer.Lister().List(labels.Everything())
	if err != nil {
		return models.TopologySnapshot{}, err
	}
	services, err := w.serviceInformer.Lister().List(labels.Everything())
	if err != nil {
		return models.TopologySnapshot{}, err
	}
	endpointSlices, err := w.endpointSliceInformer.Lister().List(labels.Everything())
	if err != nil {
		return models.TopologySnapshot{}, err
	}
	endpoints, err := w.endpointInformer.Lister().List(labels.Everything())
	if err != nil {
		return models.TopologySnapshot{}, err
	}
	pods, err := w.podInformer.Lister().List(labels.Everything())
	if err != nil {
		return models.TopologySnapshot{}, err
	}
	nodes := w.listNodes(context.Background())
	externalResources := w.optionalExternalResources(context.Background())

	return BuildTopologyWithResourcesAndEndpoints(ingresses, ingressClasses, services, pods, nodes, externalResources, endpoints, endpointSlices), nil
}

func (w *Watcher) optionalExternalResources(ctx context.Context) []ExternalResource {
	if w.dynamicClient == nil {
		return nil
	}

	w.optionalResourceMu.Lock()
	defer w.optionalResourceMu.Unlock()

	now := time.Now()
	if !w.optionalResourceLastRefresh.IsZero() && now.Sub(w.optionalResourceLastRefresh) < optionalResourceRefreshInterval {
		return append([]ExternalResource(nil), w.optionalResourceCache...)
	}

	availableGVRs, err := w.availableOptionalResourceGVRs()
	if err != nil {
		w.optionalResourceDiscoveryWarningOnce.Do(func() {
			log.Printf("optional Gateway/F5 discovery disabled; API resource discovery failed: %v", err)
		})
		availableGVRs = w.optionalResourceGVRs
		if availableGVRs == nil {
			availableGVRs = map[schema.GroupVersionResource]struct{}{}
		}
	}

	resources := listOptionalExternalResources(ctx, w.dynamicClient, availableGVRs)
	w.optionalResourceGVRs = availableGVRs
	w.optionalResourceCache = append([]ExternalResource(nil), resources...)
	w.optionalResourceLastRefresh = now
	return resources
}

func (w *Watcher) availableOptionalResourceGVRs() (map[schema.GroupVersionResource]struct{}, error) {
	if w.discovery == nil {
		return nil, nil
	}

	resourceLists, err := w.discovery.ServerPreferredResources()
	if err != nil && len(resourceLists) == 0 {
		return nil, err
	}

	served := make(map[schema.GroupVersionResource]struct{})
	for _, resourceList := range resourceLists {
		gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range resourceList.APIResources {
			if resource.Name == "" || resource.Kind == "" {
				continue
			}
			served[schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: resource.Name,
			}] = struct{}{}
		}
	}

	available := make(map[schema.GroupVersionResource]struct{})
	for _, item := range optionalTopologyResources {
		if _, ok := served[item.gvr]; ok {
			available[item.gvr] = struct{}{}
		}
	}
	return available, nil
}

func (w *Watcher) listNodes(ctx context.Context) []*corev1.Node {
	list, err := w.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) || apierrors.IsNotFound(err) {
			w.nodeListWarningOnce.Do(func() {
				log.Printf("node topology disabled; list nodes failed: %v", err)
			})
			return nil
		}
		log.Printf("list nodes failed: %v", err)
		return nil
	}

	nodes := make([]*corev1.Node, 0, len(list.Items))
	for i := range list.Items {
		nodes = append(nodes, &list.Items[i])
	}
	return nodes
}

func (w *Watcher) unsubscribe(ch chan models.TopologySnapshot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.subscribers, ch)
	close(ch)
}
