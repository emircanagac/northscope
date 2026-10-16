# Changelog

All notable changes to NorthScope will be documented in this file.

This project follows a lightweight changelog style inspired by Keep a Changelog. NorthScope is an early preview: real-cluster validation for scale and platform-specific installs is still in progress.

## Unreleased

- Fixed rebuilds of unchanged cluster state republishing the whole topology to every WebSocket client: node positions no longer depend on informer list order.

## 1.0.0 - 2026-10-16

First public release.

- Read-only topology for Ingress, Gateway API (GatewayClass, Gateway, HTTP/gRPC/TLS/TCP/UDP routes, ReferenceGrant), and F5 CIS (VirtualServer, TransportServer, IngressLink), traced from load balancers, NodePorts, and ingress controllers through Services and EndpointSlices or Endpoints to Pods and Nodes.
- Route diagnostics for missing Services, wrong backend ports, unsupported backends, selectors without Pods, Pods or endpoints that are not ready, and unresolved IngressClass ownership.
- Kubernetes 1.20+ support: the served EndpointSlice API is detected at startup (`discovery.k8s.io/v1`, `v1beta1` on 1.20, or legacy Endpoints only), and classes referenced only through the legacy `kubernetes.io/ingress.class` annotation get an inferred controller.
- Web UI with host and route search across namespaces, Simple and Expanded layouts, dark mode, and live WebSocket updates.
- Topology snapshots carry a per-process `instance` ID, so after a backend restart, rollout, or reconnect to another replica the UI shows the new process's topology right away instead of keeping the previous one until version numbers catch up.
- Helm chart with read-only RBAC, optional namespace-scoped watches, optional Gateway API and F5 discovery, Ingress, NetworkPolicy, and PodDisruptionBudget options, and a default Pod that satisfies the Pod Security Standards `restricted` profile without a fixed UID.
- `/healthz`, `/readyz`, `/metrics`, and `/api/version` endpoints.
- Helm chart repository landing page with install steps, requirements, and checksum verification, in a dark theme by default.
- Multi-architecture (linux/amd64, linux/arm64) container images with SBOM and provenance attestations, and Helm chart packages with SHA-256 checksums.
