# NorthScope

![Build Status](https://github.com/emircanagac/northscope/actions/workflows/ci.yml/badge.svg)
![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)

Read-only Kubernetes traffic topology debugger. Follow a host's configured route from its controller to Services, Pods, and endpoints in one view.

![NorthScope topology view](docs/assets/northscope-simple-topology.gif)

- Search across namespaces by route, host, path, or Service.
- Use **Simple** for a compact traffic path or **Expanded** for individual Pods, endpoints, and Nodes.
- Identify missing Services, invalid backend ports, and backends with no ready endpoints.
- Discover Kubernetes Ingress, Gateway API, and F5 CIS resources when their APIs are available.

NorthScope reads Kubernetes configuration; it does not capture traffic or actively test connectivity. An external F5/LB entry is assumed, not discovered.

## Install

Requires Kubernetes 1.30+ and Helm 3.

```bash
helm repo add northscope https://emircanagac.github.io/northscope
helm repo update
helm upgrade --install northscope northscope/northscope \
  --namespace northscope \
  --create-namespace
```

Follow the access instructions printed by Helm, then search for and select a host route. Switch to Expanded when you need more detail.

For Ingress access and other options, see the [chart values](charts/northscope/values.yaml) and [production access guide](docs/production-access.md).

## Try A Demo

To explore healthy and broken routes in a test cluster:

```bash
kubectl apply -f https://raw.githubusercontent.com/emircanagac/northscope/main/examples/demo-topology.yaml
```

Select the `northscope` namespace. To remove the demo, run the same command with `delete` instead of `apply`.

## Status And Security

NorthScope is an early preview / pre-beta project. Real-cluster feedback and bug reports are welcome.

Kubernetes access is read-only (`get`, `list`, `watch`). NorthScope does not read Secrets or Pod logs. Protect access to the UI: topology data includes internal hostnames and IPs. See the [security policy](SECURITY.md).

## Resources

- [Troubleshooting](docs/troubleshooting.md)
- [Contributing](CONTRIBUTING.md)
- [Changelog](CHANGELOG.md)
- [Report an issue](https://github.com/emircanagac/northscope/issues)

Licensed under [Apache 2.0](LICENSE).
