package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/emircanagac/northscope/internal/models"
)

func endpointSliceResourceList(groupVersion string, resources ...string) *metav1.APIResourceList {
	list := &metav1.APIResourceList{GroupVersion: groupVersion}
	for _, resource := range resources {
		list.APIResources = append(list.APIResources, metav1.APIResource{Name: resource, Namespaced: true})
	}
	return list
}

func clientWithServedResources(resources []*metav1.APIResourceList, objects ...runtime.Object) *fake.Clientset {
	client := fake.NewSimpleClientset(objects...)
	client.Discovery().(*fakediscovery.FakeDiscovery).Resources = resources
	return client
}

func TestDetectEndpointSliceAPIPrefersServedVersion(t *testing.T) {
	tests := []struct {
		name      string
		resources []*metav1.APIResourceList
		expected  string
	}{
		{
			name: "Kubernetes 1.21 to 1.24 serves both versions",
			resources: []*metav1.APIResourceList{
				endpointSliceResourceList(endpointSliceV1beta1, endpointSliceResource),
				endpointSliceResourceList(endpointSliceV1, endpointSliceResource),
			},
			expected: endpointSliceV1,
		},
		{
			name:      "Kubernetes 1.25 and newer serves v1",
			resources: []*metav1.APIResourceList{endpointSliceResourceList(endpointSliceV1, endpointSliceResource)},
			expected:  endpointSliceV1,
		},
		{
			name:      "Kubernetes 1.20 serves v1beta1",
			resources: []*metav1.APIResourceList{endpointSliceResourceList(endpointSliceV1beta1, endpointSliceResource)},
			expected:  endpointSliceV1beta1,
		},
		{
			name:      "group without EndpointSlices",
			resources: []*metav1.APIResourceList{endpointSliceResourceList(endpointSliceV1, "other")},
			expected:  "",
		},
		{
			name:     "API not served",
			expected: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := clientWithServedResources(test.resources)
			apiVersion, err := detectEndpointSliceAPI(client.Discovery())
			if err != nil {
				t.Fatalf("detect EndpointSlice API: %v", err)
			}
			if apiVersion != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, apiVersion)
			}
		})
	}
}

func TestNewWatcherFailsWhenEndpointSliceDiscoveryFails(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("discovery unavailable")
	})

	if _, err := NewWatcherFromClient(client, time.Minute); err == nil {
		t.Fatal("expected discovery failure to stop watcher creation")
	}
}

