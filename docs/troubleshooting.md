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
NorthScope compatibility: using EndpointSlice API discovery.k8s.io/v1
HTTP server listening on :8080
NorthScope Kubernetes caches synced
NorthScope ready: snapshot v1, ... nodes, ... edges
```

If `/readyz` returns `503`, the watcher has not generated the first topology snapshot yet. Check RBAC and API connectivity.

NorthScope supports Kubernetes 1.20 and newer. It detects the served EndpointSlice API at startup: Kubernetes 1.21+ uses `discovery.k8s.io/v1`, Kubernetes 1.20 uses `discovery.k8s.io/v1beta1`, and clusters that serve neither use legacy Endpoints only. If the API server cannot be reached during this check, NorthScope exits so Kubernetes restarts it instead of watching an API version the cluster may not serve.

## The NorthScope Pod Is Not Created

If the Deployment exists but has no Pod, check the ReplicaSet events for an admission error:

```bash
kubectl -n northscope describe rs -l app.kubernetes.io/name=northscope
```

The default Pod is non-root, drops all capabilities, uses a read-only root filesystem and the `RuntimeDefault` seccomp profile, and satisfies the Pod Security Standards `restricted` profile. It does not set a fixed UID, so OpenShift's `restricted-v2` SCC can assign one from the namespace range.

Kubernetes 1.20-1.24 clusters that enforce PodSecurityPolicy reject Pods unless a policy authorizes the NorthScope ServiceAccount. The chart does not create PodSecurityPolicies because the API was removed in Kubernetes 1.25. Bind an existing restricted policy to the ServiceAccount, for example:

```bash
kubectl -n northscope create role northscope-psp --verb=use --resource=podsecuritypolicies.policy --resource-name=<restricted-policy>
kubectl -n northscope create rolebinding northscope-psp --role=northscope-psp --serviceaccount=northscope:northscope
```

On large clusters, raise `resources.limits.memory` (default `256Mi`) or set `watchNamespace` if the Pod is `OOMKilled`. NorthScope caches Pods, Services, Endpoints, and EndpointSlices for the watched scope.

## No Traffic Routes Are Listed

Verify that traffic resources exist and that NorthScope can read them:

```bash
kubectl get ingress -A
kubectl auth can-i list ingresses.networking.k8s.io --as=system:serviceaccount:northscope:northscope -A
kubectl auth can-i list ingressclasses.networking.k8s.io --as=system:serviceaccount:northscope:northscope
kubectl get httproute,gateway -A
```

NorthScope defaults to **All namespaces**. If a UI namespace filter is selected, clear it and search again by host, route name, path, or Service.

The Helm `watchNamespace` value is a server-side boundary. Routes outside that namespace are not watched and cannot be restored with the UI filter. Inspect the installed value with:

```bash
helm get values northscope -n northscope
```

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

If several controllers are installed, check that every Ingress has the intended `ingressClassName`. An Ingress without an explicit class is attached only when exactly one IngressClass is marked as the default; NorthScope does not guess ownership from an arbitrary NodePort Service.

Older controllers often rely on the deprecated `kubernetes.io/ingress.class` annotation without creating IngressClass objects. When an annotation names a class that has no IngressClass object, NorthScope shows an `Inferred` controller for that class name and matches controller Services by name and labels. Create an IngressClass object when you want ownership to be explicit.
## Optional Gateway API Or F5 Objects Are Missing

Gateway API and F5 CIS resources are optional. NorthScope only watches those resources when their CRDs are installed, discoverable, and enabled in Helm values.

```bash
kubectl get crd | grep -E 'gateway.networking.k8s.io|cis.f5.com'
helm get values northscope -n northscope
```

For a Gateway API backend in another namespace, verify that the backend namespace contains a matching ReferenceGrant. NorthScope intentionally reports and disconnects cross-namespace references that are not granted.

## A Route Looks Healthy But Requests Fail

NorthScope does not perform active network requests. `Configured`, `Backends ready`, and `Error` are derived from Kubernetes object status, references, Service ports, Pod readiness, and endpoint data.

If the configured graph is complete but requests still fail, inspect the data plane:

```bash
kubectl describe ingress -n <namespace> <ingress>
kubectl describe gateway -n <namespace> <gateway>
kubectl logs -n <controller-namespace> deploy/<controller>
```

Also check external load balancer health, TLS, DNS, NetworkPolicy, firewall rules, and application logs.

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
- `northscope_snapshot_publishes_total`
- `northscope_snapshot_unchanged_total`
- `northscope_snapshot_build_duration_seconds`
- `northscope_websocket_clients`

If `northscope_snapshot_build_errors_total` increases, check NorthScope logs for `build topology snapshot failed`.

`northscope_snapshot_unchanged_total` increases when informer resyncs or resource events produce the same traffic-root-scoped topology. Those builds are intentionally not republished to WebSocket clients.
