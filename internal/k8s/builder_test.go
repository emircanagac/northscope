package k8s

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/emircanagac/northscope/internal/models"
)

func TestBuildTopologyLargeClusterSmoke(t *testing.T) {
	const ingressCount = 80
	const pathsPerIngress = 3

	className := "nginx"
	pathType := networkingv1.PathTypePrefix

	ingresses := make([]*networkingv1.Ingress, 0, ingressCount)
	services := make([]*corev1.Service, 0, ingressCount*pathsPerIngress)
	pods := make([]*corev1.Pod, 0, ingressCount*pathsPerIngress)

	for ingressIndex := 0; ingressIndex < ingressCount; ingressIndex++ {
		paths := make([]networkingv1.HTTPIngressPath, 0, pathsPerIngress)
		for pathIndex := 0; pathIndex < pathsPerIngress; pathIndex++ {
			serviceName := fmt.Sprintf("svc-%03d-%d", ingressIndex, pathIndex)
			path := fmt.Sprintf("/app-%03d/%d", ingressIndex, pathIndex)
			paths = append(paths, networkingv1.HTTPIngressPath{
				Path:     path,
				PathType: &pathType,
				Backend: networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{
						Name: serviceName,
						Port: networkingv1.ServiceBackendPort{Name: "http"},
					},
				},
			})
			services = append(services, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "scale",
					Name:      serviceName,
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": serviceName},
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
				},
			})
			pods = append(pods, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "scale",
					Name:      serviceName + "-pod",
					Labels:    map[string]string{"app": serviceName},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					PodIP: fmt.Sprintf("10.42.%d.%d", ingressIndex/250, ingressIndex%250+pathIndex+1),
				},
			})
		}

		ingresses = append(ingresses, &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "scale",
				Name:      fmt.Sprintf("ingress-%03d", ingressIndex),
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				Rules: []networkingv1.IngressRule{{
					Host: fmt.Sprintf("app-%03d.example.com", ingressIndex),
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths},
					},
				}},
			},
		})
	}

	snapshot := BuildTopology(
		ingresses,
		[]*networkingv1.IngressClass{{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: networkingv1.IngressClassSpec{
				Controller: "k8s.io/ingress-nginx",
			},
		}},
		services,
		pods,
	)

	assertKindCount(t, snapshot, models.NodeKindIngress, ingressCount)
	assertKindCount(t, snapshot, models.NodeKindRoute, ingressCount*pathsPerIngress)
	assertKindCount(t, snapshot, models.NodeKindService, ingressCount*pathsPerIngress)
	assertKindCount(t, snapshot, models.NodeKindPod, ingressCount*pathsPerIngress)
}

func TestBuildTopologyIngressServicePod(t *testing.T) {
	className := "nginx"
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "web",
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				Rules: []networkingv1.IngressRule{{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "web",
										Port: networkingv1.ServiceBackendPort{Number: 80},
									},
								},
							}},
						},
					},
				}},
			},
		}},
		[]*networkingv1.IngressClass{{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: networkingv1.IngressClassSpec{
				Controller: "k8s.io/ingress-nginx",
			},
		}},
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "web",
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "web"},
				Ports:    []corev1.ServicePort{{Port: 80}},
			},
		}},
		[]*corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "web-abc",
				Labels:    map[string]string{"app": "web"},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				PodIP: "10.0.0.10",
			},
		}},
	)

	assertNode(t, snapshot, nodeID(models.NodeKindIngress, "default", "web"))
	assertNode(t, snapshot, nodeID(models.NodeKindService, "default", "web"))
	assertNode(t, snapshot, nodeID(models.NodeKindPod, "default", "web-abc"))
	assertEdge(t, snapshot, controllerNodeID(className), nodeID(models.NodeKindIngress, "default", "web"), "controls")
	route := findRouteNodeByBackend(t, snapshot, "web:80")
	assertEdge(t, snapshot, nodeID(models.NodeKindIngress, "default", "web"), route.ID, "defines")
	assertEdge(t, snapshot, route.ID, nodeID(models.NodeKindService, "default", "web"), "routes")
	assertNoEdge(t, snapshot, nodeID(models.NodeKindIngress, "default", "web"), nodeID(models.NodeKindService, "default", "web"), "routes")
	assertEdge(t, snapshot, nodeID(models.NodeKindService, "default", "web"), nodeID(models.NodeKindPod, "default", "web-abc"), "selects")
}

