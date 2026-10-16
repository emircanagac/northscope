package k8s

import (
	"reflect"
	"strings"

	"github.com/emircanagac/northscope/internal/models"
)

func ingressScopedSnapshot(snapshot models.TopologySnapshot) models.TopologySnapshot {
	nodesByID := make(map[string]models.Node, len(snapshot.Nodes))
	outgoing := make(map[string][]models.Edge, len(snapshot.Nodes))
	incoming := make(map[string][]models.Edge, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		nodesByID[node.ID] = node
	}
	for _, edge := range snapshot.Edges {
		outgoing[edge.Source] = append(outgoing[edge.Source], edge)
		incoming[edge.Target] = append(incoming[edge.Target], edge)
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
		node, ok := nodesByID[current]
		if !ok {
			continue
		}
		for _, neighbor := range scopedTopologyNeighbors(node, outgoing[current], incoming[current]) {
			if _, ok := visited[neighbor]; ok {
				continue
			}
			if _, ok := nodesByID[neighbor]; !ok {
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

func scopedTopologyNeighbors(node models.Node, outgoing, incoming []models.Edge) []string {
	neighbors := make([]string, 0, len(outgoing)+len(incoming))
	addOutgoing := func(kinds ...string) {
		for _, edge := range outgoing {
			if edgeHasKind(edge, kinds...) {
				neighbors = append(neighbors, edge.Target)
			}
		}
	}
	addIncoming := func(kinds ...string) {
		for _, edge := range incoming {
			if edgeHasKind(edge, kinds...) {
				neighbors = append(neighbors, edge.Source)
			}
		}
	}

	switch node.Data.Kind {
	case models.NodeKindIngress:
		addOutgoing("defines", "routes")
		addIncoming("controls", "fronts", "resolves")
	case models.NodeKindGateway:
		addOutgoing("attaches")
		addIncoming("controls", "fronts", "resolves")
	case models.NodeKindRoute:
		addOutgoing("routes")
		addIncoming("attaches", "defines", "resolves")
	case models.NodeKindService:
		addOutgoing("selects", "endpointslice", "endpoint", "externalname")
	case models.NodeKindPod:
		addIncoming("hosts")
	case models.NodeKindController:
		addOutgoing("controls")
		addIncoming("forwards", "balances")
	case models.NodeKindNodePort:
		addOutgoing("forwards")
		addIncoming("exposes")
	case models.NodeKindLoadBalancer:
		addOutgoing("balances", "exposes", "fronts")
		addIncoming("resolves")
	case models.NodeKindDNS:
		addOutgoing("resolves")
	}

	return neighbors
}

func edgeHasKind(edge models.Edge, kinds ...string) bool {
	if edge.Data == nil {
		return false
	}
	for _, kind := range kinds {
		if edge.Data.Kind == kind {
			return true
		}
	}
	return false
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
