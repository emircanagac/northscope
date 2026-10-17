package k8s

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/emircanagac/northscope/internal/models"
)

var overlapBaseTime = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// shopIngress defines shop.example.com/ for className through the legacy
// annotation, created the given number of minutes after overlapBaseTime.
func shopIngress(name, className, service string, createdMinute int, annotations map[string]string) *networkingv1.Ingress {
	ingress := annotatedIngress(name, className)
	ingress.CreationTimestamp = metav1.NewTime(overlapBaseTime.Add(time.Duration(createdMinute) * time.Minute))
	ingress.Spec.Rules[0].Host = "shop.example.com"
	ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name = service
	for key, value := range annotations {
		ingress.Annotations[key] = value
	}
	return ingress
}

func overlapServices(names ...string) []*corev1.Service {
	services := make([]*corev1.Service, 0, len(names))
	for _, name := range names {
		services = append(services, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "api"},
				Ports:    []corev1.ServicePort{{Port: 80}},
			},
		})
	}
	return services
}

func overlapEndpointSlices(services ...string) []*discoveryv1.EndpointSlice {
	slices := make([]*discoveryv1.EndpointSlice, 0, len(services))
	for _, service := range services {
		slice := apiEndpointSlice("api-1")
		slice.Name = service + "-abc"
		slice.Labels = map[string]string{discoveryv1.LabelServiceName: service}
		slices = append(slices, slice)
	}
	return slices
}

func buildOverlapTopology(ingresses []*networkingv1.Ingress, services ...string) models.TopologySnapshot {
	return BuildTopology(ingresses, nil, overlapServices(services...), []*corev1.Pod{runningPod("api-1", "10.0.0.1")}, overlapEndpointSlices(services...))
}

func ingressRouteNode(t *testing.T, snapshot models.TopologySnapshot, ingress *networkingv1.Ingress) models.Node {
	t.Helper()
	routes := ingressRoutes(ingress)
	if len(routes) != 1 {
		t.Fatalf("expected one route on %s, got %d", ingress.Name, len(routes))
	}
	return findNode(t, snapshot, ingressRouteNodeID(ingress.Namespace, ingress.Name, routes[0].ID))
}

