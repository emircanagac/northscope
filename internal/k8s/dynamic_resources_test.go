package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/emircanagac/northscope/internal/models"
)

func TestPreferredOptionalResourcesUsesOneServedVersionPerResource(t *testing.T) {
	v1 := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	v1beta1 := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "httproutes"}

	selected := preferredOptionalResources(map[schema.GroupVersionResource]struct{}{
		v1:      {},
		v1beta1: {},
	})

	var matches []schema.GroupVersionResource
	for _, item := range selected {
		if item.gvr.Group == v1.Group && item.gvr.Resource == v1.Resource {
			matches = append(matches, item.gvr)
		}
	}
	if len(matches) != 1 || matches[0] != v1 {
		t.Fatalf("expected preferred HTTPRoute GVR %v, got %v", v1, matches)
	}
}

func TestExternalHTTPRouteParsesPathsReferencesAndParentStatus(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"namespace": "web", "name": "store"},
		"spec": map[string]interface{}{
			"hostnames": []interface{}{"shop.example.com"},
			"parentRefs": []interface{}{
				map[string]interface{}{
					"group":       "gateway.networking.k8s.io",
					"kind":        "Gateway",
					"namespace":   "edge",
					"name":        "public",
					"sectionName": "https",
				},
			},
			"rules": []interface{}{
				map[string]interface{}{
					"matches": []interface{}{
						map[string]interface{}{"path": map[string]interface{}{"value": "/api"}},
						map[string]interface{}{"path": map[string]interface{}{"value": "/"}},
					},
					"backendRefs": []interface{}{
						map[string]interface{}{
							"group":     "",
							"kind":      "Service",
							"namespace": "backend",
							"name":      "api",
							"port":      int64(8080),
						},
					},
				},
			},
		},
		"status": map[string]interface{}{
			"parents": []interface{}{
				map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Accepted",
							"status": "False",
							"reason": "NotAllowedByListeners",
						},
					},
				},
			},
		},
	}}

	resource, ok := externalResourceFromUnstructured(obj, ExternalKindHTTPRoute, "gateway.networking.k8s.io/v1")
	if !ok {
		t.Fatal("expected HTTPRoute to be parsed")
	}
	if got := resource.Properties["status"]; got != "Accepted=False" {
		t.Fatalf("expected rejected status, got %q", got)
	}
	if got := resource.Properties["statusReason"]; got != "NotAllowedByListeners" {
		t.Fatalf("expected condition reason, got %q", got)
	}
	if len(resource.Paths) != 2 || resource.Paths[0] != "/" || resource.Paths[1] != "/api" {
		t.Fatalf("expected sorted HTTP paths, got %v", resource.Paths)
	}
	if len(resource.ParentRefs) != 1 || resource.ParentRefs[0].SectionName != "https" {
		t.Fatalf("expected complete parent reference, got %#v", resource.ParentRefs)
	}
	if len(resource.Backends) != 1 || resource.Backends[0].Namespace != "backend" || resource.Backends[0].Port != "8080" {
		t.Fatalf("expected cross-namespace backend reference, got %#v", resource.Backends)
	}
}

func TestExternalReferenceGrantParsesFromAndTo(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"namespace": "backend", "name": "allow-web"},
		"spec": map[string]interface{}{
			"from": []interface{}{
				map[string]interface{}{
					"group":     "gateway.networking.k8s.io",
					"kind":      "HTTPRoute",
					"namespace": "web",
				},
			},
			"to": []interface{}{
				map[string]interface{}{"group": "", "kind": "Service", "name": "api"},
			},
		},
	}}

	resource, ok := externalResourceFromUnstructured(obj, ExternalKindReferenceGrant, "gateway.networking.k8s.io/v1")
	if !ok {
		t.Fatal("expected ReferenceGrant to be parsed")
	}
	if len(resource.GrantFrom) != 1 || resource.GrantFrom[0].Namespace != "web" {
		t.Fatalf("unexpected from references: %#v", resource.GrantFrom)
	}
	if len(resource.GrantTo) != 1 || resource.GrantTo[0].Name != "api" {
		t.Fatalf("unexpected to references: %#v", resource.GrantTo)
	}
}

func TestBuildTopologyRequiresReferenceGrantForCrossNamespaceGatewayBackend(t *testing.T) {
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "backend", Name: "api"}}
	gateway := ExternalResource{Kind: ExternalKindGateway, Namespace: "edge", Name: "public"}
	route := ExternalResource{
		Kind:       ExternalKindHTTPRoute,
		Namespace:  "web",
		Name:       "store",
		ParentRefs: []ExternalParentRef{{Kind: "Gateway", Namespace: "edge", Name: "public"}},
		Backends:   []ExternalBackendRef{{Kind: "Service", Namespace: "backend", Name: "api"}},
		Properties: map[string]string{},
	}

	withoutGrant := BuildTopologyWithResources(nil, nil, []*corev1.Service{service}, nil, nil, []ExternalResource{gateway, route})
	routeID := routeNodeID("web", "store", string(ExternalKindHTTPRoute))
	serviceID := nodeID(models.NodeKindService, "backend", "api")
	assertNoEdge(t, withoutGrant, routeID, serviceID, "routes")
	if node := findNode(t, withoutGrant, routeID); node.Data.Status != "Reference error" {
		t.Fatalf("expected Reference error, got %q", node.Data.Status)
	}

	grant := ExternalResource{
		Kind:      ExternalKindReferenceGrant,
		Namespace: "backend",
		Name:      "allow-web",
		GrantFrom: []ExternalGrantFrom{{
			Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "web",
		}},
		GrantTo: []ExternalGrantTo{{Kind: "Service", Name: "api"}},
	}
	withGrant := BuildTopologyWithResources(nil, nil, []*corev1.Service{service}, nil, nil, []ExternalResource{gateway, route, grant})
	assertEdge(t, withGrant, routeID, serviceID, "routes")
}
