package k8s

import (
	"encoding/binary"
	"hash/maphash"
	"slices"
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
		addOutgoing("routes", "canary")
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

// topologyIndex fingerprints every node and edge of a snapshot by ID. Comparing
// two indexes tells whether a rebuilt snapshot changed and which nodes and
// edges a delta must carry, without walking both snapshots with reflection.
//
// Node positions are left out: they are the builder's default grid, which
// shifts whenever a node is added before others of its kind, and the UI lays
// nodes out itself. A delta therefore never carries a node only because its
// default position moved.
type topologyIndex struct {
	inventory models.ClusterInventory
	nodes     map[string]uint64
	edges     map[string]uint64
}

func indexTopology(snapshot models.TopologySnapshot) topologyIndex {
	index := topologyIndex{
		inventory: snapshot.Inventory,
		nodes:     make(map[string]uint64, len(snapshot.Nodes)),
		edges:     make(map[string]uint64, len(snapshot.Edges)),
	}
	var h fingerprint
	for i := range snapshot.Nodes {
		index.nodes[snapshot.Nodes[i].ID] = h.node(&snapshot.Nodes[i])
	}
	for i := range snapshot.Edges {
		index.edges[snapshot.Edges[i].ID] = h.edge(&snapshot.Edges[i])
	}
	return index
}

func (i topologyIndex) equal(other topologyIndex) bool {
	return i.inventory == other.inventory && sameFingerprints(i.nodes, other.nodes) && sameFingerprints(i.edges, other.edges)
}

func sameFingerprints(left, right map[string]uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for id, sum := range left {
		if other, ok := right[id]; !ok || other != sum {
			return false
		}
	}
	return true
}

func sameTopology(left, right models.TopologySnapshot) bool {
	return indexTopology(left).equal(indexTopology(right))
}

// fingerprintSeed is fixed per process; fingerprints are only compared within
// one process. A 64-bit collision would hide one changed node until its next
// change, which is far less likely than any real cluster event.
var fingerprintSeed = maphash.MakeSeed()

type fingerprint struct {
	h    maphash.Hash
	pair maphash.Hash
	buf  [8]byte
}

func (f *fingerprint) reset() {
	f.h.SetSeed(fingerprintSeed)
}

func (f *fingerprint) string(value string) {
	binary.LittleEndian.PutUint64(f.buf[:], uint64(len(value)))
	_, _ = f.h.Write(f.buf[:])
	_, _ = f.h.WriteString(value)
}

func (f *fingerprint) uint(value uint64) {
	binary.LittleEndian.PutUint64(f.buf[:], value)
	_, _ = f.h.Write(f.buf[:])
}

// labels hashes a map independently of its iteration order by summing the
// hash of every key and value pair, so no keys are sorted or allocated.
func (f *fingerprint) labels(values map[string]string) {
	var sum uint64
	for key, value := range values {
		f.pair.SetSeed(fingerprintSeed)
		binary.LittleEndian.PutUint64(f.buf[:], uint64(len(key)))
		_, _ = f.pair.Write(f.buf[:])
		_, _ = f.pair.WriteString(key)
		_, _ = f.pair.WriteString(value)
		sum += f.pair.Sum64()
	}
	f.uint(uint64(len(values)))
	f.uint(sum)
}

func (f *fingerprint) node(node *models.Node) uint64 {
	f.reset()
	f.string(node.ID)
	f.string(node.Type)
	f.string(node.Data.Label)
	f.string(string(node.Data.Kind))
	f.string(node.Data.Namespace)
	f.string(node.Data.Name)
	f.string(node.Data.Status)
	f.string(node.Data.Phase)
	f.labels(node.Data.Metadata)
	f.labels(node.Data.Properties)
	return f.h.Sum64()
}

func (f *fingerprint) edge(edge *models.Edge) uint64 {
	f.reset()
	f.string(edge.ID)
	f.string(edge.Source)
	f.string(edge.Target)
	f.string(edge.Type)
	f.string(edge.Label)
	if edge.Animated {
		f.uint(1)
	} else {
		f.uint(0)
	}
	if edge.Data == nil {
		f.uint(0)
		return f.h.Sum64()
	}
	f.uint(1)
	f.string(edge.Data.Kind)
	f.labels(edge.Data.Metadata)
	f.labels(edge.Data.Properties)
	return f.h.Sum64()
}

// maxDeltaShare is the share of nodes and edges above which a full snapshot is
// sent instead of a delta: past it a delta saves little and costs the browser
// a merge.
const maxDeltaShare = 0.5

// topologyDelta lists what changed from the snapshot indexed by previous to
// next. It returns nil when most of the topology changed.
func topologyDelta(previous, next topologyIndex, snapshot models.TopologySnapshot, baseVersion int64) *models.TopologyDelta {
	delta := &models.TopologyDelta{
		Type:        models.TopologyDeltaType,
		Instance:    snapshot.Instance,
		Version:     snapshot.Version,
		BaseVersion: baseVersion,
		GeneratedAt: snapshot.GeneratedAt,
		Inventory:   snapshot.Inventory,
		Health:      snapshot.Health,
	}
	for i := range snapshot.Nodes {
		node := &snapshot.Nodes[i]
		if sum, ok := previous.nodes[node.ID]; !ok || sum != next.nodes[node.ID] {
			delta.UpsertNodes = append(delta.UpsertNodes, *node)
		}
	}
	for id := range previous.nodes {
		if _, ok := next.nodes[id]; !ok {
			delta.RemoveNodes = append(delta.RemoveNodes, id)
		}
	}
	for i := range snapshot.Edges {
		edge := &snapshot.Edges[i]
		if sum, ok := previous.edges[edge.ID]; !ok || sum != next.edges[edge.ID] {
			delta.UpsertEdges = append(delta.UpsertEdges, *edge)
		}
	}
	for id := range previous.edges {
		if _, ok := next.edges[id]; !ok {
			delta.RemoveEdges = append(delta.RemoveEdges, id)
		}
	}

	changed := len(delta.UpsertNodes) + len(delta.RemoveNodes) + len(delta.UpsertEdges) + len(delta.RemoveEdges)
	if float64(changed) > maxDeltaShare*float64(len(snapshot.Nodes)+len(snapshot.Edges)) {
		return nil
	}
	slices.Sort(delta.RemoveNodes)
	slices.Sort(delta.RemoveEdges)
	return delta
}
