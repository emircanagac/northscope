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
			{ID: "ingress-route", Source: "ingress", Target: "route", Data: &models.EdgeData{Kind: "defines"}},
			{ID: "route-service", Source: "route", Target: "service-related", Data: &models.EdgeData{Kind: "routes"}},
			{ID: "service-pod", Source: "service-related", Target: "pod-related", Data: &models.EdgeData{Kind: "selects"}},
			{ID: "unrelated", Source: "service-unrelated", Target: "pod-unrelated", Data: &models.EdgeData{Kind: "selects"}},
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

func TestIngressScopedSnapshotDoesNotCrossSharedKubernetesNode(t *testing.T) {
	edge := func(id, source, target, kind string) models.Edge {
		return models.Edge{
			ID:     id,
			Source: source,
			Target: target,
			Data:   &models.EdgeData{Kind: kind},
		}
	}
	snapshot := models.TopologySnapshot{
		Nodes: []models.Node{
			{ID: "ingress", Data: models.NodeData{Kind: models.NodeKindIngress}},
			{ID: "route", Data: models.NodeData{Kind: models.NodeKindRoute}},
			{ID: "service-related", Data: models.NodeData{Kind: models.NodeKindService}},
			{ID: "pod-related", Data: models.NodeData{Kind: models.NodeKindPod}},
			{ID: "shared-node", Data: models.NodeData{Kind: models.NodeKindNode}},
			{ID: "service-unrelated", Data: models.NodeData{Kind: models.NodeKindService}},
			{ID: "pod-unrelated", Data: models.NodeData{Kind: models.NodeKindPod}},
		},
		Edges: []models.Edge{
			edge("ingress-route", "ingress", "route", "defines"),
			edge("route-service", "route", "service-related", "routes"),
			edge("service-pod", "service-related", "pod-related", "selects"),
			edge("node-related-pod", "shared-node", "pod-related", "hosts"),
			edge("unrelated-service-pod", "service-unrelated", "pod-unrelated", "selects"),
			edge("node-unrelated-pod", "shared-node", "pod-unrelated", "hosts"),
		},
	}

	scoped := ingressScopedSnapshot(snapshot)
	visible := make(map[string]struct{}, len(scoped.Nodes))
	for _, node := range scoped.Nodes {
		visible[node.ID] = struct{}{}
	}
	for _, id := range []string{"ingress", "route", "service-related", "pod-related", "shared-node"} {
		if _, ok := visible[id]; !ok {
			t.Fatalf("expected related node %q to remain visible", id)
		}
	}
	for _, id := range []string{"service-unrelated", "pod-unrelated"} {
		if _, ok := visible[id]; ok {
			t.Fatalf("expected unrelated node %q to be removed", id)
		}
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