func TestBuildTopologySameHostAcrossIngressObjectsKeepsRoutesSeparate(t *testing.T) {
	className := "nginx"
	sharedHost := "shared.example.com"

	snapshot := BuildTopology(
		[]*networkingv1.Ingress{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "blue",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &className,
					Rules: []networkingv1.IngressRule{{
						Host: sharedHost,
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{{
									Path:     "/blue",
									PathType: pathTypePtr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "blue",
											Port: networkingv1.ServiceBackendPort{Name: "http"},
										},
									},
								}},
							},
						},
					}},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "green",
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &className,
					Rules: []networkingv1.IngressRule{{
						Host: sharedHost,
						IngressRuleValue: networkingv1.IngressRuleValue{
							HTTP: &networkingv1.HTTPIngressRuleValue{
								Paths: []networkingv1.HTTPIngressPath{{
									Path:     "/green",
									PathType: pathTypePtr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "green",
											Port: networkingv1.ServiceBackendPort{Name: "http"},
										},
									},
								}},
							},
						},
					}},
				},
			},
		},
		[]*networkingv1.IngressClass{{
			ObjectMeta: metav1.ObjectMeta{Name: className},
			Spec: networkingv1.IngressClassSpec{
				Controller: "k8s.io/ingress-nginx",
			},
		}},
		[]*corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "blue",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "blue"},
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "green",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "green"},
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
				},
			},
		},
		nil,
	)

	dnsID := nodeID(models.NodeKindDNS, "", sharedHost)
	blueIngressID := nodeID(models.NodeKindIngress, "default", "blue")
	greenIngressID := nodeID(models.NodeKindIngress, "default", "green")
	blueRoute := findRouteNodeByBackend(t, snapshot, "blue:http")
	greenRoute := findRouteNodeByBackend(t, snapshot, "green:http")

	assertNode(t, snapshot, dnsID)
	assertEdge(t, snapshot, dnsID, blueIngressID, "resolves")
	assertEdge(t, snapshot, dnsID, greenIngressID, "resolves")
	assertEdge(t, snapshot, blueIngressID, blueRoute.ID, "defines")
	assertEdge(t, snapshot, greenIngressID, greenRoute.ID, "defines")
	assertEdge(t, snapshot, blueRoute.ID, nodeID(models.NodeKindService, "default", "blue"), "routes")
	assertEdge(t, snapshot, greenRoute.ID, nodeID(models.NodeKindService, "default", "green"), "routes")
	assertNoEdge(t, snapshot, blueIngressID, greenRoute.ID, "defines")
	assertNoEdge(t, snapshot, greenIngressID, blueRoute.ID, "defines")
}

func TestBuildTopologySelectorlessServiceUsesEndpointSlice(t *testing.T) {
	snapshot := BuildTopology(
		nil,
		nil,
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "external",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Port: 8080}},
			},
		}},
		[]*corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "external-pod",
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				PodIP: "10.0.0.20",
			},
		}},
		[]*discoveryv1.EndpointSlice{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "external-abc",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "external",
				},
			},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.0.0.20"},
			}},
		}},
	)

	serviceID := nodeID(models.NodeKindService, "default", "external")
	podID := nodeID(models.NodeKindPod, "default", "external-pod")
	assertNode(t, snapshot, serviceID)
	assertNode(t, snapshot, podID)
	assertEdge(t, snapshot, serviceID, podID, "endpointslice")
	assertNoEdge(t, snapshot, serviceID, podID, "selects")
}

