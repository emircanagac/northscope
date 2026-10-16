package k8s

import (
	"context"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/emircanagac/northscope/internal/models"
)

var httpRoutesV1 = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}

// switchableDiscovery serves a preferred resource list that a test can change,
// to simulate CRDs being installed or removed after startup.
type switchableDiscovery struct {
	*fakediscovery.FakeDiscovery
	mu        sync.Mutex
	preferred []*metav1.APIResourceList
}

func (d *switchableDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.preferred, nil
}

func (d *switchableDiscovery) set(preferred []*metav1.APIResourceList) {
	d.mu.Lock()
	d.preferred = preferred
	d.mu.Unlock()
}

func httpRouteResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{{
		GroupVersion: httpRoutesV1.GroupVersion().String(),
		APIResources: []metav1.APIResource{{Name: "httproutes", Kind: "HTTPRoute", Namespaced: true}},
	}}
}

func httpRoute(namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata":   map[string]interface{}{"namespace": namespace, "name": name},
		"spec":       map[string]interface{}{"hostnames": []interface{}{name + ".example.com"}},
	}}
}

func hasNode(snapshot models.TopologySnapshot, id string) bool {
	for _, node := range snapshot.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func countListActions(actions []clienttesting.Action, resource string) int {
	count := 0
	for _, action := range actions {
		if action.GetVerb() == "list" && action.GetResource().Resource == resource {
			count++
		}
	}
	return count
}

func newOptionalTestWatcher(t *testing.T, preferred []*metav1.APIResourceList, objects ...runtime.Object) (*Watcher, *dynamicfake.FakeDynamicClient, *switchableDiscovery) {
	t.Helper()
	client := fake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{httpRoutesV1: "HTTPRouteList"},
		objects...,
	)
	watcher, err := NewWatcherFromClients(client, dynamicClient, time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	discovery := &switchableDiscovery{FakeDiscovery: client.Discovery().(*fakediscovery.FakeDiscovery), preferred: preferred}
	watcher.discovery = discovery
	watcher.rebuildDebounce = 10 * time.Millisecond
	return watcher, dynamicClient, discovery
}

func TestWatcherReadsOptionalResourcesFromInformerCache(t *testing.T) {
	watcher, dynamicClient, _ := newOptionalTestWatcher(t, httpRouteResources(), httpRoute("web", "store"))
	runWatcher(t, watcher)

	storeID := routeNodeID("web", "store", string(ExternalKindHTTPRoute))
	eventually(t, 5*time.Second, "HTTPRoute from the informer cache", func() bool {
		return hasNode(watcher.Latest(), storeID)
	})
	listsAfterSync := countListActions(dynamicClient.Actions(), "httproutes")

	for _, name := range []string{"blog", "docs", "shop"} {
		if _, err := dynamicClient.Resource(httpRoutesV1).Namespace("web").Create(context.Background(), httpRoute("web", name), metav1.CreateOptions{}); err != nil {
			t.Fatalf("create HTTPRoute %s: %v", name, err)
		}
	}
	shopID := routeNodeID("web", "shop", string(ExternalKindHTTPRoute))
	eventually(t, 5*time.Second, "new HTTPRoutes through the watch", func() bool {
		return hasNode(watcher.Latest(), shopID)
	})

	if lists := countListActions(dynamicClient.Actions(), "httproutes"); lists != listsAfterSync {
		t.Fatalf("expected snapshot rebuilds to read the informer cache, but HTTPRoute LIST calls grew from %d to %d", listsAfterSync, lists)
	}
}

func TestWatcherStartsAndStopsOptionalInformersOnRediscovery(t *testing.T) {
	watcher, _, discovery := newOptionalTestWatcher(t, nil, httpRoute("web", "store"))
	runWatcher(t, watcher)
	eventually(t, 5*time.Second, "watcher to become ready", watcher.Ready)

	storeID := routeNodeID("web", "store", string(ExternalKindHTTPRoute))
	const resource = "httproutes.gateway.networking.k8s.io"
	if hasNode(watcher.Latest(), storeID) {
		t.Fatal("expected no HTTPRoute before its CRD is served")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	discovery.set(httpRouteResources())
	watcher.syncOptionalInformers(ctx)
	eventually(t, 5*time.Second, "HTTPRoute after the CRD appears", func() bool {
		return hasNode(watcher.Latest(), storeID)
	})
	if _, ok := watchedResource(watcher.Status(), resource); !ok {
		t.Fatalf("expected %s to be tracked after rediscovery", resource)
	}

	discovery.set(nil)
	watcher.syncOptionalInformers(ctx)
	eventually(t, 5*time.Second, "HTTPRoute to disappear after the CRD is removed", func() bool {
		return !hasNode(watcher.Latest(), storeID)
	})
	if _, ok := watchedResource(watcher.Status(), resource); ok {
		t.Fatalf("expected %s to be untracked after its CRD was removed", resource)
	}
}
