# Security Policy

NorthScope is a read-only Kubernetes topology debugger. Security is especially important because the application runs inside a cluster and watches cluster resources.

## Supported Versions

NorthScope is currently pre-beta. Security fixes target the `main` branch and the latest published container image.

## Reporting a Vulnerability

Please do not open a public issue for security vulnerabilities.

Use GitHub private vulnerability reporting if it is enabled for this repository. If private reporting is not available, contact the maintainer listed in [MAINTAINERS.md](MAINTAINERS.md) before sharing public details.

Helpful report details:

- Affected version or commit
- Deployment method and Helm values, if relevant
- Impacted Kubernetes permissions or exposed data
- Reproduction steps
- Suggested mitigation, if known

## Security Model

NorthScope should remain read-only:

- Kubernetes RBAC should use only `get`, `list`, and `watch`.
- The application should not exec into Pods, proxy traffic, mutate resources, or create CRDs.
- The UI should not expose secrets or sensitive object data.
- The container should run as non-root with a minimal runtime image.

Changes that weaken this model require explicit maintainer review.

## Kubernetes Permissions

The default Helm chart creates a ClusterRole with read-only permissions only. NorthScope needs cluster-wide reads because ingress traffic paths commonly cross namespaces and node placement is part of the topology.

| API group | Resources | Purpose |
| --- | --- | --- |
| core | namespaces, nodes, pods, services | Namespace filtering, backend Pod readiness, Service routing, and Node placement |
| discovery.k8s.io | endpointslices | Endpoint and selector-less Service resolution |
| networking.k8s.io | ingressclasses, ingresses | Ingress ownership, hosts, paths, and controller mapping |
| gateway.networking.k8s.io | gatewayclasses, gateways, grpcroutes, httproutes, tcproutes, tlsroutes, udproutes | Optional Gateway API topology when the CRDs are installed |
| cis.f5.com | ingresslinks, virtualservers, transportservers | Optional F5 CIS topology when the CRDs are installed |

The chart does not grant access to Secrets, ConfigMaps, Events, Pod logs, Pod exec, or resource mutation verbs such as `create`, `update`, `patch`, or `delete`.

If you disable chart-managed RBAC with `rbac.create=false`, provide an equivalent read-only role before starting NorthScope.

## Exposed Data

NorthScope can display internal topology metadata, including:

- hostnames and Ingress paths
- Service names and ports
- Pod names, readiness state, and Pod IPs
- Node names
- EndpointSlice addresses, including external endpoint IPs for selector-less Services
- Gateway API and F5 CIS object names and selected routing fields when those CRDs are present

Run NorthScope behind trusted internal access controls. If exposed through Ingress, configure TLS and authentication at the ingress controller, identity proxy, or platform edge.

## Deployment Hardening

Recommended production settings:

- use a private/internal hostname
- enable TLS and authentication before exposing the UI outside a trusted network
- enable `networkPolicy` where your CNI enforces NetworkPolicy
- enable `podDisruptionBudget` when running more than one replica
- keep `securityContext` and `podSecurityContext` hardened