func TestBuildTopologySelectorlessServiceUsesExternalEndpointSliceAddress(t *testing.T) {
	ready := true
	snapshot := BuildTopology(
		nil,
		nil,
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "legacy",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Name: "http", Port: 80}},
			},
		}},
		nil,
		[]*discoveryv1.EndpointSlice{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "legacy-manual",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "legacy",
				},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{{
				Addresses:  []string{"192.0.2.45"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			}},
		}},
	)

	serviceID := nodeID(models.NodeKindService, "default", "legacy")
	endpointID := endpointNodeID("default", "legacy", "legacy-manual", "192.0.2.45")
	endpoint := findNode(t, snapshot, endpointID)
	if endpoint.Data.Kind != models.NodeKindEndpointSlice {
		t.Fatalf("expected endpoint node kind EndpointSlice, got %q", endpoint.Data.Kind)
	}
	if endpoint.Data.Properties["address"] != "192.0.2.45" {
		t.Fatalf("expected endpoint address property, got %#v", endpoint.Data.Properties)
	}
	assertEdge(t, snapshot, serviceID, endpointID, "endpointslice")
}

func TestBuildTopologyLoadBalancerIngressControllerForwardsToController(t *testing.T) {
	snapshot := BuildTopology(
		nil,
		[]*networkingv1.IngressClass{{
			ObjectMeta: metav1.ObjectMeta{Name: "nginx"},
			Spec: networkingv1.IngressClassSpec{
				Controller: "k8s.io/ingress-nginx",
			},
		}},
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ingress-nginx",
				Name:      "ingress-nginx-controller",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
				Ports: []corev1.ServicePort{{
					Name:     "http",
					Port:     80,
					NodePort: 30080,
				}},
			},
		}},
		nil,
	)

	loadBalancerID := loadBalancerNodeID("ingress-nginx", "ingress-nginx-controller")
	nodePortID := nodePortNodeID("ingress-nginx", "ingress-nginx-controller", corev1.ServicePort{Name: "http", Port: 80})
	controllerID := controllerNodeID("nginx")
	assertNode(t, snapshot, loadBalancerID)
	assertNode(t, snapshot, nodePortID)
	assertEdge(t, snapshot, loadBalancerID, nodePortID, "exposes")
	assertEdge(t, snapshot, nodePortID, controllerID, "forwards")
}

func TestBuildTopologyDoesNotGuessIngressControllerWhenMultipleClassesExist(t *testing.T) {
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "unclassified",
			},
		}},
		[]*networkingv1.IngressClass{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "nginx"},
				Spec: networkingv1.IngressClassSpec{
					Controller: "k8s.io/ingress-nginx",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "traefik"},
				Spec: networkingv1.IngressClassSpec{
					Controller: "traefik.io/ingress-controller",
				},
			},
		},
		nil,
		nil,
	)

	ingressID := nodeID(models.NodeKindIngress, "default", "unclassified")
	assertNode(t, snapshot, ingressID)
	assertNoEdge(t, snapshot, controllerNodeID("nginx"), ingressID, "controls")
	assertNoEdge(t, snapshot, controllerNodeID("traefik"), ingressID, "controls")
}

func TestBuildTopologyRoutesIngressWithNamedBackendPort(t *testing.T) {
	className := "nginx"
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "api",
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				Rules: []networkingv1.IngressRule{{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "api",
										Port: networkingv1.ServiceBackendPort{Name: "http"},
									},
								},
							}},
						},
					},
				}},
			},
		}},
		[]*networkingv1.IngressClass{{
			ObjectMeta: metav1.ObjectMeta{Name: className},
		}},
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "api",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Name: "http", Port: 8080}},
			},
		}},
		nil,
	)

	route := findRouteNodeByBackend(t, snapshot, "api:http")
	assertEdge(t, snapshot, nodeID(models.NodeKindIngress, "default", "api"), route.ID, "defines")
	assertEdge(t, snapshot, route.ID, nodeID(models.NodeKindService, "default", "api"), "routes")
	assertNoEdge(t, snapshot, nodeID(models.NodeKindIngress, "default", "api"), nodeID(models.NodeKindService, "default", "api"), "routes")
}

