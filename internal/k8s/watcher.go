package k8s

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
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
const defaultRebuildDebounce = 250 * time.Millisecond
const optionalResourceRefreshInterval = 2 * time.Minute
const optionalResourceRequestTimeout = 15 * time.Second

type Watcher struct {
	discovery       discovery.DiscoveryInterface
	dynamicClient   dynamic.Interface
	resyncPeriod    time.Duration
	rebuildDebounce time.Duration
	gatewayAPI      bool
	f5              bool
	namespace       string

	factory               informers.SharedInformerFactory
	ingressInformer       networkinginformers.IngressInformer
	ingressClassInformer  networkinginformers.IngressClassInformer
	serviceInformer       coreinformers.ServiceInformer
	endpointInformer      coreinformers.EndpointsInformer
	endpointSliceInformer discoveryinformers.EndpointSliceInformer
	podInformer           coreinformers.PodInformer
	nodeInformer          coreinformers.NodeInformer

	mu              sync.RWMutex
	version         int64
	latest          models.TopologySnapshot
	subscribers     map[chan models.TopologySnapshot]struct{}
	ready           uint32
	rebuildRequests chan struct{}

	snapshotBuildsTotal       uint64
	snapshotBuildErrorsTotal  uint64
	snapshotPublishesTotal    uint64
	snapshotUnchangedTotal    uint64
	lastSnapshotBuildDuration time.Duration

	optionalResourceDiscoveryWarningOnce sync.Once
	optionalResourceMu                   sync.Mutex
	optionalResourceLastRefresh          time.Time
	optionalResourceGVRs                 map[schema.GroupVersionResource]struct{}
	optionalResourceCache                []ExternalResource
	optionalInformers                    []cache.SharedIndexInformer

	buildSnapshotFunc func() (models.TopologySnapshot, error)
}

type WatcherOptions struct {
	GatewayAPI bool
	F5         bool
	Namespace  string
}

func DefaultWatcherOptions() WatcherOptions {
	return WatcherOptions{GatewayAPI: true, F5: true}
}

type WatcherMetrics struct {
	Ready                            bool
	SnapshotVersion                  int64
	SnapshotNodes                    int
	SnapshotEdges                    int
	SnapshotBuildsTotal              uint64
	SnapshotBuildErrorsTotal         uint64
	SnapshotPublishesTotal           uint64
	SnapshotUnchangedTotal           uint64
	LastSnapshotBuildDurationSeconds float64
	WebsocketSubscribers             int
}

func NewWatcher(config *rest.Config) (*Watcher, error) {
	return NewWatcherWithOptions(config, DefaultWatcherOptions())
}

func NewWatcherWithOptions(config *rest.Config, options WatcherOptions) (*Watcher, error) {
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return newWatcherFromClients(client, dynamicClient, defaultResyncPeriod, options)
}

func NewWatcherFromClient(client kubernetes.Interface, resyncPeriod time.Duration) (*Watcher, error) {
	return NewWatcherFromClients(client, nil, resyncPeriod)
}

func NewWatcherFromClients(client kubernetes.Interface, dynamicClient dynamic.Interface, resyncPeriod time.Duration) (*Watcher, error) {
	return newWatcherFromClients(client, dynamicClient, resyncPeriod, DefaultWatcherOptions())
}

