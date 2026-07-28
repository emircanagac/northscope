package k8s

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScopeTrafficInputsExcludesUnrelatedClusterWorkloads(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "store"},
		Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
					Path:     "/",
					PathType: &pathType,
					Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
						Name: "frontend",
						Port: networkingv1.ServiceBackendPort{Number: 80},
					}},
				}}},
			},
		}}},
	}
	services := []*corev1.Service{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "frontend"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "frontend"}},
	}}
	pods := []*corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "frontend-0", Labels: map[string]string{"app": "frontend"}},
		Spec:       corev1.PodSpec{NodeName: "worker-a"},
	}}
	for i := 0; i < 500; i++ {
		name := fmt.Sprintf("unrelated-%03d", i)
		services = append(services, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: name},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": name}},
		})
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: name, Labels: map[string]string{"app": name}},
			Spec:       corev1.PodSpec{NodeName: "worker-b"},
		})
	}

	scoped := scopeTrafficInputs(
		[]*networkingv1.Ingress{ingress},
		services,
		pods,
		[]*corev1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "worker-b"}},
		},
		nil,
		nil,
		nil,
	)

	if len(scoped.services) != 1 || scoped.services[0].Name != "frontend" {
		t.Fatalf("expected only frontend Service, got %d: %#v", len(scoped.services), scoped.services)
	}
	if len(scoped.pods) != 1 || scoped.pods[0].Name != "frontend-0" {
		t.Fatalf("expected only frontend Pod, got %d: %#v", len(scoped.pods), scoped.pods)
	}
	if len(scoped.nodes) != 1 || scoped.nodes[0].Name != "worker-a" {
		t.Fatalf("expected only worker-a, got %#v", scoped.nodes)
	}
}

func TestScopeTrafficInputsKeepsSelectorlessEndpointSliceTargets(t *testing.T) {
	ready := true
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "manual"}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: "manual-0"},
		Spec:       corev1.PodSpec{NodeName: "worker-a"},
		Status:     corev1.PodStatus{PodIP: "10.42.0.10"},
	}
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "apps",
			Name:      "manual-a",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "manual"},
		},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{"10.42.0.10"},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: "apps", Name: "manual-0"},
		}},
	}
	route := ExternalResource{
		Kind:      ExternalKindHTTPRoute,
		Namespace: "apps",
		Name:      "manual",
		Backends:  []ExternalBackendRef{{Name: "manual"}},
	}

	scoped := scopeTrafficInputs(
		nil,
		[]*corev1.Service{service},
		[]*corev1.Pod{pod},
		[]*corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "worker-a"}}},
		[]ExternalResource{route},
		nil,
		[]*discoveryv1.EndpointSlice{slice},
	)

	if len(scoped.endpointSlices) != 1 || len(scoped.pods) != 1 || len(scoped.nodes) != 1 {
		t.Fatalf("expected selector-less backend chain to remain scoped, got slices=%d pods=%d nodes=%d", len(scoped.endpointSlices), len(scoped.pods), len(scoped.nodes))
	}
}
