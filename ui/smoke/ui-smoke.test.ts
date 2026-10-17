import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  filterNodesByRoute,
  groupRoutesByHost,
  layoutTrafficPath,
  routeNotes,
  type RouteItem,
} from '../src/trafficGraph.ts';
import {
  namespaceInputValue,
  resolveNamespaceEnterSelection,
  routeMatchesSearch,
  viewStateFromSearch,
  viewStateSearch,
} from '../src/uiState.ts';
import {
  acceptSnapshot,
  type TopologyEdge,
  type TopologyNode,
  type TopologySnapshot,
} from '../src/hooks/useTopologyStream.ts';
import { reconcileTopologyEdges, reconcileTopologyNodes } from '../src/flowState.ts';
import {
  degradedResources,
  formatSnapshotAge,
  healthFromStatus,
  streamStatusLabel,
  type WatcherStatus,
} from '../src/watchHealth.ts';
import { loginRedirectPath, sessionCheckFromStatus } from '../src/auth.ts';

function route(overrides: Partial<RouteItem>): RouteItem {
  const item = {
    id: 'route:default',
    topologyId: 'route:default',
    ingressId: 'ingress:store',
    rootKind: 'Ingress',
    serviceId: 'service:frontend',
    namespace: 'northscope',
    name: 'store',
    host: 'shop.demo.localhost',
    path: '/',
    hostLaneId: 'lane:store:shop',
    ingress: 'store',
    backend: 'frontend:http',
    status: 'Backends ready',
    severity: 'ok',
    diagnosis: '',
    nextStep: '',
    kubectl: '',
    notes: [],
    ...overrides,
  };
  return {
    ...item,
    topologyId: overrides.topologyId ?? overrides.id ?? item.topologyId,
  };
}

function node(id: string, kind: string, lane: string): TopologyNode {
  return {
    id,
    type: 'northscopeNode',
    position: { x: 0, y: 0 },
    data: {
      label: id,
      kind,
      namespace: 'northscope',
      name: id,
      status: 'Active',
      properties: {
        visualLane: lane,
      },
    },
  };
}

test('namespace picker supports All namespaces as the default and Enter target', () => {
  assert.equal(namespaceInputValue('', false, ''), 'All namespaces');
  assert.equal(namespaceInputValue('northscope', false, ''), 'northscope');
  assert.equal(namespaceInputValue('northscope', true, 'north'), 'north');
  assert.equal(resolveNamespaceEnterSelection('', [{ name: 'northscope', ingressCount: 3 }]), '');
  assert.equal(resolveNamespaceEnterSelection('all namespaces', [{ name: 'northscope', ingressCount: 3 }]), '');
  assert.equal(resolveNamespaceEnterSelection('north', [{ name: 'northscope', ingressCount: 3 }]), 'northscope');
});

test('route search matches namespace, ingress, host, path, backend, and status', () => {
  const item = route({
    namespace: 'payments',
    ingress: 'checkout',
    host: 'shop.demo.localhost',
    path: '/api',
    backend: 'api:http',
    status: 'Warning',
  });

  assert.equal(routeMatchesSearch(item, ''), true);
  assert.equal(routeMatchesSearch(item, 'shop.demo'), true);
  assert.equal(routeMatchesSearch(item, 'checkout'), true);
  assert.equal(routeMatchesSearch(item, '/api'), true);
  assert.equal(routeMatchesSearch(item, 'api:http'), true);
  assert.equal(routeMatchesSearch(item, 'warning'), true);
  assert.equal(routeMatchesSearch(item, 'missing-service'), false);
});

test('route search also matches conflict and canary notes', () => {
  const item = route({ notes: routeNotes({ canaries: 'shop/shop-canary -> api-canary:80 (weight 20%)' }) });

  assert.equal(routeMatchesSearch(item, 'canary'), true);
  assert.equal(routeMatchesSearch(item, 'conflict'), false);
});

test('route notes describe overlapping Ingresses and canaries', () => {
  assert.deepEqual(routeNotes({}), []);
  assert.deepEqual(routeNotes({ conflictsWith: 'shop/shop-v2' }), ['Also defined by shop/shop-v2']);
  assert.deepEqual(routeNotes({ canary: 'true', canaryOf: 'shop/shop', canaryRule: 'weight 20%' }), ['Canary of shop/shop (weight 20%)']);
  assert.deepEqual(routeNotes({ canary: 'true', canaryRule: 'header X-Canary: always' }), [
    'Canary without a primary Ingress (header X-Canary: always)',
  ]);
  assert.deepEqual(routeNotes({ canary: 'true', canaryOf: 'shop/shop', canaryRule: 'weight 30%', canaryServiceSharedWith: 'shop/other' }), [
    'Canary of shop/shop (weight 30%)',
    'Canary Service also used by shop/other',
  ]);
});