func TestBuildTopologyCreatesOneRouteNodePerIngressBackend(t *testing.T) {
	className := "nginx"
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "web",
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				Rules: []networkingv1.IngressRule{{
					Host: "app.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: pathTypePtr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "frontend",
											Port: networkingv1.ServiceBackendPort{Name: "http"},
										},
									},
								},
								{
									Path:     "/api",
									PathType: pathTypePtr(networkingv1.PathTypePrefix),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "backend",
											Port: networkingv1.ServiceBackendPort{Number: 8080},
										},
									},
								},
							},
						},
					},
				}},
			},
		}},
		[]*networkingv1.IngressClass{{
			ObjectMeta: metav1.ObjectMeta{Name: className},
		}},
		[]*corev1.Service{
			{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "frontend"},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "frontend"},
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "backend"},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "backend"},
					Ports:    []corev1.ServicePort{{Port: 8080}},
				},
			},
		},
		[]*corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "frontend-abc", Labels: map[string]string{"app": "frontend"}},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "backend-abc", Labels: map[string]string{"app": "backend"}},
				Status: corev1.PodStatus{
					Phase:      corev1.PodRunning,
					Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				},
			},
		},
	)

	frontendRoute := findRouteNodeByBackend(t, snapshot, "frontend:http")
	backendRoute := findRouteNodeByBackend(t, snapshot, "backend:8080")
	ingressID := nodeID(models.NodeKindIngress, "default", "web")

	assertEdge(t, snapshot, ingressID, frontendRoute.ID, "defines")
	assertEdge(t, snapshot, frontendRoute.ID, nodeID(models.NodeKindService, "default", "frontend"), "routes")
	assertEdge(t, snapshot, ingressID, backendRoute.ID, "defines")
	assertEdge(t, snapshot, backendRoute.ID, nodeID(models.NodeKindService, "default", "backend"), "routes")
}

func TestBuildTopologyDiagnosesMissingServicePortOnIngressRoute(t *testing.T) {
	className := "nginx"
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "api",
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				Rules: []networkingv1.IngressRule{{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "api",
										Port: networkingv1.ServiceBackendPort{Name: "http"},
									},
								},
							}},
						},
					},
				}},
			},
		}},
		[]*networkingv1.IngressClass{{ObjectMeta: metav1.ObjectMeta{Name: className}}},
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "api"},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Name: "grpc", Port: 9090}},
			},
		}},
		nil,
	)

	route := findRouteNodeByBackend(t, snapshot, "api:http")
	if route.Data.Status != "Error" {
		t.Fatalf("expected route status Error, got %q", route.Data.Status)
	}
	if route.Data.Properties["severity"] != "error" {
		t.Fatalf("expected route severity error, got %#v", route.Data.Properties)
	}
	if route.Data.Properties["diagnosis"] != `Service "api" has no port matching backend port "http".` {
		t.Fatalf("expected missing named port diagnosis, got %#v", route.Data.Properties)
	}
	if route.Data.Properties["kubectl"] != "kubectl get svc api -n default -o yaml" {
		t.Fatalf("expected service kubectl command, got %#v", route.Data.Properties)
	}
	assertEdge(t, snapshot, route.ID, nodeID(models.NodeKindService, "default", "api"), "routes")
}

func TestBuildTopologyEndpointSliceTargetRefWinsOverAddressLookup(t *testing.T) {
	snapshot := BuildTopology(
		nil,
		nil,
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "external",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Port: 8080}},
			},
		}},
		[]*corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "target-pod",
				},
				Status: corev1.PodStatus{PodIP: "10.0.0.30"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "address-pod",
				},
				Status: corev1.PodStatus{PodIP: "10.0.0.99"},
			},
		},
		[]*discoveryv1.EndpointSlice{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "external-abc",
				Labels: map[string]string{
					discoveryv1.LabelServiceName: "external",
				},
			},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.0.0.99"},
				TargetRef: &corev1.ObjectReference{
					Kind: "Pod",
					Name: "target-pod",
				},
			}},
		}},
	)

	serviceID := nodeID(models.NodeKindService, "default", "external")
	assertEdge(t, snapshot, serviceID, nodeID(models.NodeKindPod, "default", "target-pod"), "endpointslice")
	assertNoEdge(t, snapshot, serviceID, nodeID(models.NodeKindPod, "default", "address-pod"), "endpointslice")
}

func TestBuildTopologyHeadlessServiceOmitsClusterIPProperty(t *testing.T) {
	snapshot := BuildTopology(
		nil,
		nil,
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "headless",
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: corev1.ClusterIPNone,
				Ports:     []corev1.ServicePort{{Port: 5432}},
			},
		}},
		nil,
	)

	node := findNode(t, snapshot, nodeID(models.NodeKindService, "default", "headless"))
	if _, ok := node.Data.Properties["clusterIP"]; ok {
		t.Fatalf("headless service should not expose clusterIP property: %#v", node.Data.Properties)
	}
}

