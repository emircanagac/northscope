package k8s

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/emircanagac/northscope/internal/models"
)

func ingressWithStatus(name, address string) *networkingv1.Ingress {
	ingress := annotatedIngress(name, "")
	ingress.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{{IP: address}}
	return ingress
}

func loadBalancerService(namespace, name, address string, labels map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, Labels: labels},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{
				{Name: "web", Port: 80, NodePort: 31080},
				{Name: "websecure", Port: 443, NodePort: 31443},
			},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: address}},
		}},
	}
}

func apiService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "api"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}},
	}
}

func TestBuildTopologyInfersControllerFromIngressStatusAddress(t *testing.T) {
	traefik := loadBalancerService("kube-system", "traefik", "172.17.0.3", map[string]string{"app.kubernetes.io/name": "traefik"})
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{ingressWithStatus("web", "172.17.0.3")},
		nil,
		[]*corev1.Service{apiService(), traefik},
		nil,
	)

	controllerID := statusControllerNodeID(traefik)
	controller := findNode(t, snapshot, controllerID)
	if controller.Data.Status != "Inferred" || controller.Data.Properties["source"] != "Ingress status address" {
		t.Fatalf("expected an inferred status controller, got %#v", controller.Data)
	}
	if controller.Data.Namespace != "" {
		t.Fatalf("controller nodes must not add a namespace to the UI filter, got %q", controller.Data.Namespace)
	}
	assertEdge(t, snapshot, controllerID, nodeID(models.NodeKindIngress, "default", "web"), "controls")
	webNodePort := nodePortNodeID("kube-system", "traefik", corev1.ServicePort{Name: "web", Port: 80})
	assertEdge(t, snapshot, webNodePort, controllerID, "forwards")
	assertEdge(t, snapshot, loadBalancerNodeID("kube-system", "traefik"), webNodePort, "exposes")
	if route := findRouteNodeByBackend(t, snapshot, "api:80"); strings.Contains(route.Data.Properties["diagnosis"], "IngressClass") {
		t.Fatalf("expected the status controller to resolve the route, got %q", route.Data.Properties["diagnosis"])
	}
}

func TestBuildTopologyUsesIngressClassControllerMatchedByStatusAddress(t *testing.T) {
	nginx := loadBalancerService("ingress-nginx", "ingress-nginx-controller", "203.0.113.10", map[string]string{"app.kubernetes.io/component": "controller"})
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{ingressWithStatus("web", "203.0.113.10")},
		[]*networkingv1.IngressClass{
			{ObjectMeta: metav1.ObjectMeta{Name: "nginx"}, Spec: networkingv1.IngressClassSpec{Controller: "k8s.io/ingress-nginx"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "haproxy"}, Spec: networkingv1.IngressClassSpec{Controller: "haproxy.org/ingress-controller/haproxy"}},
		},
		[]*corev1.Service{apiService(), nginx},
		nil,
	)

	assertEdge(t, snapshot, controllerNodeID("nginx"), nodeID(models.NodeKindIngress, "default", "web"), "controls")
	assertNoEdge(t, snapshot, controllerNodeID("haproxy"), nodeID(models.NodeKindIngress, "default", "web"), "controls")
	for _, node := range snapshot.Nodes {
		if node.ID == statusControllerNodeID(nginx) {
			t.Fatal("expected the IngressClass controller instead of a separate status controller")
		}
	}
}

func TestBuildTopologyDoesNotInferControllerFromAmbiguousStatusAddress(t *testing.T) {
	first := loadBalancerService("apps", "billing", "198.51.100.7", nil)
	second := loadBalancerService("apps", "reports", "198.51.100.7", nil)
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{ingressWithStatus("web", "198.51.100.7")},
		nil,
		[]*corev1.Service{apiService(), first, second},
		nil,
	)

	for _, edge := range snapshot.Edges {
		if edge.Data != nil && edge.Data.Kind == "controls" {
			t.Fatalf("expected no controller for an ambiguous status address, got %s", edge.ID)
		}
	}
}
