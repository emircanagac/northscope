package k8s

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// typedInformer describes one typed resource NorthScope caches.
type typedInformer struct {
	resource schema.GroupResource
	object   runtime.Object
	list     func(context.Context, kubernetes.Interface, metav1.ListOptions) (runtime.Object, error)
	watch    func(context.Context, kubernetes.Interface, metav1.ListOptions) (watch.Interface, error)
}

func topologyInformers(namespace, endpointSliceAPI string) []typedInformer {
	resources := []typedInformer{
		{
			resource: schema.GroupResource{Group: networkingv1.GroupName, Resource: "ingresses"},
			object:   &networkingv1.Ingress{},
			list: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (runtime.Object, error) {
				return client.NetworkingV1().Ingresses(namespace).List(ctx, options)
			},
			watch: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (watch.Interface, error) {
				return client.NetworkingV1().Ingresses(namespace).Watch(ctx, options)
			},
		},
		{
			resource: schema.GroupResource{Group: networkingv1.GroupName, Resource: "ingressclasses"},
			object:   &networkingv1.IngressClass{},
			list: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (runtime.Object, error) {
				return client.NetworkingV1().IngressClasses().List(ctx, options)
			},
			watch: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (watch.Interface, error) {
				return client.NetworkingV1().IngressClasses().Watch(ctx, options)
			},
		},
		{
			resource: schema.GroupResource{Group: corev1.GroupName, Resource: "services"},
			object:   &corev1.Service{},
			list: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Services(namespace).List(ctx, options)
			},
			watch: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Services(namespace).Watch(ctx, options)
			},
		},
		{
			resource: schema.GroupResource{Group: corev1.GroupName, Resource: "endpoints"},
			object:   &corev1.Endpoints{},
			list: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Endpoints(namespace).List(ctx, options)
			},
			watch: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Endpoints(namespace).Watch(ctx, options)
			},
		},
		{
			resource: schema.GroupResource{Group: corev1.GroupName, Resource: "pods"},
			object:   &corev1.Pod{},
			list: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Pods(namespace).List(ctx, options)
			},
			watch: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Pods(namespace).Watch(ctx, options)
			},
		},
		{
			resource: schema.GroupResource{Group: corev1.GroupName, Resource: "nodes"},
			object:   &corev1.Node{},
			list: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Nodes().List(ctx, options)
			},
			watch: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Nodes().Watch(ctx, options)
			},
		},
	}

	endpointSlices := schema.GroupResource{Group: discoveryv1.GroupName, Resource: endpointSliceResource}
	switch endpointSliceAPI {
	case endpointSliceV1:
		resources = append(resources, typedInformer{
			resource: endpointSlices,
			object:   &discoveryv1.EndpointSlice{},
			list: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (runtime.Object, error) {
				return client.DiscoveryV1().EndpointSlices(namespace).List(ctx, options)
			},
			watch: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (watch.Interface, error) {
				return client.DiscoveryV1().EndpointSlices(namespace).Watch(ctx, options)
			},
		})
	case endpointSliceV1beta1:
		resources = append(resources, typedInformer{
			resource: endpointSlices,
			object:   &discoveryv1beta1.EndpointSlice{},
			list: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (runtime.Object, error) {
				return client.DiscoveryV1beta1().EndpointSlices(namespace).List(ctx, options)
			},
			watch: func(ctx context.Context, client kubernetes.Interface, options metav1.ListOptions) (watch.Interface, error) {
				return client.DiscoveryV1beta1().EndpointSlices(namespace).Watch(ctx, options)
			},
		})
	}
	return resources
}

// registerTypedInformer makes the factory build the resource's informer with
// observed list and watch calls. Generated accessors such as
// factory.Core().V1().Pods() look informers up by object type, so they return
// this instance and their listers read its cache.
func registerTypedInformer(factory informers.SharedInformerFactory, health *informerHealth, item typedInformer) error {
	name := item.resource.String()
	informer := factory.InformerFor(item.object, func(client kubernetes.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
		lw := health.listWatch(
			name,
			func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
				return item.list(ctx, client, options)
			},
			func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				return item.watch(ctx, client, options)
			},
		)
		return cache.NewSharedIndexInformerWithOptions(
			cache.ToListWatcherWithWatchListSemantics(lw, client),
			item.object,
			cache.SharedIndexInformerOptions{
				ResyncPeriod: resyncPeriod,
				Indexers:     cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			},
		)
	})
	return health.track(name, informer, false)
}

// newOptionalInformer builds a Gateway API or F5 informer with observed list
// and watch calls.
func (w *Watcher) newOptionalInformer(item optionalResource, namespace string) cache.SharedIndexInformer {
	resourceClient := w.dynamicClient.Resource(item.gvr).Namespace(namespace)
	lw := w.health.listWatch(
		item.gvr.GroupResource().String(),
		func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			return resourceClient.List(ctx, options)
		},
		resourceClient.Watch,
	)
	return cache.NewSharedIndexInformerWithOptions(
		cache.ToListWatcherWithWatchListSemantics(lw, w.dynamicClient),
		&unstructured.Unstructured{},
		cache.SharedIndexInformerOptions{
			ResyncPeriod:      w.resyncPeriod,
			Indexers:          cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			ObjectDescription: item.gvr.String(),
		},
	)
}