func TestBuildTopologyGatewayAPIRouteToService(t *testing.T) {
	snapshot := BuildTopologyWithResources(
		nil,
		nil,
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "api",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Port: 8080}},
			},
		}},
		nil,
		nil,
		[]ExternalResource{
			{
				Kind:       ExternalKindGatewayClass,
				APIVersion: "gateway.networking.k8s.io/v1",
				Name:       "public",
				Properties: map[string]string{"controllerName": "example.com/gateway-controller"},
			},
			{
				Kind:       ExternalKindGateway,
				APIVersion: "gateway.networking.k8s.io/v1",
				Namespace:  "default",
				Name:       "edge",
				ClassName:  "public",
				Addresses:  []string{"203.0.113.10"},
				Hostnames:  []string{"api.example.com"},
				Listeners:  []string{"https:HTTPS/443"},
			},
			{
				Kind:       ExternalKindHTTPRoute,
				APIVersion: "gateway.networking.k8s.io/v1",
				Namespace:  "default",
				Name:       "api",
				Hostnames:  []string{"api.example.com"},
				ParentRefs: []ExternalParentRef{{Kind: "Gateway", Name: "edge"}},
				Backends:   []ExternalBackendRef{{Name: "api", Port: "8080"}},
			},
		},
	)

	gatewayID := gatewayNodeID("default", "edge")
	routeID := routeNodeID("default", "api", string(ExternalKindHTTPRoute))
	serviceID := nodeID(models.NodeKindService, "default", "api")
	dnsID := nodeID(models.NodeKindDNS, "", "api.example.com")
	lbID := gatewayLoadBalancerNodeID("default", "edge", "203.0.113.10")

	assertNode(t, snapshot, gatewayID)
	assertNode(t, snapshot, routeID)
	assertNode(t, snapshot, dnsID)
	assertNode(t, snapshot, lbID)
	assertEdge(t, snapshot, controllerNodeID("public"), gatewayID, "controls")
	assertEdge(t, snapshot, lbID, gatewayID, "fronts")
	assertEdge(t, snapshot, dnsID, gatewayID, "resolves")
	assertEdge(t, snapshot, gatewayID, routeID, "attaches")
	assertEdge(t, snapshot, routeID, serviceID, "routes")
}

func TestBuildTopologyF5IngressLinkBalancesSelectedService(t *testing.T) {
	snapshot := BuildTopologyWithResources(
		nil,
		nil,
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ingress-nginx",
				Name:      "ingress-nginx-controller",
				Labels:    map[string]string{"app": "ingress-nginx"},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Port: 80}},
			},
		}},
		nil,
		nil,
		[]ExternalResource{{
			Kind:      ExternalKindF5IngressLink,
			Namespace: "ingress-nginx",
			Name:      "public",
			Addresses: []string{"198.51.100.25"},
			Hostnames: []string{"www.example.com"},
			Selector:  map[string]string{"app": "ingress-nginx"},
		}},
	)

	lbID := externalResourceNodeID(ExternalResource{Kind: ExternalKindF5IngressLink, Namespace: "ingress-nginx", Name: "public"})
	serviceID := nodeID(models.NodeKindService, "ingress-nginx", "ingress-nginx-controller")
	dnsID := nodeID(models.NodeKindDNS, "", "www.example.com")

	assertNode(t, snapshot, lbID)
	assertNode(t, snapshot, dnsID)
	assertEdge(t, snapshot, dnsID, lbID, "resolves")
	assertEdge(t, snapshot, lbID, serviceID, "balances")
}

