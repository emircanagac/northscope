package k8s

import (
	"testing"

	"github.com/emircanagac/northscope/internal/models"
)

func TestIngressScopedSnapshotRemovesUnrelatedClusterResources(t *testing.T) {
	snapshot := models.TopologySnapshot{
		Inventory: models.ClusterInventory{
			Controllers: 1,
			Ingresses:   1,
			Services:    2,
			Pods:        2,
			Nodes:       2,
		},
		Nodes: []models.Node{
			{ID: "ingress", Data: models.NodeData{Kind: models.NodeKindIngress}},
			{ID: "route", Data: models.NodeData{Kind: models.NodeKindRoute}},
			{ID: "service-related", Data: models.NodeData{Kind: models.NodeKindService}},
			{ID: "pod-related", Data: models.NodeData{Kind: models.NodeKindPod}},
			{ID: "service-unrelated", Data: models.NodeData{Kind: models.NodeKindService}},
			{ID: "pod-unrelated", Data: models.NodeData{Kind: models.NodeKindPod}},
		},
		Edges: []models.Edge{
			{ID: "ingress-route", Source: "ingress", Target: "route"},
			{ID: "route-service", Source: "route", Target: "service-related"},
			{ID: "service-pod", Source: "service-related", Target: "pod-related"},
			{ID: "unrelated", Source: "service-unrelated", Target: "pod-unrelated"},
		},
	}

	scoped := ingressScopedSnapshot(snapshot)

	if len(scoped.Nodes) != 4 {
		t.Fatalf("expected 4 ingress-related nodes, got %d", len(scoped.Nodes))
	}
	if len(scoped.Edges) != 3 {
		t.Fatalf("expected 3 ingress-related edges, got %d", len(scoped.Edges))
	}
	if scoped.Inventory.Services != 2 || scoped.Inventory.Pods != 2 {
		t.Fatalf("expected cluster inventory to remain complete, got %#v", scoped.Inventory)
	}
}

func TestSameTopologyIgnoresSnapshotMetadata(t *testing.T) {
	left := models.TopologySnapshot{
		Version:   1,
		Inventory: models.ClusterInventory{Ingresses: 1},
		Nodes:     []models.Node{{ID: "ingress"}},
	}
	right := left
	right.Version = 2

	if !sameTopology(left, right) {
		t.Fatal("expected versions and timestamps to be ignored")
	}
}
