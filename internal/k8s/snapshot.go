package k8s

import (
	"reflect"
	"strings"

	"github.com/emircanagac/northscope/internal/models"
)

func ingressScopedSnapshot(snapshot models.TopologySnapshot) models.TopologySnapshot {
	adjacency := make(map[string][]string, len(snapshot.Nodes))
	for _, edge := range snapshot.Edges {
		adjacency[edge.Source] = append(adjacency[edge.Source], edge.Target)
		adjacency[edge.Target] = append(adjacency[edge.Target], edge.Source)
	}

	visited := make(map[string]struct{}, len(snapshot.Nodes))
	queue := make([]string, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if topologyRoot(node) {
			visited[node.ID] = struct{}{}
			queue = append(queue, node.ID)
		}
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, neighbor := range adjacency[current] {
			if _, ok := visited[neighbor]; ok {
				continue
			}
			visited[neighbor] = struct{}{}
			queue = append(queue, neighbor)
		}
	}

	nodes := make([]models.Node, 0, len(visited))
	for _, node := range snapshot.Nodes {
		if _, ok := visited[node.ID]; ok {
			nodes = append(nodes, node)
		}
	}

	edges := make([]models.Edge, 0, len(snapshot.Edges))
	for _, edge := range snapshot.Edges {
		_, sourceVisible := visited[edge.Source]
		_, targetVisible := visited[edge.Target]
		if sourceVisible && targetVisible {
			edges = append(edges, edge)
		}
	}

	snapshot.Nodes = nodes
	snapshot.Edges = edges
	return snapshot
}

func topologyRoot(node models.Node) bool {
	switch node.Data.Kind {
	case models.NodeKindIngress, models.NodeKindGateway, models.NodeKindRoute:
		return true
	case models.NodeKindLoadBalancer:
		return strings.EqualFold(node.Data.Properties["provider"], "F5")
	default:
		return false
	}
}

func sameTopology(left, right models.TopologySnapshot) bool {
	return reflect.DeepEqual(left.Inventory, right.Inventory) &&
		reflect.DeepEqual(left.Nodes, right.Nodes) &&
		reflect.DeepEqual(left.Edges, right.Edges)
}
