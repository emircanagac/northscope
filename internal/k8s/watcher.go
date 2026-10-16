package k8s

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	networkinginformers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/emircanagac/northscope/internal/models"
)

const defaultResyncPeriod = 10 * time.Minute
const defaultRebuildDebounce = 250 * time.Millisecond
const optionalResourceDiscoveryInterval = 2 * time.Minute
const cacheSyncPollInterval = 100 * time.Millisecond
const defaultCacheSyncReportInterval = 30 * time.Second

type Watcher struct {
	discovery       discovery.DiscoveryInterface
	dynamicClient   dynamic.Interface
	resyncPeriod    time.Duration
	rebuildDebounce time.Duration
	gatewayAPI      bool
	f5              bool
	namespace       string

	factory              informers.SharedInformerFactory
	ingressInformer      networkinginformers.IngressInformer
	ingressClassInformer networkinginformers.IngressClassInformer
	serviceInformer      coreinformers.ServiceInformer
	endpointInformer     coreinformers.EndpointsInformer
	endpointSlices       *endpointSliceSource
	podInformer          coreinformers.PodInformer
	nodeInformer         coreinformers.NodeInformer

	health                  *informerHealth
	cacheSyncReportInterval time.Duration

	mu              sync.RWMutex
	instance        string
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
	optionalInformers                    map[schema.GroupVersionResource]optionalInformer

	buildSnapshotFunc func() (models.TopologySnapshot, error)
}

// optionalInformer is a running Gateway API or F5 informer.
type optionalInformer struct {
	item     optionalResource
	informer cache.SharedIndexInformer
	stop     func()
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
	SnapshotGeneratedAt              time.Time
	WebsocketSubscribers             int
	Resources                        []models.ResourceHealth
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

	factoryOptions := []informers.SharedInformerOption{informers.WithTransform(stripManagedFields)}
	if options.Namespace != "" {
		factoryOptions = append(factoryOptions, informers.WithNamespace(options.Namespace))
	}
	endpointSliceAPI, err := detectEndpointSliceAPI(client.Discovery())
	if err != nil {
		return nil, err
	}
	if endpointSliceAPI == "" {
		log.Printf("NorthScope compatibility: EndpointSlice API is not served; using legacy Endpoints only")
	} else {
		log.Printf("NorthScope compatibility: using EndpointSlice API %s", endpointSliceAPI)
	}

	factory := informers.NewSharedInformerFactoryWithOptions(client, resyncPeriod, factoryOptions...)
	health := newInformerHealth()
	for _, item := range topologyInformers(options.Namespace, endpointSliceAPI) {
		if err := registerTypedInformer(factory, health, item); err != nil {
			return nil, err
		}
	}
	w := &Watcher{
		discovery:            client.Discovery(),
		dynamicClient:        dynamicClient,
		resyncPeriod:         resyncPeriod,
		rebuildDebounce:      defaultRebuildDebounce,
		gatewayAPI:           options.GatewayAPI,
		f5:                   options.F5,
		namespace:            options.Namespace,
		factory:              factory,
		ingressInformer:      factory.Networking().V1().Ingresses(),
		ingressClassInformer: factory.Networking().V1().IngressClasses(),
		serviceInformer:      factory.Core().V1().Services(),
		endpointInformer:     factory.Core().V1().Endpoints(),
		endpointSlices:       newEndpointSliceSource(factory, endpointSliceAPI),
		podInformer:          factory.Core().V1().Pods(),
		nodeInformer:         factory.Core().V1().Nodes(),
		health:               health,
		instance:             rand.Text(),
		subscribers:          make(map[chan models.TopologySnapshot]struct{}),
		rebuildRequests:      make(chan struct{}, 1),

		cacheSyncReportInterval: defaultCacheSyncReportInterval,
	}
	health.onChange = w.rebuildAndPublishWhenReady

	if err := w.registerHandlers(); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *Watcher) Run(ctx context.Context) error {
	w.factory.Start(ctx.Done())

	if err := w.waitForCacheSync(ctx); err != nil {
		return err
	}

	log.Printf("NorthScope Kubernetes caches synced")
	w.startOptionalInformers(ctx)
	atomic.StoreUint32(&w.ready, 1)
	w.rebuildAndPublishContext(ctx)

	return w.runRebuildLoop(ctx)
}

