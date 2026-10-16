# Changelog

All notable changes to NorthScope will be documented in this file.

This project follows a lightweight changelog style inspired by Keep a Changelog. NorthScope is an early preview: real-cluster validation for scale and platform-specific installs is still in progress.

## Unreleased

First public release.

- Read-only topology for Ingress, Gateway API (GatewayClass, Gateway, HTTP/gRPC/TLS/TCP/UDP routes, ReferenceGrant), and F5 CIS (VirtualServer, TransportServer, IngressLink), traced from load balancers, NodePorts, and ingress controllers through Services and EndpointSlices or Endpoints to Pods and Nodes.
- Route diagnostics for missing Services, wrong backend ports, unsupported backends, selectors without Pods, Pods or endpoints that are not ready, and unresolved IngressClass ownership.
- Kubernetes 1.20+ support: the served EndpointSlice API is detected at startup (`discovery.k8s.io/v1`, `v1beta1` on 1.20, or legacy Endpoints only), and classes referenced only through the legacy `kubernetes.io/ingress.class` annotation get an inferred controller.
- Web UI with host and route search across namespaces, Simple and Expanded layouts, dark mode, and live WebSocket updates.
- Helm chart with read-only RBAC, optional namespace-scoped watches, optional Gateway API and F5 discovery, Ingress, NetworkPolicy, and PodDisruptionBudget options, and a default Pod that satisfies the Pod Security Standards `restricted` profile without a fixed UID.
- `/healthz`, `/readyz`, `/metrics`, `/api/version`, and `/api/status` endpoints.
- Watch health reporting: list and watch failures are tracked per resource for every typed and dynamic informer and exposed through `/api/status`, `/readyz?verbose`, a `health` section in topology snapshots, and the `northscope_informer_synced`, `northscope_informer_degraded`, `northscope_watch_errors_total`, `northscope_last_watch_success_timestamp_seconds`, and `northscope_snapshot_generated_timestamp_seconds` metrics. Startup logs the informers that are still unsynced every 30 seconds, and the UI shows the snapshot age and a `Degraded` banner instead of `Live config` when part of the cache may be stale.
- Multi-architecture (linux/amd64, linux/arm64) container images with SBOM and provenance attestations, and Helm chart packages with SHA-256 checksums.
