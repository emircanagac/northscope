# Troubleshooting

This guide covers the common checks for NorthScope itself and for topology results that look incomplete or surprising.

## NorthScope Does Not Become Ready

Check the Pod and readiness endpoint:

```bash
kubectl -n northscope get pods
kubectl -n northscope logs deploy/northscope
kubectl -n northscope port-forward svc/northscope 18080:80
curl -i http://localhost:18080/readyz
```

Expected startup logs include:

```text
NorthScope starting
HTTP server listening on :8080
NorthScope Kubernetes caches synced
NorthScope ready: snapshot v1, ... nodes, ... edges
```

If `/readyz` returns `503`, the watcher has not generated the first topology snapshot yet. Check RBAC and API connectivity.

## No Ingress Routes Are Listed

Verify that Ingress resources exist and that NorthScope can read them:

```bash
kubectl get ingress -A
kubectl auth can-i list ingresses.networking.k8s.io --as=system:serviceaccount:northscope:northscope -A
kubectl auth can-i list ingressclasses.networking.k8s.io --as=system:serviceaccount:northscope:northscope
```

NorthScope defaults to **All namespaces**. If a namespace filter is selected, clear it and search again by host, ingress name, path, or Service.

## Routes Show Missing Service Or Missing Port

Compare the Ingress backend with the Service name and ports:

```bash
kubectl describe ingress -n <namespace> <ingress>
kubectl get svc -n <namespace> <service> -o yaml
```

For named backend ports, the Ingress name must match a Service port name.

## Routes Have No Ready Pods

Check Pod readiness, Service selectors, and endpoints:

```bash
kubectl get pods -n <namespace> -o wide
kubectl get svc -n <namespace> <service> -o yaml
kubectl get endpointslice -n <namespace> -l kubernetes.io/service-name=<service> -o wide
kubectl get endpoints -n <namespace> <service> -o yaml
```

For selector-less Services, NorthScope reads manually managed EndpointSlices and legacy Endpoints. If both exist, EndpointSlices are preferred.

## Controller Or NodePort Looks Wrong

NorthScope infers the external entry path from controller-like NodePort or LoadBalancer Services and IngressClass ownership. Validate the controller Service and class:

```bash
kubectl get ingressclass
kubectl get svc -A | grep -Ei 'ingress|controller|nginx|traefik|haproxy'
```

If several controllers are installed, check that every Ingress has the intended `ingressClassName`.

## Optional Gateway API Or F5 Objects Are Missing

Gateway API and F5 CIS resources are optional. NorthScope only queries those resources when their CRDs are installed and discoverable.

```bash
kubectl get crd | grep -E 'gateway.networking.k8s.io|cis.f5.com'
```

## Metrics

NorthScope exposes Prometheus-compatible text metrics at `/metrics`:

```bash
kubectl -n northscope port-forward svc/northscope 18080:80
curl http://localhost:18080/metrics
```

Useful metrics:

- `northscope_ready`
- `northscope_snapshot_version`
- `northscope_snapshot_nodes`
- `northscope_snapshot_edges`
- `northscope_snapshot_builds_total`
- `northscope_snapshot_build_errors_total`
- `northscope_snapshot_build_duration_seconds`
- `northscope_websocket_clients`

If `northscope_snapshot_build_errors_total` increases, check NorthScope logs for `build topology snapshot failed`.
