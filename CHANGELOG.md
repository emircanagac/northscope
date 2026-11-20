# Changelog

All notable changes to NorthScope will be documented in this file.

This project follows a lightweight changelog style inspired by Keep a Changelog. NorthScope is an early preview: real-cluster validation for scale and platform-specific installs is still in progress.

## 1.4.0 - 2026-11-20

- Redesigned UI. The layout keeps the route list beside the graph, while colors, type, borders, and controls follow GitHub's open source Primer design system: neutral surfaces, one accent color, and color reserved for health. Health is also shown by icon shape, so it does not depend on color alone. Resource kinds have their own icons (from Lucide, ISC License), route details have a copy button for the `kubectl` command, and the UI code is split into components.
- Accessibility: the namespace picker is a keyboard-operable combobox (arrow keys, Home, End, Enter, Escape), the page has landmarks and headings, connection status is announced, focus is always visible, and reduced-motion settings turn off animations. CI checks WCAG 2.1 AA with axe in both themes.
- Live updates are sent as deltas. After the first full snapshot, a browser receives only the nodes and edges that changed: a change to one Pod in a 20,000-Pod cluster is under 1 KB instead of about 20 MB. A browser tab still running an older UI after an upgrade keeps receiving full snapshots.
- Pods, Nodes, Endpoints, and EndpointSlices are trimmed to the fields NorthScope reads before they are cached; a Node takes about 1.4 KB instead of 12 KB. Updates that cannot change the topology, such as status heartbeats, restart counts, Endpoints leader-election renewals on older control planes, and informer resyncs, no longer rebuild the snapshot.
- Fixed: on a cluster where something changed more often than every 250 ms, the debounce kept restarting and the topology only refreshed on the 10-minute resync. A rebuild now runs at most 2 seconds after the first change.
- Change detection fingerprints nodes and edges instead of comparing whole snapshots with reflection, which is nearly twice as fast and allocates a sixth of the memory.
- The UI lists routes without drawing them and builds the graph for the selected host only. Layout no longer sorts a column once per node, the graph renders only the nodes in view, and search filters in the background, so a 2,000-host cluster stays responsive.
- New metrics: the `northscope_snapshot_build_seconds` histogram, `northscope_snapshot_deltas_total`, `northscope_snapshot_encoded_bytes`, `northscope_websocket_messages_total` and `northscope_websocket_payload_bytes_total` by message type, and `northscope_watch_events_total` by resource and event.
- Logs are structured: messages carry fields such as `resource`, `error`, and `version`, as `key=value` in the text format and as JSON keys with `logFormat: json`. Several messages were reworded, so update log searches that match their old text.
- Benchmarks at 100, 2,000, and 20,000 Pods, with the results in [docs/production-access.md](docs/production-access.md#capacity-guidance), and `go run ./hack/demo`, which runs NorthScope against a generated cluster for UI development and screenshots.
- Dependabot opens one monthly pull request for Go modules, UI packages, GitHub Actions, and Docker base images instead of one per group.
- CI fails when `go.mod` and the golang build image name different Go releases, because tests and vulnerability scans use `go.mod` while release images use the image. Dependabot only bumps the image.
- Updated `azure/setup-helm` and `docker/login-action`.

## 1.3.0 - 2026-11-20

- Signing out now revokes the session instead of only clearing the cookie: a copy of the cookie is rejected, and open WebSocket streams for that session close within a few seconds. Revocations are kept in process memory until the session would have expired, so with several replicas or after a restart a copied cookie stays valid until it expires (see [SECURITY.md](SECURITY.md)).
- Each topology snapshot is encoded once and shared by every connected browser instead of being encoded per connection, and WebSocket messages are compressed with permessage-deflate when the browser supports it, which shrinks the repetitive topology JSON several times over.
- New `-log-format=json` flag (chart value `logFormat: json`) writes NorthScope and Kubernetes client messages as one JSON object per line for log collectors.
- Snapshot builds reuse each Service's selector matches instead of matching every Pod twice.
- `/api/topology` responses carry `Cache-Control: no-store` like the other API endpoints, because they list internal hostnames and IPs, and idle keep-alive HTTP connections are closed after two minutes.
- CI runs the race detector on the sign-in package too.
- Built with Go 1.27.4, which fixes an HTTP/1 server crash when a request body is limited with `MaxBytesReader`, as the sign-in form does (GO-2026-6859), and stops trusting compromised intermediate certificates in `crypto/x509` (GO-2026-6809). `golang.org/x/net` is updated to v0.62.0 (GO-2026-6860, GO-2026-6866; NorthScope does not call the affected code).
- Updated the Node.js build image, `actions/setup-node`, `actions/upload-artifact`, and `docker/login-action`.

## 1.2.0 - 2026-10-17

- Routes now warn when several Ingresses define the same host, path, and path type for one controller, because only one of their backends receives that traffic. The warning names the other Ingresses and the one ingress-nginx serves (the oldest).
- ingress-nginx canary Ingresses (`nginx.ingress.kubernetes.io/canary`) are linked to the routes they split: the canary Service is drawn as a second backend with its header, cookie, or weight rule, on the primary route and on every other route of the controller to the same Service, because ingress-nginx attaches canaries to the backend Service. A canary without a primary Ingress, canaries that share a canary Service (their rules override each other), and a second canary for the same host and path are flagged. Validated against ingress-nginx 1.15.1 on k3s 1.34.
- Selecting a host now shows each route's diagnosis, suggested next step, and `kubectl` command in the sidebar, together with notes about overlapping Ingresses and canaries.
- The namespace, route search, selected route, and layout mode are kept in the URL, so a link opens the same route for a teammate (after sign-in), and a Copy link button copies it.
- CI now lints Go code with golangci-lint and the UI with oxlint, including the React hooks rules, and runs the UI unit tests.

## 1.1.1 - 2026-10-17

- Added a chart README, a logo, and Artifact Hub metadata (license, category, links, screenshots, container image, and release changes) so the chart page on Artifact Hub is complete.
- Added `values.schema.json`, which rejects unknown keys and invalid values such as an unsupported `service.type` and documents the chart values on Artifact Hub.
- CI now checks that the default image tag and the Artifact Hub image annotation match the chart `appVersion`.

## 1.1.0 - 2026-10-16

- Linked Ingresses without a resolvable IngressClass to the controller whose Service load balancer address appears in the Ingress status, so controllers that never created an IngressClass (such as Traefik on k3s 1.21) show their LoadBalancer and NodePort entry path instead of a missing-controller warning.
- Routes now warn when running Pods match a Service selector but are not endpoints for the Service port the Ingress uses (for example a named `targetPort` the Pods do not expose: the EndpointSlice lists them without that port), because those Pods receive no traffic.
- ExternalName Services without ports no longer get a false "no matching port" error.
- With `watchNamespace` set, Gateway API references to Gateways or Services in other namespaces are reported as outside the watch scope instead of as missing.
- Sign-in throttling no longer locks every user out behind an ingress controller: clients forwarded by a proxy on a private, shared, or loopback address are identified by the address the proxy appended to `X-Forwarded-For` (or `X-Real-IP`), and a wider per-proxy budget stops forged forwarded addresses from bypassing the limit.
- Gateway API and F5 CIS resources are now read from their informer caches instead of being re-listed from the API server whenever one of them changed, which removes a LIST storm on clusters with frequent Gateway or route status updates. Served CRDs are rediscovered every 2 minutes in the background, so CRDs installed, removed, or upgraded after startup start or stop their watches.

## 1.0.0 - 2026-10-16

First public release.

- Read-only topology for Ingress, Gateway API (GatewayClass, Gateway, HTTP/gRPC/TLS/TCP/UDP routes, ReferenceGrant), and F5 CIS (VirtualServer, TransportServer, IngressLink), traced from load balancers, NodePorts, and ingress controllers through Services and EndpointSlices or Endpoints to Pods and Nodes. IngressClass and GatewayClass controllers with the same name are kept apart.
- Route diagnostics for missing Services, wrong backend ports, unsupported backends, selectors without Pods, Pods or endpoints that are not ready, and unresolved IngressClass ownership.
- Kubernetes 1.20+ support: the served EndpointSlice API is detected at startup (`discovery.k8s.io/v1`, `v1beta1` on 1.20, or legacy Endpoints only), and classes referenced only through the legacy `kubernetes.io/ingress.class` annotation get an inferred controller.
- Web UI with host and route search across namespaces, Simple and Expanded layouts, dark mode, and live WebSocket updates. Unchanged cluster state is not republished, and after a backend restart or failover the UI switches to the new process's topology immediately.
- Watch health reporting: per-resource list/watch status through `/api/status`, `/readyz?verbose`, the snapshot `health` section, and metrics, with a `Degraded` banner and snapshot age in the UI when part of the cache may be stale.
- Built-in username/password sign-in, enabled by default: the UI, `/api/*`, and the WebSocket require a signed session cookie, failed sign-ins are throttled, and responses carry a restrictive Content-Security-Policy. The chart generates a `<release>-auth` Secret with a random password, or uses `auth.existingSecret`.
- Helm chart with an Ingress enabled by default (set your host and, ideally, a TLS Secret), read-only RBAC, optional namespace-scoped watches, optional Gateway API and F5 discovery, NetworkPolicy and PodDisruptionBudget options, and a default Pod that satisfies the Pod Security Standards `restricted` profile without a fixed UID.
- `/healthz`, `/readyz`, `/metrics`, `/api/version`, `/api/session`, and `/api/status` endpoints.
- Helm chart repository landing page with install steps, requirements, and checksum verification.
- Multi-architecture (linux/amd64, linux/arm64) container images with SBOM and provenance attestations, and Helm chart packages with SHA-256 checksums.