func TestBuildTopologyWarnsWhenIngressesShareHostAndPath(t *testing.T) {
	older := shopIngress("shop-v1", "nginx", "api", 0, nil)
	newer := shopIngress("shop-v2", "nginx", "api-v2", 5, nil)
	snapshot := buildOverlapTopology([]*networkingv1.Ingress{newer, older}, "api", "api-v2")

	olderRoute := ingressRouteNode(t, snapshot, older)
	newerRoute := ingressRouteNode(t, snapshot, newer)
	for _, route := range []models.Node{olderRoute, newerRoute} {
		if route.Data.Status != "Conflict" || route.Data.Properties["severity"] != "warning" {
			t.Fatalf("expected a Conflict warning, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
		}
		if !strings.Contains(route.Data.Properties["diagnosis"], "shop.example.com/") {
			t.Fatalf("expected the diagnosis to name the location, got %q", route.Data.Properties["diagnosis"])
		}
	}
	if olderRoute.Data.Properties["conflictsWith"] != "default/shop-v2" || newerRoute.Data.Properties["conflictsWith"] != "default/shop-v1" {
		t.Fatalf("expected each route to list the other Ingress, got %q and %q", olderRoute.Data.Properties["conflictsWith"], newerRoute.Data.Properties["conflictsWith"])
	}
	if !strings.Contains(olderRoute.Data.Properties["nextStep"], "serves this Ingress because it is the oldest") {
		t.Fatalf("expected the oldest Ingress to be named as served, got %q", olderRoute.Data.Properties["nextStep"])
	}
	if !strings.Contains(newerRoute.Data.Properties["nextStep"], "serves the oldest, default/shop-v1") {
		t.Fatalf("expected the newer Ingress to point at the oldest, got %q", newerRoute.Data.Properties["nextStep"])
	}
}

func TestBuildTopologyDoesNotReportOverlapsAcrossControllersOrPaths(t *testing.T) {
	nginx := shopIngress("shop-nginx", "nginx", "api", 0, nil)
	traefik := shopIngress("shop-traefik", "traefik", "api", 1, nil)
	otherPath := shopIngress("shop-admin", "nginx", "api", 2, nil)
	otherPath.Spec.Rules[0].HTTP.Paths[0].Path = "/admin"
	exact := shopIngress("shop-exact", "nginx", "api", 3, nil)
	exact.Spec.Rules[0].HTTP.Paths[0].PathType = pathTypePtr(networkingv1.PathTypeExact)

	snapshot := buildOverlapTopology([]*networkingv1.Ingress{nginx, traefik, otherPath, exact}, "api")
	for _, ingress := range []*networkingv1.Ingress{nginx, traefik, otherPath, exact} {
		route := ingressRouteNode(t, snapshot, ingress)
		if route.Data.Status != "Backends ready" || route.Data.Properties["conflictsWith"] != "" {
			t.Fatalf("expected %s not to conflict, got %q (%q): %s", ingress.Name, route.Data.Status, route.Data.Properties["conflictsWith"], route.Data.Properties["diagnosis"])
		}
	}
}

func TestBuildTopologyKeepsErrorsAheadOfOverlaps(t *testing.T) {
	healthy := shopIngress("shop-v1", "nginx", "api", 0, nil)
	broken := shopIngress("shop-v2", "nginx", "missing", 5, nil)
	snapshot := buildOverlapTopology([]*networkingv1.Ingress{healthy, broken}, "api")

	route := ingressRouteNode(t, snapshot, broken)
	if route.Data.Status != "Error" || route.Data.Properties["conflictsWith"] != "default/shop-v1" {
		t.Fatalf("expected the missing Service error with the overlap kept as a property, got %q (%q)", route.Data.Status, route.Data.Properties["conflictsWith"])
	}
	if route := ingressRouteNode(t, snapshot, healthy); route.Data.Status != "Conflict" {
		t.Fatalf("expected the healthy route to report the conflict, got %q", route.Data.Status)
	}
}

func TestBuildTopologyLinksNginxCanaryToPrimaryRoute(t *testing.T) {
	primary := shopIngress("shop", "nginx", "api", 0, nil)
	canary := shopIngress("shop-canary", "nginx", "api-canary", 5, map[string]string{
		nginxCanaryAnnotation:       "true",
		nginxCanaryWeightAnnotation: "20",
	})
	snapshot := buildOverlapTopology([]*networkingv1.Ingress{primary, canary}, "api", "api-canary")

	primaryRoute := ingressRouteNode(t, snapshot, primary)
	if primaryRoute.Data.Status != "Backends ready" {
		t.Fatalf("expected a canary not to be a conflict, got %q: %s", primaryRoute.Data.Status, primaryRoute.Data.Properties["diagnosis"])
	}
	if got := primaryRoute.Data.Properties["canaries"]; got != "default/shop-canary -> api-canary:80 (weight 20%)" {
		t.Fatalf("expected the primary route to list the canary, got %q", got)
	}
	assertEdge(t, snapshot, primaryRoute.ID, nodeID(models.NodeKindService, "default", "api-canary"), "canary")

	canaryRoute := ingressRouteNode(t, snapshot, canary)
	if canaryRoute.Data.Properties["canary"] != "true" || canaryRoute.Data.Properties["canaryOf"] != "default/shop" || canaryRoute.Data.Properties["canaryRule"] != "weight 20%" {
		t.Fatalf("expected canary properties on the canary route, got %#v", canaryRoute.Data.Properties)
	}
	if canaryRoute.Data.Status != "Backends ready" {
		t.Fatalf("expected a canary with a primary to stay healthy, got %q: %s", canaryRoute.Data.Status, canaryRoute.Data.Properties["diagnosis"])
	}

	scoped := ingressScopedSnapshot(snapshot)
	assertEdge(t, scoped, primaryRoute.ID, nodeID(models.NodeKindService, "default", "api-canary"), "canary")
}

func TestBuildTopologyWarnsAboutCanaryWithoutPrimary(t *testing.T) {
	canary := shopIngress("shop-canary", "nginx", "api", 0, map[string]string{nginxCanaryAnnotation: "true", nginxCanaryByHeaderAnnotation: "X-Canary"})
	snapshot := buildOverlapTopology([]*networkingv1.Ingress{canary}, "api")

	route := ingressRouteNode(t, snapshot, canary)
	if route.Data.Status != "Warning" || !strings.Contains(route.Data.Properties["diagnosis"], "no primary Ingress") {
		t.Fatalf("expected a warning for a canary without a primary, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}
	if route.Data.Properties["canaryRule"] != "header X-Canary: always" {
		t.Fatalf("expected the header rule, got %q", route.Data.Properties["canaryRule"])
	}
}

func TestBuildTopologyWarnsAboutSeveralCanariesForOneLocation(t *testing.T) {
	primary := shopIngress("shop", "nginx", "api", 0, nil)
	first := shopIngress("shop-canary-a", "nginx", "api", 1, map[string]string{nginxCanaryAnnotation: "true", nginxCanaryWeightAnnotation: "10"})
	second := shopIngress("shop-canary-b", "nginx", "api", 2, map[string]string{nginxCanaryAnnotation: "true", nginxCanaryWeightAnnotation: "10"})
	snapshot := buildOverlapTopology([]*networkingv1.Ingress{primary, first, second}, "api")

	for _, ingress := range []*networkingv1.Ingress{first, second} {
		if route := ingressRouteNode(t, snapshot, ingress); route.Data.Status != "Conflict" {
			t.Fatalf("expected several canaries to conflict, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
		}
	}
	if route := ingressRouteNode(t, snapshot, primary); route.Data.Status != "Backends ready" {
		t.Fatalf("expected the primary to stay healthy, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}
}

func TestNginxCanaryRule(t *testing.T) {
	cases := []struct {
		annotations map[string]string
		want        string
		canary      bool
	}{
		{annotations: map[string]string{}, canary: false},
		{annotations: map[string]string{nginxCanaryAnnotation: "false", nginxCanaryWeightAnnotation: "50"}, canary: false},
		{annotations: map[string]string{nginxCanaryAnnotation: "true"}, want: "no canary rule, so it receives no traffic", canary: true},
		{annotations: map[string]string{nginxCanaryAnnotation: "true", nginxCanaryWeightAnnotation: "5", nginxCanaryWeightTotalAnnotation: "1000"}, want: "weight 5/1000", canary: true},
		{
			annotations: map[string]string{
				nginxCanaryAnnotation:              "True",
				nginxCanaryByHeaderAnnotation:      "X-Env",
				nginxCanaryByHeaderValueAnnotation: "beta",
				nginxCanaryByCookieAnnotation:      "beta",
				nginxCanaryWeightAnnotation:        "25",
			},
			want:   "header X-Env: beta, cookie beta: always, weight 25%",
			canary: true,
		},
		{annotations: map[string]string{nginxCanaryAnnotation: "true", nginxCanaryByHeaderAnnotation: "X-Env", nginxCanaryByHeaderPatternAnnot: "^beta"}, want: "header X-Env matching ^beta", canary: true},
	}
	for _, tc := range cases {
		ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Annotations: tc.annotations}}
		got, canary := nginxCanaryRule(ingress)
		if canary != tc.canary || got != tc.want {
			t.Fatalf("nginxCanaryRule(%v) = %q, %v; want %q, %v", tc.annotations, got, canary, tc.want, tc.canary)
		}
	}
}