func TestBuildTopologyIngressStatusCreatesLoadBalancerAndDNS(t *testing.T) {
	className := "nginx"
	snapshot := BuildTopology(
		[]*networkingv1.Ingress{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "web",
			},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				Rules: []networkingv1.IngressRule{{
					Host: "web.example.com",
				}},
			},
			Status: networkingv1.IngressStatus{
				LoadBalancer: networkingv1.IngressLoadBalancerStatus{
					Ingress: []networkingv1.IngressLoadBalancerIngress{{Hostname: "edge.example.net"}},
				},
			},
		}},
		[]*networkingv1.IngressClass{{
			ObjectMeta: metav1.ObjectMeta{Name: className},
		}},
		nil,
		nil,
	)

	ingressID := nodeID(models.NodeKindIngress, "default", "web")
	dnsID := nodeID(models.NodeKindDNS, "", "web.example.com")
	lbID := ingressLoadBalancerNodeID("default", "web", "edge.example.net")

	assertNode(t, snapshot, dnsID)
	assertNode(t, snapshot, lbID)
	assertEdge(t, snapshot, dnsID, ingressID, "resolves")
	assertEdge(t, snapshot, lbID, ingressID, "fronts")
}

func TestBuildTopologyNodePortOpensOnNodesAndHostsPods(t *testing.T) {
	snapshot := BuildTopologyWithResources(
		nil,
		nil,
		[]*corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "web",
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeNodePort,
				Selector: map[string]string{"app": "web"},
				Ports: []corev1.ServicePort{{
					Name:     "http",
					Port:     80,
					NodePort: 30080,
				}},
			},
		}},
		[]*corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "web-abc",
				Labels:    map[string]string{"app": "web"},
			},
			Spec: corev1.PodSpec{NodeName: "node-a"},
		}},
		[]*corev1.Node{{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			},
		}},
		nil,
	)

	nodeIDValue := nodeID(models.NodeKindNode, "", "node-a")
	podID := nodeID(models.NodeKindPod, "default", "web-abc")
	nodePortID := nodePortNodeID("default", "web", corev1.ServicePort{Name: "http", Port: 80})

	assertNode(t, snapshot, nodeIDValue)
	assertEdge(t, snapshot, nodeIDValue, podID, "hosts")
	assertEdge(t, snapshot, nodePortID, nodeIDValue, "opens_on")
}

func assertNode(t *testing.T, snapshot models.TopologySnapshot, id string) {
	t.Helper()
	for _, node := range snapshot.Nodes {
		if node.ID == id {
			return
		}
	}
	t.Fatalf("missing node %q; got %#v", id, snapshot.Nodes)
}

func findNode(t *testing.T, snapshot models.TopologySnapshot, id string) models.Node {
	t.Helper()
	for _, node := range snapshot.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("missing node %q; got %#v", id, snapshot.Nodes)
	return models.Node{}
}

func findRouteNodeByBackend(t *testing.T, snapshot models.TopologySnapshot, backend string) models.Node {
	t.Helper()
	for _, node := range snapshot.Nodes {
		if node.Data.Kind == models.NodeKindRoute && node.Data.Properties["backend"] == backend {
			return node
		}
	}
	t.Fatalf("missing route node for backend %q; got %#v", backend, snapshot.Nodes)
	return models.Node{}
}

func pathTypePtr(value networkingv1.PathType) *networkingv1.PathType {
	return &value
}

func assertEdge(t *testing.T, snapshot models.TopologySnapshot, source, target, kind string) {
	t.Helper()
	if hasEdge(snapshot, source, target, kind) {
		return
	}
	t.Fatalf("missing edge %q -> %q kind %q; got %#v", source, target, kind, snapshot.Edges)
}

func assertNoEdge(t *testing.T, snapshot models.TopologySnapshot, source, target, kind string) {
	t.Helper()
	if hasEdge(snapshot, source, target, kind) {
		t.Fatalf("unexpected edge %q -> %q kind %q", source, target, kind)
	}
}

func hasEdge(snapshot models.TopologySnapshot, source, target, kind string) bool {
	for _, edge := range snapshot.Edges {
		if edge.Source == source && edge.Target == target && edge.Data != nil && edge.Data.Kind == kind {
			return true
		}
	}
	return false
}

func assertKindCount(t *testing.T, snapshot models.TopologySnapshot, kind models.NodeKind, expected int) {
	t.Helper()
	actual := 0
	for _, node := range snapshot.Nodes {
		if node.Data.Kind == kind {
			actual++
		}
	}
	if actual != expected {
		t.Fatalf("expected %d %s nodes, got %d", expected, kind, actual)
	}
}