func TestConvertV1beta1EndpointSliceMovesTopologyFields(t *testing.T) {
	ready := true
	serving := false
	port := int32(8080)
	name := "http"
	protocol := corev1.ProtocolTCP
	slice := &discoveryv1beta1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "apps",
			Name:      "api-abc12",
			Labels:    map[string]string{discoveryv1beta1.LabelServiceName: "api"},
		},
		AddressType: discoveryv1beta1.AddressTypeIPv4,
		Endpoints: []discoveryv1beta1.Endpoint{{
			Addresses:  []string{"10.0.0.12"},
			Conditions: discoveryv1beta1.EndpointConditions{Ready: &ready, Serving: &serving},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: "apps", Name: "api-0"},
			Topology: map[string]string{
				corev1.LabelHostname:       "worker-a",
				corev1.LabelTopologyZone:   "zone-a",
				corev1.LabelTopologyRegion: "region-a",
			},
		}},
		Ports: []discoveryv1beta1.EndpointPort{{Name: &name, Protocol: &protocol, Port: &port}},
	}

	converted := convertV1beta1EndpointSlice(slice)

	if converted.APIVersion != endpointSliceV1 {
		t.Fatalf("expected converted apiVersion %q, got %q", endpointSliceV1, converted.APIVersion)
	}
	if converted.Labels[discoveryv1.LabelServiceName] != "api" || converted.AddressType != discoveryv1.AddressTypeIPv4 {
		t.Fatalf("expected metadata and address type to be preserved, got %#v", converted)
	}
	endpoint := converted.Endpoints[0]
	if endpoint.NodeName == nil || *endpoint.NodeName != "worker-a" {
		t.Fatalf("expected node name from topology hostname, got %v", endpoint.NodeName)
	}
	if endpoint.Zone == nil || *endpoint.Zone != "zone-a" {
		t.Fatalf("expected zone from topology, got %v", endpoint.Zone)
	}
	if len(endpoint.DeprecatedTopology) != 1 || endpoint.DeprecatedTopology[corev1.LabelTopologyRegion] != "region-a" {
		t.Fatalf("expected only unmapped topology keys to remain, got %#v", endpoint.DeprecatedTopology)
	}
	if !conditionPtrValue(endpoint.Conditions.Ready, false) || conditionPtrValue(endpoint.Conditions.Serving, true) {
		t.Fatalf("expected conditions to be preserved, got %#v", endpoint.Conditions)
	}
	if endpoint.TargetRef == nil || endpoint.TargetRef.Name != "api-0" {
		t.Fatalf("expected target reference to be preserved, got %#v", endpoint.TargetRef)
	}
	if endpointSlicePorts(converted) != "http:8080/TCP" {
		t.Fatalf("expected ports to be preserved, got %q", endpointSlicePorts(converted))
	}
	if _, ok := slice.Endpoints[0].Topology[corev1.LabelHostname]; !ok {
		t.Fatal("expected conversion to leave the cached v1beta1 object unchanged")
	}
}

func TestWatcherReadsV1beta1EndpointSlicesOnKubernetes120(t *testing.T) {
	ready := true
	pathType := networkingv1.PathTypePrefix
	client := clientWithServedResources(
		[]*metav1.APIResourceList{endpointSliceResourceList(endpointSliceV1beta1, endpointSliceResource)},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "legacy"},
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{{
					Host: "legacy.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
								Name: "legacy",
								Port: networkingv1.ServiceBackendPort{Number: 80},
							}},
						}},
					}},
				}},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "legacy"},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 80}}},
		},
		&discoveryv1beta1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "apps",
				Name:      "legacy-manual",
				Labels:    map[string]string{discoveryv1beta1.LabelServiceName: "legacy"},
			},
			AddressType: discoveryv1beta1.AddressTypeIPv4,
			Endpoints: []discoveryv1beta1.Endpoint{{
				Addresses:  []string{"192.0.2.45"},
				Conditions: discoveryv1beta1.EndpointConditions{Ready: &ready},
			}},
		},
	)

	watcher, err := NewWatcherFromClient(client, time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	if watcher.endpointSlices == nil || watcher.endpointSlices.apiVersion != endpointSliceV1beta1 {
		t.Fatalf("expected v1beta1 EndpointSlice source, got %#v", watcher.endpointSlices)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- watcher.Run(ctx)
	}()

	deadline := time.After(5 * time.Second)
	for !watcher.Ready() {
		select {
		case err := <-errCh:
			t.Fatalf("watcher exited before becoming ready: %v", err)
		case <-deadline:
			t.Fatal("watcher did not become ready with v1beta1 EndpointSlices")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	snapshot := watcher.Latest()
	serviceID := nodeID(models.NodeKindService, "apps", "legacy")
	endpointID := endpointNodeID("apps", "legacy", "legacy-manual", "192.0.2.45")
	if findNode(t, snapshot, endpointID).Data.Status != "Ready" {
		t.Fatalf("expected v1beta1 endpoint to be Ready, got %#v", findNode(t, snapshot, endpointID).Data)
	}
	assertEdge(t, snapshot, serviceID, endpointID, "endpointslice")
}

func TestWatcherFallsBackToLegacyEndpointsWithoutEndpointSliceAPI(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	if watcher.endpointSlices != nil {
		t.Fatalf("expected no EndpointSlice source, got %s", watcher.endpointSlices.apiVersion)
	}
}