func newWatcherFromClients(
	client kubernetes.Interface,
	dynamicClient dynamic.Interface,
	resyncPeriod time.Duration,
	options WatcherOptions,
) (*Watcher, error) {
	if resyncPeriod == 0 {
		resyncPeriod = defaultResyncPeriod
	}

	factoryOptions := []informers.SharedInformerOption{}
	if options.Namespace != "" {
		factoryOptions = append(factoryOptions, informers.WithNamespace(options.Namespace))
	}
	factory := informers.NewSharedInformerFactoryWithOptions(client, resyncPeriod, factoryOptions...)
	w := &Watcher{
		discovery:             client.Discovery(),
		dynamicClient:         dynamicClient,
		resyncPeriod:          resyncPeriod,
		rebuildDebounce:       defaultRebuildDebounce,
		gatewayAPI:            options.GatewayAPI,
		f5:                    options.F5,
		namespace:             options.Namespace,
		factory:               factory,
		ingressInformer:       factory.Networking().V1().Ingresses(),
		ingressClassInformer:  factory.Networking().V1().IngressClasses(),
		serviceInformer:       factory.Core().V1().Services(),
		endpointInformer:      factory.Core().V1().Endpoints(),
		endpointSliceInformer: factory.Discovery().V1().EndpointSlices(),
		podInformer:           factory.Core().V1().Pods(),
		nodeInformer:          factory.Core().V1().Nodes(),
		subscribers:           make(map[chan models.TopologySnapshot]struct{}),
		rebuildRequests:       make(chan struct{}, 1),
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
		w.nodeInformer.Informer().HasSynced,
	); !ok {
		return fmt.Errorf("kubernetes informer cache sync failed")
	}

	log.Printf("NorthScope Kubernetes caches synced")
	w.startOptionalInformers(ctx)
	atomic.StoreUint32(&w.ready, 1)
	w.rebuildAndPublishContext(ctx)

	return w.runRebuildLoop(ctx)
}

func (w *Watcher) runRebuildLoop(ctx context.Context) error {
	ticker := time.NewTicker(w.resyncPeriod)
	defer ticker.Stop()

	var debounceTimer *time.Timer
	var debounceC <-chan time.Time
	stopDebounce := func() {
		if debounceTimer == nil {
			return
		}
		if !debounceTimer.Stop() {
			select {
			case <-debounceTimer.C:
			default:
			}
		}
		debounceC = nil
	}
	defer stopDebounce()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stopDebounce()
			w.rebuildAndPublishContext(ctx)
		case <-w.rebuildRequests:
			if w.rebuildDebounce <= 0 {
				w.rebuildAndPublishContext(ctx)
				continue
			}
			if debounceTimer == nil {
				debounceTimer = time.NewTimer(w.rebuildDebounce)
			} else {
				stopDebounce()
				debounceTimer.Reset(w.rebuildDebounce)
			}
			debounceC = debounceTimer.C
		case <-debounceC:
			debounceC = nil
			w.rebuildAndPublishContext(ctx)
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
		SnapshotPublishesTotal:           w.snapshotPublishesTotal,
		SnapshotUnchangedTotal:           w.snapshotUnchangedTotal,
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
	if !w.latest.GeneratedAt.IsZero() {
		ch <- w.latest
	}
	w.mu.Unlock()

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
	if _, err := w.nodeInformer.Informer().AddEventHandler(handler); err != nil {
		return err
	}

	return nil
}

func (w *Watcher) rebuildAndPublishWhenReady() {
	if atomic.LoadUint32(&w.ready) == 0 {
		return
	}
	select {
	case w.rebuildRequests <- struct{}{}:
	default:
	}
}

func (w *Watcher) rebuildAndPublish() {
	w.rebuildAndPublishContext(context.Background())
}

