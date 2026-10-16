# Changelog

All notable changes to NorthScope will be documented in this file.

This project follows a lightweight changelog style inspired by Keep a Changelog. NorthScope is an early preview: real-cluster validation for scale and platform-specific installs is still in progress.

## Unreleased

- The Helm chart now creates an Ingress by default (`ingress.enabled: true`, placeholder host `northscope.example.com`, default IngressClass), so an install only needs the host and, ideally, a TLS Secret. Set `ingress.enabled=false` to keep the previous ClusterIP-only behavior.
- Rewrote the README around the values-file Ingress install with sign-in, replaced the 7.7 MB animation with current light and dark screenshots, and added Helm NOTES hints for the placeholder host and for an Ingress without TLS while sign-in is enabled.
- Added built-in username/password sign-in, enabled by default in the Helm chart: the UI, `/api/*`, and the WebSocket now require a session (HMAC-signed, HttpOnly, SameSite=Lax cookie, 12 hours by default), failed sign-ins are throttled per client, and every response carries a restrictive Content-Security-Policy and other security headers. The chart generates a `<release>-auth` Secret with a random password and reuses it on upgrade, or mounts `auth.existingSecret`; `auth.enabled=false` restores open access. Without `-auth-dir` the binary keeps running unauthenticated and logs a warning.
- Fixed rebuilds of unchanged cluster state republishing the whole topology to every WebSocket client: node positions no longer depend on informer list order.
- Fixed an IngressClass and a GatewayClass with the same name (such as `nginx`, `traefik`, or `istio`) sharing one controller node: GatewayClass controllers now have their own node with `source: GatewayClass`, and Ingress and Gateway edges stay on their own controller.
- Added watch health reporting, so a stale or incomplete cache no longer looks live. List and watch failures are tracked per resource for every typed and dynamic informer and exposed through the new `/api/status` endpoint, `/readyz?verbose`, a `health` section in topology snapshots, and the `northscope_informer_synced`, `northscope_informer_degraded`, `northscope_watch_errors_total`, `northscope_last_watch_success_timestamp_seconds`, and `northscope_snapshot_generated_timestamp_seconds` metrics. `/readyz` is unchanged after the first snapshot. Startup logs the informers that are still unsynced every 30 seconds, and the UI shows the snapshot age and a `Degraded` banner instead of `Live config` when part of the cache may be stale.

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
