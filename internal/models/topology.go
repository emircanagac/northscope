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
	Controllers int `json:"controllers"`
	Ingresses   int `json:"ingresses"`
	Services    int `json:"services"`
	Pods        int `json:"pods"`
	Nodes       int `json:"nodes"`
}

type TopologySnapshot struct {
	Version     int64            `json:"version"`
	GeneratedAt time.Time        `json:"generatedAt"`
	Inventory   ClusterInventory `json:"inventory"`
	Nodes       []Node           `json:"nodes"`
	Edges       []Edge           `json:"edges"`
}
