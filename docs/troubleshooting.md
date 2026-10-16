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
NorthScope authentication enabled for user "admin" (cookie secure: auto, metrics protected: false)
NorthScope compatibility: using EndpointSlice API discovery.k8s.io/v1
HTTP server listening on :8080
NorthScope Kubernetes caches synced
NorthScope ready: snapshot v1, ... nodes, ... edges
```

If `/readyz` returns `503`, the watcher has not generated the first topology snapshot yet. NorthScope waits until Ingresses, IngressClasses, Services, Endpoints, EndpointSlices, Pods, and Nodes have each been listed once. While it waits, it logs the resources that are still unsynced every 30 seconds, with their latest error:

```text
NorthScope still waiting for Kubernetes caches after 30s; unsynced: pods (last error: failed to list *v1.Pod: pods is forbidden: ...)
```

The same information is available over HTTP. `/api/status` requires a session when the built-in sign-in is enabled, so sign in with curl first:

```bash
curl http://localhost:18080/readyz?verbose
curl -c /tmp/northscope.cookies -d username=admin \
  --data-urlencode "password=$(kubectl -n northscope get secret northscope-auth -o jsonpath='{.data.password}' | base64 -d)" \
  http://localhost:18080/login
curl -b /tmp/northscope.cookies http://localhost:18080/api/status
```

Grant the missing read-only permission or restore API server connectivity. NorthScope retries with backoff and does not need a restart.

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

## Signing In

The Helm chart enables the built-in sign-in. The username is `admin` unless `auth.username` or `auth.existingSecret` says otherwise. Read the generated password from the release's Secret:

```bash
kubectl -n northscope get secret northscope-auth -o jsonpath='{.data.password}' | base64 -d; echo
```

If the Pod does not start after enabling authentication, check its events and logs:

- `MountVolume.SetUp failed for volume "auth"` in `kubectl describe pod`, for example `references non-existent secret key: session-key`: the `auth.existingSecret` Secret must contain `username`, `password`, and `session-key`.
- `configure authentication: ...` in the logs: one of those files is empty, or the session key is shorter than 32 characters.

If the sign-in form returns to the login page or keeps asking again after a successful sign-in:

- The browser rejected the session cookie. With `auth.cookieSecure=true` the cookie is only sent over HTTPS, so a plain-HTTP `kubectl port-forward` cannot keep a session. Use `auto`, or reach NorthScope over HTTPS.
- A proxy in front of NorthScope sends `X-Forwarded-Proto: https` while the browser uses plain HTTP. Fix the proxy or set `auth.cookieSecure=false` for that environment.
- The password or the session key changed, or the Secret was recreated by a `helm template`-based deployment. Sessions signed with the previous values are no longer valid; sign in again and use `auth.existingSecret` for GitOps installs.

`Too many failed sign-in attempts` means the client address used up its 5 failed attempts. One more attempt becomes available every 30 seconds. Behind an ingress controller all clients share the controller's address and therefore this limit.

After a password change through Helm, the Deployment rolls automatically. After rotating an `auth.existingSecret` Secret, restart NorthScope with `kubectl -n northscope rollout restart deploy/northscope`.

If the log shows `WARNING: authentication is disabled`, NorthScope was started without `-auth-dir` (for example with `auth.enabled=false`) and anyone who can reach it can view the topology.

## The UI Shows Degraded Or An Old Snapshot

The header shows when the current snapshot was generated, for example `Snapshot 3m ago`; hover over it for the exact time and snapshot version. NorthScope sends a new snapshot only when topology or watch health changes, so an old snapshot on a healthy instance means nothing changed.

When a list or watch call fails, for example because RBAC was revoked, the ServiceAccount token expired, or the API server is unreachable, NorthScope keeps serving its last cached data and marks the resource as degraded:

- The status pill shows `Degraded` instead of `Live config`.
- A banner names the degraded resources and their latest error. `(not loaded)` means the resource was never listed, so its objects are missing rather than stale.
- The log records `NorthScope watch for <resource> degraded` once, and `NorthScope watch for <resource> recovered` when a watch succeeds again.

A resource stays degraded until a watch for it starts or delivers an event again. A successful list alone does not clear it, so a Role that grants `list` but not `watch` stays visible.

After the first snapshot, degraded resources do not fail `/readyz`, and `/healthz` always succeeds while the process runs. Restarting or unrouting NorthScope does not fix RBAC or API server problems; it would only hide the last known topology. Inspect the details instead:

```bash
curl -b /tmp/northscope.cookies http://localhost:18080/api/status
curl http://localhost:18080/readyz?verbose
kubectl auth can-i watch pods --as=system:serviceaccount:northscope:northscope -A
```

`/api/status` reports `synced`, `degraded`, `degradedSince`, `lastSuccessAt`, `lastError`, `lastErrorAt`, and `errorCount` for every watched resource. Resources use `kubectl` names such as `pods`, `ingresses.networking.k8s.io`, and `httproutes.gateway.networking.k8s.io`. Optional Gateway API and F5 resources are reported too, but never block startup.

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

If several controllers are installed, check that every Ingress has the intended `ingressClassName`. NorthScope attaches an Ingress without an explicit class in this order:

1. The IngressClass marked as the default, when exactly one is marked.
2. The only IngressClass, when the cluster has exactly one.
3. The controller that wrote the Ingress status: when the status address matches the load balancer address or external IP of exactly one exposed Service (or of exactly one controller-like Service), NorthScope links the Ingress to that Service's IngressClass controller, or shows an `Inferred` controller named after the Service. This covers controllers without an IngressClass object, such as Traefik on older k3s releases.

NorthScope does not guess ownership from an arbitrary NodePort Service.

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
- `northscope_snapshot_generated_timestamp_seconds`
- `northscope_websocket_clients`
- `northscope_informer_synced{resource}`
- `northscope_informer_degraded{resource}`
- `northscope_watch_errors_total{resource}`
- `northscope_last_watch_success_timestamp_seconds{resource}`

If `northscope_snapshot_build_errors_total` increases, check NorthScope logs for `build topology snapshot failed`.

`northscope_snapshot_unchanged_total` increases when informer resyncs or resource events produce the same traffic-root-scoped topology and watch health. Those builds are intentionally not republished to WebSocket clients.

To catch a stale view, alert when `max(northscope_informer_degraded) > 0` for several minutes. `northscope_watch_errors_total` counts failed list and watch attempts and keeps increasing while client-go retries a degraded resource. `northscope_last_watch_success_timestamp_seconds` advances on every successful list, watch start, and watch event, including bookmarks. A quiet resource can go about 10 minutes without one, so prefer the degraded gauge for alerting.
