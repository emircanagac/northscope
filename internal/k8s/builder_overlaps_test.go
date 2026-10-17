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
	first := shopIngress("shop-canary-a", "nginx", "canary-a", 1, map[string]string{nginxCanaryAnnotation: "true", nginxCanaryWeightAnnotation: "10"})
	second := shopIngress("shop-canary-b", "nginx", "canary-b", 2, map[string]string{nginxCanaryAnnotation: "true", nginxCanaryWeightAnnotation: "10"})
	snapshot := buildOverlapTopology([]*networkingv1.Ingress{primary, first, second}, "api", "canary-a", "canary-b")

	for _, ingress := range []*networkingv1.Ingress{first, second} {
		route := ingressRouteNode(t, snapshot, ingress)
		if route.Data.Status != "Conflict" || !strings.Contains(route.Data.Properties["diagnosis"], "only one canary per host and path") {
			t.Fatalf("expected several canaries to conflict, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
		}
	}
	if route := ingressRouteNode(t, snapshot, primary); route.Data.Status != "Backends ready" {
		t.Fatalf("expected the primary to stay healthy, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}
}

// ingress-nginx keeps one traffic rule per canary Service. On a k3s cluster
// with ingress-nginx 1.15.1, a header canary for api.test and a weight-only
// canary for another host that both used Service shop-canary ended up with
// the weight rule only, so the header no longer selected the canary.
func TestBuildTopologyWarnsWhenCanariesShareAService(t *testing.T) {
	primary := shopIngress("shop", "nginx", "api", 0, nil)
	header := shopIngress("shop-canary", "nginx", "api-canary", 1, map[string]string{nginxCanaryAnnotation: "true", nginxCanaryByHeaderAnnotation: "X-Canary"})
	other := shopIngress("other-canary", "nginx", "api-canary", 2, map[string]string{nginxCanaryAnnotation: "true", nginxCanaryWeightAnnotation: "50"})
	other.Spec.Rules[0].Host = "other.example.com"
	snapshot := buildOverlapTopology([]*networkingv1.Ingress{primary, header, other}, "api", "api-canary")

	route := ingressRouteNode(t, snapshot, header)
	if route.Data.Status != "Conflict" || !strings.Contains(route.Data.Properties["diagnosis"], `Canary Service "api-canary" is also used by default/other-canary`) {
		t.Fatalf("expected a shared canary Service conflict, got %q: %s", route.Data.Status, route.Data.Properties["diagnosis"])
	}
	if route.Data.Properties["canaryServiceSharedWith"] != "default/other-canary" {
		t.Fatalf("expected the other canary to be listed, got %#v", route.Data.Properties)
	}
	// The canary without a primary reports that first.
	if route := ingressRouteNode(t, snapshot, other); route.Data.Status != "Warning" || route.Data.Properties["canaryServiceSharedWith"] != "default/shop-canary" {
		t.Fatalf("expected the lone canary warning with the shared Service noted, got %q: %#v", route.Data.Status, route.Data.Properties)
	}
}

// ingress-nginx attaches a canary to the primary route's backend Service, so
// every route to that Service receives the split. On the k3s validation
// cluster, shop.test answered from the canary of api.test because both
// Ingresses used Service shop-v1.
func TestBuildTopologyAppliesCanaryToRoutesSharingThePrimaryService(t *testing.T) {
	primary := shopIngress("api", "nginx", "api", 0, nil)
	primary.Spec.Rules[0].Host = "api.example.com"
	canary := shopIngress("api-canary", "nginx", "api-canary", 1, map[string]string{nginxCanaryAnnotation: "true", nginxCanaryWeightAnnotation: "30"})
	canary.Spec.Rules[0].Host = "api.example.com"
	shared := shopIngress("shop", "nginx", "api", 2, nil)
	traefik := shopIngress("shop-traefik", "traefik", "api", 3, nil)
	traefik.Spec.Rules[0].Host = "traefik.example.com"
	snapshot := buildOverlapTopology([]*networkingv1.Ingress{primary, canary, shared, traefik}, "api", "api-canary")

	canaryServiceID := nodeID(models.NodeKindService, "default", "api-canary")
	direct := ingressRouteNode(t, snapshot, primary)
	if got := direct.Data.Properties["canaries"]; got != "default/api-canary -> api-canary:80 (weight 30%)" {
		t.Fatalf("expected the direct canary on the primary, got %q", got)
	}
	sharedRoute := ingressRouteNode(t, snapshot, shared)
	if got := sharedRoute.Data.Properties["canaries"]; got != "default/api-canary -> api-canary:80 (weight 30%), applied through Service api shared with api.example.com/" {
		t.Fatalf("expected the canary on a route sharing the primary Service, got %q", got)
	}
	if sharedRoute.Data.Status != "Backends ready" {
		t.Fatalf("expected the shared route to stay healthy, got %q", sharedRoute.Data.Status)
	}
	assertEdge(t, snapshot, direct.ID, canaryServiceID, "canary")
	assertEdge(t, snapshot, sharedRoute.ID, canaryServiceID, "canary")

	traefikRoute := ingressRouteNode(t, snapshot, traefik)
	if traefikRoute.Data.Properties["canaries"] != "" {
		t.Fatalf("expected another controller's route not to get the nginx canary, got %q", traefikRoute.Data.Properties["canaries"])
	}
	assertNoEdge(t, snapshot, traefikRoute.ID, canaryServiceID, "canary")
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