test('shared links restore namespace, search, route, and view mode', () => {
  assert.deepEqual(viewStateFromSearch(''), { namespace: '', search: '', route: '', mode: 'simple' });
  const state = {
    namespace: 'store',
    search: 'shop',
    route: 'ingress-host:store:shop:shop-demo-localhost',
    mode: 'expanded' as const,
  };
  const search = viewStateSearch(state, '');

  assert.deepEqual(viewStateFromSearch(search), state);
  assert.equal(viewStateSearch({ namespace: '', search: ' ', route: '', mode: 'simple' }, ''), '');
  assert.equal(viewStateSearch({ namespace: '', search: '', route: '', mode: 'simple' }, '?namespace=old&debug=1'), '?debug=1');
  assert.equal(viewStateFromSearch('?view=unknown').mode, 'simple');
});

test('host route selection keeps all paths for the selected host together', () => {
  const routes = [
    route({ id: 'route:admin:/', host: 'admin.demo.localhost', path: '/', hostLaneId: 'lane:store:admin' }),
    route({ id: 'route:shop:/', host: 'shop.demo.localhost', path: '/', hostLaneId: 'lane:store:shop' }),
    route({ id: 'route:shop:/api', host: 'shop.demo.localhost', path: '/api', hostLaneId: 'lane:store:shop' }),
  ];

  const groups = groupRoutesByHost(routes);
  const shop = groups.find((group) => group.host === 'shop.demo.localhost');

  assert.ok(shop);
  assert.deepEqual(
    shop.routes.map((item) => item.path),
    ['/', '/api'],
  );

  const selectedNodes = filterNodesByRoute(
    [
      node('ingress-admin', 'Ingress', 'lane:store:admin'),
      node('ingress-shop', 'Ingress', 'lane:store:shop'),
      node('service-shop', 'Service', 'route:shop:/api'),
    ],
    shop.routes.map((item) => item.topologyId),
    [shop.id],
  );

  assert.deepEqual(
    selectedNodes.map((item) => item.id),
    ['ingress-shop', 'service-shop'],
  );
});

test('simple and expanded topology modes share layout primitives and set view mode', () => {
  const nodes = [
    node('f5', 'ExternalEdge', 'lane:store:shop'),
    node('controller', 'Controller', 'lane:store:shop'),
    node('ingress', 'Ingress', 'lane:store:shop'),
    node('service', 'Service', 'route:shop:/'),
    node('pods', 'PodGroup', 'route:shop:/'),
  ];

  const simple = layoutTrafficPath(nodes, 'simple');
  const expanded = layoutTrafficPath(nodes, 'expanded');

  assert.equal(simple.length, nodes.length);
  assert.equal(expanded.length, nodes.length);
  assert.equal(simple[0].data.properties?.viewMode, 'simple');
  assert.equal(expanded[0].data.properties?.viewMode, 'expanded');
  assert.ok((simple.find((item) => item.id === 'service')?.position.x ?? 0) > (simple.find((item) => item.id === 'ingress')?.position.x ?? 0));
  assert.ok((expanded.find((item) => item.id === 'service')?.position.x ?? 0) > (expanded.find((item) => item.id === 'ingress')?.position.x ?? 0));
});

test('live snapshot reconciliation preserves measured nodes and unchanged edge references', () => {
  const currentNode = {
    ...node('pod', 'Pod', 'route:shop:/'),
    position: { x: 1200, y: 240 },
    measured: { width: 260, height: 144 },
  };
  const incomingNode = node('pod', 'Pod', 'route:shop:/');
  const currentEdge: TopologyEdge = {
    id: 'pod->node:runs_on',
    source: 'pod',
    target: 'node',
    type: 'northscopeStep',
    label: 'runs on',
    data: { kind: 'runs_on' },
  };
  const incomingEdge: TopologyEdge = {
    ...currentEdge,
    data: { kind: 'runs_on' },
  };

  const stableNodes = reconcileTopologyNodes([currentNode], [incomingNode], false);
  const stableEdges = reconcileTopologyEdges([currentEdge], [incomingEdge], false);

  assert.equal(stableNodes[0], currentNode);
  assert.equal(stableEdges[0], currentEdge);

  const changedNode = {
    ...incomingNode,
    data: {
      ...incomingNode.data,
      status: 'NotReady',
    },
  };
  const updatedNodes = reconcileTopologyNodes([currentNode], [changedNode], false);

  assert.notEqual(updatedNodes[0], currentNode);
  assert.deepEqual(updatedNodes[0].measured, currentNode.measured);
  assert.deepEqual(updatedNodes[0].position, currentNode.position);
});

function streamSnapshot(instance: string, version: number): TopologySnapshot {
  return {
    instance,
    version,
    generatedAt: new Date(version * 1000).toISOString(),
    nodes: [],
    edges: [],
  };
}

