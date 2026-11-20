package models

import "time"

type NodeKind string

const (
	NodeKindInternet      NodeKind = "Internet"
	NodeKindDNS           NodeKind = "DNS"
	NodeKindGateway       NodeKind = "Gateway"
	NodeKindIngress       NodeKind = "Ingress"
	NodeKindController    NodeKind = "Controller"
	NodeKindLoadBalancer  NodeKind = "LoadBalancer"
	NodeKindNodePort      NodeKind = "NodePort"
	NodeKindNode          NodeKind = "Node"
	NodeKindRoute         NodeKind = "Route"
	NodeKindService       NodeKind = "Service"
	NodeKindEndpoint      NodeKind = "Endpoint"
	NodeKindEndpointSlice NodeKind = "EndpointSlice"
	NodeKindPod           NodeKind = "Pod"
)

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type NodeData struct {
	Label      string            `json:"label"`
	Kind       NodeKind          `json:"kind"`
	Namespace  string            `json:"namespace,omitempty"`
	Name       string            `json:"name"`
	Status     string            `json:"status,omitempty"`
	Phase      string            `json:"phase,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type Node struct {
	ID       string   `json:"id"`
	Type     string   `json:"type,omitempty"`
	Position Position `json:"position"`
	Data     NodeData `json:"data"`
}

type EdgeData struct {
	Kind       string            `json:"kind"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

type Edge struct {
	ID       string    `json:"id"`
	Source   string    `json:"source"`
	Target   string    `json:"target"`
	Type     string    `json:"type,omitempty"`
	Label    string    `json:"label,omitempty"`
	Animated bool      `json:"animated,omitempty"`
	Data     *EdgeData `json:"data,omitempty"`
}

type ClusterInventory struct {
	IngressClasses int `json:"ingressClasses"`
	Ingresses      int `json:"ingresses"`
	GatewayClasses int `json:"gatewayClasses"`
	Gateways       int `json:"gateways"`
	GatewayRoutes  int `json:"gatewayRoutes"`
	F5Resources    int `json:"f5Resources"`
	Services       int `json:"services"`
	Pods           int `json:"pods"`
	Nodes          int `json:"nodes"`
}

type TopologySnapshot struct {
	// Instance identifies the NorthScope process that published the snapshot.
	// Version only increases within one instance, so clients must not compare
	// versions across instances (restarts, rollouts, or other replicas).
	Instance    string           `json:"instance"`
	Version     int64            `json:"version"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Inventory   ClusterInventory `json:"inventory"`
	Health      SnapshotHealth   `json:"health"`
	Nodes       []Node           `json:"nodes"`
	Edges       []Edge           `json:"edges"`
}

// TopologyDeltaType marks a WebSocket message as a TopologyDelta. Full
// snapshots carry no type.
const TopologyDeltaType = "delta"

// TopologyDelta turns the snapshot with BaseVersion into the snapshot with
// Version: nodes and edges are replaced or added by ID, removed IDs are
// dropped, and the inventory and health replace the old ones. Nodes and edges
// stay sorted by ID. Unchanged nodes keep the default position they had in
// the base snapshot.
type TopologyDelta struct {
	Type        string           `json:"type"`
	Instance    string           `json:"instance"`
	Version     int64            `json:"version"`
	BaseVersion int64            `json:"baseVersion"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Inventory   ClusterInventory `json:"inventory"`
	Health      SnapshotHealth   `json:"health"`
	UpsertNodes []Node           `json:"upsertNodes,omitempty"`
	RemoveNodes []string         `json:"removeNodes,omitempty"`
	UpsertEdges []Edge           `json:"upsertEdges,omitempty"`
	RemoveEdges []string         `json:"removeEdges,omitempty"`
}

type HealthStatus string

const (
	HealthOK       HealthStatus = "ok"
	HealthDegraded HealthStatus = "degraded"
	HealthSyncing  HealthStatus = "syncing"
)

// SnapshotHealth summarizes Kubernetes watch health when a snapshot was
// published. A degraded resource failed its latest list or watch, so its part
// of the topology may be stale or missing.
type SnapshotHealth struct {
	Status   HealthStatus       `json:"status"`
	Degraded []DegradedResource `json:"degraded,omitempty"`
}

type DegradedResource struct {
	Resource string    `json:"resource"`
	Synced   bool      `json:"synced"`
	Since    time.Time `json:"since"`
	Error    string    `json:"error"`
}

// ResourceHealth reports list and watch health for one watched resource.
type ResourceHealth struct {
	Resource      string     `json:"resource"`
	Optional      bool       `json:"optional,omitempty"`
	Synced        bool       `json:"synced"`
	Degraded      bool       `json:"degraded"`
	DegradedSince *time.Time `json:"degradedSince,omitempty"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	LastError     string     `json:"lastError,omitempty"`
	LastErrorAt   *time.Time `json:"lastErrorAt,omitempty"`
	ErrorCount    uint64     `json:"errorCount"`
}

// WatcherStatus is served by /api/status.
type WatcherStatus struct {
	Status          HealthStatus     `json:"status"`
	Ready           bool             `json:"ready"`
	SnapshotVersion int64            `json:"snapshotVersion"`
	GeneratedAt     *time.Time       `json:"generatedAt,omitempty"`
	Degraded        []string         `json:"degraded,omitempty"`
	Unsynced        []string         `json:"unsynced,omitempty"`
	Resources       []ResourceHealth `json:"resources"`
}