// waitForCacheSync blocks until every typed informer has listed its resource
// once. A missing RBAC rule or an unreachable API server would otherwise block
// startup silently, so it periodically logs the informers that are still
// unsynced together with their latest list or watch error.
func (w *Watcher) waitForCacheSync(ctx context.Context) error {
	started := time.Now()
	poll := time.NewTicker(cacheSyncPollInterval)
	defer poll.Stop()
	report := time.NewTicker(w.cacheSyncReportInterval)
	defer report.Stop()

	for {
		unsynced := w.health.unsynced()
		if len(unsynced) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-poll.C:
		case <-report.C:
			log.Printf(
				"NorthScope still waiting for Kubernetes caches after %s; unsynced: %s",
				time.Since(started).Round(time.Second),
				describeUnsynced(unsynced),
			)
		}
	}
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
	resources := w.health.resourceHealth()
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
		SnapshotGeneratedAt:              w.latest.GeneratedAt,
		WebsocketSubscribers:             len(w.subscribers),
		Resources:                        resources,
	}
}

// Status reports readiness and per-resource watch health. Degraded resources
// do not affect readiness: once the first snapshot exists NorthScope keeps
// serving its cache and reports which parts may be stale.
func (w *Watcher) Status() models.WatcherStatus {
	resources := w.health.resourceHealth()
	w.mu.RLock()
	ready := atomic.LoadUint32(&w.ready) == 1 && !w.latest.GeneratedAt.IsZero()
	status := models.WatcherStatus{
		Ready:           ready,
		SnapshotVersion: w.latest.Version,
		GeneratedAt:     optionalTime(w.latest.GeneratedAt),
		Resources:       resources,
	}
	w.mu.RUnlock()

	for _, resource := range resources {
		if resource.Degraded {
			status.Degraded = append(status.Degraded, resource.Resource)
		}
		if !resource.Synced {
			status.Unsynced = append(status.Unsynced, resource.Resource)
		}
	}
	switch {
	case len(status.Degraded) > 0:
		status.Status = models.HealthDegraded
	case !ready:
		status.Status = models.HealthSyncing
	default:
		status.Status = models.HealthOK
	}
	return status
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
	if w.endpointSlices != nil {
		if _, err := w.endpointSlices.informer.AddEventHandler(handler); err != nil {
			return err
		}
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
	snapshot.Health = w.health.snapshotHealth()

	w.mu.Lock()
	firstSnapshot := w.latest.GeneratedAt.IsZero()
	w.snapshotBuildsTotal++
	w.lastSnapshotBuildDuration = time.Since(started)
	if !firstSnapshot && sameTopology(w.latest, snapshot) && sameHealth(w.latest.Health, snapshot.Health) {
		w.snapshotUnchangedTotal++
		w.mu.Unlock()
		return
	}
	w.version++
	snapshot.Instance = w.instance
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
		degraded := ""
		if snapshot.Health.Status == models.HealthDegraded {
			degraded = fmt.Sprintf(" (degraded: %s)", strings.Join(degradedResourceNames(snapshot.Health), ", "))
		}
		log.Printf(
			"NorthScope ready: snapshot v%d, %d nodes, %d edges%s",
			snapshot.Version,
			len(snapshot.Nodes),
			len(snapshot.Edges),
			degraded,
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
	var endpointSlices []*discoveryv1.EndpointSlice
	if w.endpointSlices != nil {
		endpointSlices, err = w.endpointSlices.list()
		if err != nil {
			return models.TopologySnapshot{}, err
		}
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
	externalResources := w.optionalExternalResources()
	scoped := scopeTrafficInputs(ingresses, services, pods, nodes, externalResources, endpoints, endpointSlices)
	gatewayClasses, gateways, gatewayRoutes, f5Resources := externalResourceInventory(externalResources)

	snapshot := BuildTopologyWithOptions(
		TopologyOptions{WatchNamespace: w.namespace},
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

// optionalExternalResources reads Gateway API and F5 resources from the
// optional informer caches. Informers that have not finished their first list
// are skipped so a partial cache never looks complete.
func (w *Watcher) optionalExternalResources() []ExternalResource {
	w.optionalResourceMu.Lock()
	informers := make([]optionalInformer, 0, len(w.optionalInformers))
	for _, informer := range w.optionalInformers {
		informers = append(informers, informer)
	}
	w.optionalResourceMu.Unlock()
	sort.Slice(informers, func(i, j int) bool {
		return informers[i].item.gvr.String() < informers[j].item.gvr.String()
	})

	var resources []ExternalResource
	for _, informer := range informers {
		if !informer.informer.HasSynced() {
			continue
		}
		apiVersion := informer.item.gvr.GroupVersion().String()
		for _, obj := range informer.informer.GetStore().List() {
			item, ok := obj.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			if resource, ok := externalResourceFromUnstructured(item, informer.item.kind, apiVersion); ok {
				resources = append(resources, resource)
			}
		}
	}
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
	for _, item := range w.preferredEnabledOptionalResources(served) {
		available[item.gvr] = struct{}{}
	}
	return available, nil
}

// startOptionalInformers watches the Gateway API and F5 resources that are
// served now, then rediscovers them periodically so CRDs installed, removed, or
// upgraded to a new preferred version after startup are picked up.
func (w *Watcher) startOptionalInformers(ctx context.Context) {
	if w.dynamicClient == nil {
		return
	}
	w.syncOptionalInformers(ctx)
	go func() {
		ticker := time.NewTicker(optionalResourceDiscoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.syncOptionalInformers(ctx)
			}
		}
	}()
}

// syncOptionalInformers starts informers for newly served optional resources
// and stops the ones that are no longer served or no longer preferred. A failed
// discovery keeps the current informers.
func (w *Watcher) syncOptionalInformers(ctx context.Context) {
	available, err := w.availableOptionalResourceGVRs()
	if err != nil {
		w.optionalResourceDiscoveryWarningOnce.Do(func() {
			log.Printf("optional Gateway/F5 discovery failed; keeping current watches and retrying every %s: %v", optionalResourceDiscoveryInterval, err)
		})
		return
	}
	wanted := map[schema.GroupVersionResource]optionalResource{}
	for _, item := range w.preferredEnabledOptionalResources(available) {
		wanted[item.gvr] = item
	}

	w.optionalResourceMu.Lock()
	defer w.optionalResourceMu.Unlock()
	if w.optionalInformers == nil {
		w.optionalInformers = map[schema.GroupVersionResource]optionalInformer{}
	}
	changed := false
	for gvr, running := range w.optionalInformers {
		if _, ok := wanted[gvr]; ok {
			continue
		}
		running.stop()
		delete(w.optionalInformers, gvr)
		w.health.untrack(gvr.GroupResource().String())
		log.Printf("stopped optional topology watch %s; it is no longer served", gvr.String())
		changed = true
	}
	for gvr, item := range wanted {
		if _, ok := w.optionalInformers[gvr]; ok {
			continue
		}
		if running, err := w.runOptionalInformer(ctx, item); err != nil {
			log.Printf("register optional topology watch %s failed: %v", gvr.String(), err)
		} else {
			w.optionalInformers[gvr] = running
		}
	}
	if changed {
		w.rebuildAndPublishWhenReady()
	}
}

func (w *Watcher) runOptionalInformer(ctx context.Context, item optionalResource) (optionalInformer, error) {
	namespace := w.namespace
	if item.scope == "cluster" || namespace == "" {
		namespace = metav1.NamespaceAll
	}
	informer := w.newOptionalInformer(item, namespace)
	if err := informer.SetTransform(stripManagedFields); err != nil {
		return optionalInformer{}, err
	}
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(interface{}) { w.rebuildAndPublishWhenReady() },
		UpdateFunc: func(interface{}, interface{}) { w.rebuildAndPublishWhenReady() },
		DeleteFunc: func(interface{}) { w.rebuildAndPublishWhenReady() },
	}
	if _, err := informer.AddEventHandler(handler); err != nil {
		return optionalInformer{}, err
	}
	if err := w.health.track(item.gvr.GroupResource().String(), informer, true); err != nil {
		return optionalInformer{}, err
	}
	stopCh := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(stopCh) }) }
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-stopCh:
		}
	}()
	go informer.Run(stopCh)
	return optionalInformer{item: item, informer: informer, stop: stop}, nil
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

// stripManagedFields drops server-side apply bookkeeping before objects enter
// informer caches. NorthScope never reads it, and on large clusters it is a
// significant share of cached Pod, Service, and Endpoint memory.
func stripManagedFields(obj interface{}) (interface{}, error) {
	if accessor, err := meta.Accessor(obj); err == nil {
		accessor.SetManagedFields(nil)
	}
	return obj, nil
}

func (w *Watcher) unsubscribe(ch chan models.TopologySnapshot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.subscribers, ch)
	close(ch)
}