func (w *Watcher) rebuildAndPublishContext(ctx context.Context) {
	started := time.Now()
	snapshot, err := w.nextSnapshot(ctx)
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
	if !firstSnapshot && sameTopology(w.latest, snapshot) {
		w.snapshotUnchangedTotal++
		w.mu.Unlock()
		return
	}
	w.version++
	snapshot.Version = w.version
	w.latest = snapshot
	w.snapshotPublishesTotal++

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

func (w *Watcher) nextSnapshot(ctx context.Context) (models.TopologySnapshot, error) {
	if w.buildSnapshotFunc != nil {
		return w.buildSnapshotFunc()
	}
	return w.buildSnapshot(ctx)
}

func (w *Watcher) buildSnapshot(ctx context.Context) (models.TopologySnapshot, error) {
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
	nodes, err := w.nodeInformer.Lister().List(labels.Everything())
	if err != nil {
		return models.TopologySnapshot{}, err
	}
	externalResources := w.optionalExternalResources(ctx)
	scoped := scopeTrafficInputs(ingresses, services, pods, nodes, externalResources, endpoints, endpointSlices)
	gatewayClasses, gateways, gatewayRoutes, f5Resources := externalResourceInventory(externalResources)

	snapshot := BuildTopologyWithResourcesAndEndpoints(
		ingresses,
		ingressClasses,
		scoped.services,
		scoped.pods,
		scoped.nodes,
		externalResources,
		scoped.endpoints,
		scoped.endpointSlices,
	)
	snapshot.Inventory = models.ClusterInventory{
		IngressClasses: len(ingressClasses),
		Ingresses:      len(ingresses),
		GatewayClasses: gatewayClasses,
		Gateways:       gateways,
		GatewayRoutes:  gatewayRoutes,
		F5Resources:    f5Resources,
		Services:       len(services),
		Pods:           len(pods),
		Nodes:          len(nodes),
	}
	return ingressScopedSnapshot(snapshot), nil
}

func externalResourceInventory(resources []ExternalResource) (gatewayClasses, gateways, gatewayRoutes, f5Resources int) {
	for _, resource := range resources {
		switch resource.Kind {
		case ExternalKindGatewayClass:
			gatewayClasses++
		case ExternalKindGateway:
			gateways++
		case ExternalKindHTTPRoute, ExternalKindGRPCRoute, ExternalKindTLSRoute, ExternalKindTCPRoute, ExternalKindUDPRoute:
			gatewayRoutes++
		case ExternalKindF5IngressLink, ExternalKindF5Virtual, ExternalKindF5Transport:
			f5Resources++
		}
	}
	return
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

	requestCtx, cancel := context.WithTimeout(ctx, optionalResourceRequestTimeout)
	defer cancel()
	resources, complete := listOptionalExternalResources(requestCtx, w.dynamicClient, availableGVRs, w.namespace)
	w.optionalResourceGVRs = availableGVRs
	if complete || w.optionalResourceCache == nil {
		w.optionalResourceCache = append([]ExternalResource(nil), resources...)
	}
	w.optionalResourceLastRefresh = now
	return append([]ExternalResource(nil), w.optionalResourceCache...)
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
	for _, item := range w.preferredEnabledOptionalResources(served) {
		available[item.gvr] = struct{}{}
	}
	return available, nil
}

func (w *Watcher) startOptionalInformers(ctx context.Context) {
	if w.dynamicClient == nil {
		return
	}

	available, err := w.availableOptionalResourceGVRs()
	if err != nil {
		w.optionalResourceDiscoveryWarningOnce.Do(func() {
			log.Printf("optional Gateway/F5 watches disabled; API resource discovery failed: %v", err)
		})
		return
	}

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(interface{}) { w.optionalResourceChanged() },
		UpdateFunc: func(interface{}, interface{}) { w.optionalResourceChanged() },
		DeleteFunc: func(interface{}) { w.optionalResourceChanged() },
	}
	for _, item := range w.preferredEnabledOptionalResources(available) {
		namespace := w.namespace
		if namespace == "" {
			namespace = metav1.NamespaceAll
		}
		if item.scope == "cluster" {
			namespace = metav1.NamespaceAll
		}
		informer := dynamicinformer.NewFilteredDynamicInformer(
			w.dynamicClient,
			item.gvr,
			namespace,
			w.resyncPeriod,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			nil,
		).Informer()
		if _, err := informer.AddEventHandler(handler); err != nil {
			log.Printf("register optional topology watch %s failed: %v", item.gvr.String(), err)
			continue
		}
		w.optionalInformers = append(w.optionalInformers, informer)
		go informer.Run(ctx.Done())
	}
}

func (w *Watcher) preferredEnabledOptionalResources(available map[schema.GroupVersionResource]struct{}) []optionalResource {
	resources := preferredOptionalResources(available)
	filtered := make([]optionalResource, 0, len(resources))
	for _, item := range resources {
		switch item.gvr.Group {
		case "gateway.networking.k8s.io":
			if !w.gatewayAPI {
				continue
			}
		case "cis.f5.com":
			if !w.f5 {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func (w *Watcher) optionalResourceChanged() {
	w.optionalResourceMu.Lock()
	w.optionalResourceLastRefresh = time.Time{}
	w.optionalResourceMu.Unlock()
	w.rebuildAndPublishWhenReady()
}

func (w *Watcher) unsubscribe(ch chan models.TopologySnapshot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.subscribers, ch)
	close(ch)
}