test('topology stream never replaces a newer snapshot with an older version from the same instance', () => {
  const current = streamSnapshot('pod-a', 4);

  assert.equal(acceptSnapshot(current, streamSnapshot('pod-a', 3), false), current);
  assert.equal(acceptSnapshot(current, streamSnapshot('pod-a', 4), false), current);
  assert.equal(acceptSnapshot(current, streamSnapshot('pod-a', 5), false).version, 5);
});

test('topology stream accepts the first snapshot of every connection', () => {
  const incoming = streamSnapshot('pod-a', 2);

  assert.equal(acceptSnapshot(null, incoming, false), incoming);
  assert.equal(acceptSnapshot(null, incoming, true), incoming);
  assert.equal(acceptSnapshot(streamSnapshot('pod-a', 9), incoming, true), incoming);
});

test('topology stream accepts lower versions from a restarted or different instance', () => {
  const current = streamSnapshot('pod-a', 42);
  const restarted = streamSnapshot('pod-b', 1);

  assert.equal(acceptSnapshot(current, restarted, true), restarted);
  assert.equal(acceptSnapshot(current, restarted, false), restarted);

  const afterSwitch = acceptSnapshot(current, restarted, true);
  assert.equal(acceptSnapshot(afterSwitch, streamSnapshot('pod-b', 2), false).version, 2);
  assert.equal(acceptSnapshot(afterSwitch, streamSnapshot('pod-b', 1), false), afterSwitch);
});

test('stream status says Degraded instead of Live config when watch health reports errors', () => {
  assert.equal(streamStatusLabel('connected', true, false), 'Live config');
  assert.equal(streamStatusLabel('connected', true, true), 'Degraded');
  assert.equal(streamStatusLabel('connected', false, true), 'Syncing');
  assert.equal(streamStatusLabel('connecting', true, true), 'Connecting');
  assert.equal(streamStatusLabel('disconnected', true, true), 'Reconnecting');
});

test('degraded resources come from snapshot health or the startup status endpoint', () => {
  const since = '2026-10-16T12:00:00Z';
  assert.deepEqual(degradedResources(undefined), []);
  assert.deepEqual(degradedResources({ status: 'ok' }), []);
  assert.deepEqual(
    degradedResources({ status: 'degraded', degraded: [{ resource: 'pods', synced: true, since, error: 'forbidden' }] }).map(
      (item) => item.resource,
    ),
    ['pods'],
  );

  const status: WatcherStatus = {
    status: 'degraded',
    ready: false,
    snapshotVersion: 0,
    unsynced: ['pods'],
    resources: [
      { resource: 'pods', synced: false, degraded: true, degradedSince: since, lastError: 'pods is forbidden', errorCount: 3 },
      { resource: 'services', synced: true, degraded: false, errorCount: 0 },
    ],
  };
  assert.deepEqual(degradedResources(healthFromStatus(status)), [
    { resource: 'pods', synced: false, since, error: 'pods is forbidden' },
  ]);
  assert.equal(healthFromStatus(null), null);
  assert.equal(healthFromStatus({ ...status, resources: undefined } as unknown as WatcherStatus), null);
});

test('snapshot age is relative to generatedAt and tolerates clock skew', () => {
  const generatedAt = '2026-10-16T12:00:00Z';
  const at = (seconds: number) => Date.parse(generatedAt) + seconds * 1000;

  assert.equal(formatSnapshotAge(generatedAt, at(-30)), 'just now');
  assert.equal(formatSnapshotAge(generatedAt, at(5)), 'just now');
  assert.equal(formatSnapshotAge(generatedAt, at(42)), '42s ago');
  assert.equal(formatSnapshotAge(generatedAt, at(5 * 60 + 59)), '5m ago');
  assert.equal(formatSnapshotAge(generatedAt, at(3 * 3600)), '3h ago');
  assert.equal(formatSnapshotAge(generatedAt, at(2 * 86400)), '2d ago');
  assert.equal(formatSnapshotAge('0001-01-01T00:00:00Z', at(0)), '');
  assert.equal(formatSnapshotAge('not a date', at(0)), '');
});

test('401 responses send the user to the login page and keep the current view as next', () => {
  assert.equal(sessionCheckFromStatus(401), 'unauthenticated');
  assert.equal(sessionCheckFromStatus(200), 'authenticated');
  assert.equal(sessionCheckFromStatus(502), 'unknown');
  assert.equal(loginRedirectPath('/', ''), '/login');
  assert.equal(loginRedirectPath('/', '?namespace=demo'), '/login?next=%2F%3Fnamespace%3Ddemo');
  assert.equal(loginRedirectPath('/login', '?next=%2F'), '/login');
});
