# Roadmap

NorthScope is pre-beta. The roadmap is intentionally focused on making Ingress topology debugging reliable before expanding the product surface.

## Near Term

- Validate Helm installation on real clusters.
- Collect screenshots and GIFs from realistic Ingress debugging scenarios.
- Improve topology layout for large namespaces and many host/path groups.
- Add more tests for IngressClass, named Service ports, EndpointSlice TargetRef, headless Services, and controller Service exposure.
- Keep the default topology simple: `F5 / LB -> Controller -> Ingress -> Service -> Pod summary`.

## Next

- Improve Gateway API discovery as supporting context.
- Add clearer unhealthy-path explanations in the UI.
- Document known controller patterns such as nginx, HAProxy, Traefik, F5 CIS, and cloud load balancers.
- Add release versioning and chart release notes.

## Later

- Decide whether Gateway API deserves a first-class topology mode.
- Explore optional export/share features for topology snapshots.
- Consider broader controller-specific integrations only if they stay read-only and low-friction.

## Non-Goals

- No in-cluster agents.
- No eBPF requirement.
- No mutating Kubernetes API permissions.
- No replacement for ingress controller logs, cloud load balancer inventory, or packet tracing.
