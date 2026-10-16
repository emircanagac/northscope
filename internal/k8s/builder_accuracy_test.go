package k8s

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/emircanagac/northscope/internal/models"
)

func TestBuildTopologyAcceptsExternalNameServiceWithoutPorts(t *testing.T) {
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{annotatedIngress("legacy", "nginx")},
		nil,
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "api"},
			Spec: corev1.ServiceSpec{
				Type:         corev1.ServiceTypeExternalName,
				ExternalName: "api.partner.example.com",
			},
		}},
		nil,
	)

	route := findRouteNodeByBackend(t, snapshot, "api:80")
	if route.Data.Status != "Configured" {
		t.Fatalf("expected an ExternalName Service without ports to be Configured, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}
}

func TestBuildTopologyReportsReferencesOutsideTheWatchedNamespace(t *testing.T) {
	route := ExternalResource{
		Kind:       ExternalKindHTTPRoute,
		Namespace:  "apps",
		Name:       "store",
		ParentRefs: []ExternalParentRef{{Kind: "Gateway", Namespace: "infra", Name: "public"}},
		Backends:   []ExternalBackendRef{{Kind: "Service", Namespace: "shared", Name: "auth"}},
		Properties: map[string]string{},
	}
	routeID := routeNodeID("apps", "store", string(ExternalKindHTTPRoute))

	scoped := BuildTopologyWithOptions(TopologyOptions{WatchNamespace: "apps"}, nil, nil, nil, nil, nil, []ExternalResource{route}, nil, nil)
	node := findNode(t, scoped, routeID)
	if node.Data.Status == "Reference error" {
		t.Fatalf("expected references outside the watched namespace not to be errors, got %#v", node.Data.Properties)
	}
	outside := node.Data.Properties["outsideWatchScope"]
	if !strings.Contains(outside, "Gateway infra/public") || !strings.Contains(outside, "Service shared/auth") {
		t.Fatalf("expected out-of-scope references to be listed, got %q", outside)
	}

	route.Properties = map[string]string{}
	allNamespaces := BuildTopologyWithOptions(TopologyOptions{}, nil, nil, nil, nil, nil, []ExternalResource{route}, nil, nil)
	if node := findNode(t, allNamespaces, routeID); node.Data.Status != "Reference error" {
		t.Fatalf("expected missing references to be errors when every namespace is watched, got %q", node.Data.Status)
	}
}

func runningPod(name, ip string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name, Labels: map[string]string{"app": "api"}},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			PodIP:      ip,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
}

// apiEndpointSlice lists the Pods for the Service's unnamed port, like the
// EndpointSlice controller does when the Pods expose the targetPort.
func apiEndpointSlice(podNames ...string) *discoveryv1.EndpointSlice {
	slice := apiEndpointSliceWithoutPorts(podNames...)
	name, port := "", int32(8080)
	slice.Ports = []discoveryv1.EndpointPort{{Name: &name, Port: &port}}
	return slice
}

// apiEndpointSliceWithoutPorts lists the Pods with no ports, which is what the
// EndpointSlice controller writes when the Pods lack a named targetPort.
func apiEndpointSliceWithoutPorts(podNames ...string) *discoveryv1.EndpointSlice {
	ready := true
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "api-abc",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "api"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	for _, name := range podNames {
		slice.Endpoints = append(slice.Endpoints, discoveryv1.Endpoint{
			Addresses:  []string{"10.0.0." + name[len(name)-1:]},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: name},
		})
	}
	return slice
}

func TestBuildTopologyWarnsWhenSelectedPodsAreNotEndpoints(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "api"},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "api"},
			Ports:    []corev1.ServicePort{{Port: 80}},
		},
	}
	pods := []*corev1.Pod{runningPod("api-1", "10.0.0.1"), runningPod("api-2", "10.0.0.2")}
	ingresses := []*networkingv1.Ingress{annotatedIngress("web", "nginx")}

	partial := BuildTopology(ingresses, nil, []*corev1.Service{service}, pods, []*discoveryv1.EndpointSlice{apiEndpointSlice("api-1")})
	route := findRouteNodeByBackend(t, partial, "api:80")
	if route.Data.Status != "Warning" || !strings.Contains(route.Data.Properties["diagnosis"], "1 running Pod(s)") {
		t.Fatalf("expected a warning for a selected Pod that is not an endpoint, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}

	missingPort := BuildTopology(ingresses, nil, []*corev1.Service{service}, pods, []*discoveryv1.EndpointSlice{apiEndpointSliceWithoutPorts("api-1", "api-2")})
	route = findRouteNodeByBackend(t, missingPort, "api:80")
	if route.Data.Status != "Warning" || !strings.Contains(route.Data.Properties["diagnosis"], "2 running Pod(s)") {
		t.Fatalf("expected a warning when the Pods are listed without the Service port, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}

	complete := BuildTopology(ingresses, nil, []*corev1.Service{service}, pods, []*discoveryv1.EndpointSlice{apiEndpointSlice("api-1", "api-2")})
	if route := findRouteNodeByBackend(t, complete, "api:80"); route.Data.Status != "Backends ready" {
		t.Fatalf("expected Backends ready when every selected Pod is an endpoint, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}

	pending := runningPod("api-3", "")
	pending.Status.Phase = corev1.PodPending
	withPending := BuildTopology(ingresses, nil, []*corev1.Service{service}, append(pods, pending), []*discoveryv1.EndpointSlice{apiEndpointSlice("api-1", "api-2")})
	if route := findRouteNodeByBackend(t, withPending, "api:80"); route.Data.Status != "Backends ready" {
		t.Fatalf("expected a Pending Pod without an IP not to count, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}
	assertEdge(t, complete, nodeID(models.NodeKindService, "default", "api"), nodeID(models.NodeKindPod, "default", "api-2"), "selects")
}
