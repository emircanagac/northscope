import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { TopologyEdge, TopologyNode, TopologySnapshot } from '../src/hooks/useTopologyStream.ts';
import { applyTopologyMessage, type TopologyDelta } from '../src/topologyDelta.ts';
import { buildNamespaceTrafficGraph } from '../src/topologyGraph.ts';
import { layoutTrafficPath } from '../src/trafficGraph.ts';
import { filterNamespaceOptions, inventoryParts, namespaceOptionsFromNodes, plural } from '../src/inventory.ts';
import { emptyStateText } from '../src/emptyState.ts';

function topologyNode(id: string, kind: TopologyNode['data']['kind'], extra: Partial<TopologyNode['data']> = {}): TopologyNode {
  return { id, type: 'northscopeNode', position: { x: 0, y: 0 }, data: { label: id, kind, name: id, ...extra } };
}

function edge(source: string, target: string, kind: string): TopologyEdge {
  return { id: `${source}->${target}:${kind}`, source, target, data: { kind } };
}

function snapshot(version: number, nodes: TopologyNode[], instance = 'a'): TopologySnapshot {
  return { instance, version, generatedAt: new Date(version * 1000).toISOString(), nodes, edges: [] };
}

function delta(overrides: Partial<TopologyDelta>): TopologyDelta {
  return { type: 'delta', instance: 'a', version: 2, baseVersion: 1, generatedAt: new Date(2000).toISOString(), ...overrides };
}

test('deltas replace, add, and remove nodes and keep them sorted by ID', () => {
  const base = snapshot(1, [topologyNode('pod:a', 'Pod'), topologyNode('pod:c', 'Pod'), topologyNode('svc:x', 'Service')]);
  const applied = applyTopologyMessage(
    base,
    delta({
      upsertNodes: [topologyNode('pod:b', 'Pod'), topologyNode('pod:c', 'Pod', { status: 'CrashLoopBackOff' })],
      removeNodes: ['svc:x'],
    }),
    false,
  );

  assert.equal(applied.resync, false);
  assert.equal(applied.snapshot?.version, 2);
  assert.deepEqual(
    applied.snapshot?.nodes.map((item) => item.id),
    ['pod:a', 'pod:b', 'pod:c'],
  );
  assert.equal(applied.snapshot?.nodes[2].data.status, 'CrashLoopBackOff');
  assert.equal(applied.snapshot?.nodes[0], base.nodes[0], 'unchanged nodes keep their object identity');
});

test('a delta that does not fit the current snapshot asks for a resync', () => {
  const base = snapshot(1, [topologyNode('pod:a', 'Pod')]);
  assert.equal(applyTopologyMessage(null, delta({}), false).resync, true);
  assert.equal(applyTopologyMessage(base, delta({}), true).resync, true, 'a connection starts with a full snapshot');
  assert.equal(applyTopologyMessage(base, delta({ baseVersion: 0 }), false).resync, true);
  assert.equal(applyTopologyMessage(base, delta({ instance: 'b' }), false).resync, true);

  const full = snapshot(5, [], 'b');
  assert.deepEqual(applyTopologyMessage(base, full, false), { snapshot: full, resync: false });
});

test('layout sorts each column once by lane and name', () => {
  const nodes = Array.from({ length: 3000 }, (_, index) =>
    topologyNode(`pod:${String(index).padStart(4, '0')}`, 'Pod', { properties: { visualLane: `lane-${index % 3}` } }),
  );
  const started = performance.now();
  const laid = layoutTrafficPath(nodes, 'expanded');
  const elapsed = performance.now() - started;

  const rows = new Set(laid.map((item) => item.position.y));
  assert.equal(rows.size, nodes.length, 'every pod gets its own row');
  const first = laid.find((item) => item.id === 'pod:0000');
  const second = laid.find((item) => item.id === 'pod:0003');
  assert.ok(first && second && first.position.y < second.position.y);
  assert.equal(first?.initialWidth, 260);
  assert.ok(elapsed < 1000, `layout of 3000 nodes took ${Math.round(elapsed)}ms`);
});

function ingressTopology(): { nodes: TopologyNode[]; edges: TopologyEdge[] } {
  const nodes = [
    topologyNode('controller:nginx', 'Controller'),
    topologyNode('ingress:demo:web', 'Ingress', { namespace: 'demo', name: 'web' }),
    topologyNode('route:demo:web:shop', 'Route', {
      namespace: 'demo',
      properties: { host: 'shop.example.test', path: '/', backend: 'shop', severity: 'ok' },
    }),
    topologyNode('route:demo:web:api', 'Route', {
      namespace: 'demo',
      properties: { host: 'api.example.test', path: '/', backend: 'api', severity: 'ok' },
    }),
    topologyNode('service:demo:shop', 'Service', { namespace: 'demo', name: 'shop' }),
    topologyNode('service:demo:api', 'Service', { namespace: 'demo', name: 'api' }),
  ];
  const edges = [
    edge('controller:nginx', 'ingress:demo:web', 'controls'),
    edge('ingress:demo:web', 'route:demo:web:shop', 'defines'),
    edge('ingress:demo:web', 'route:demo:web:api', 'defines'),
    edge('route:demo:web:shop', 'service:demo:shop', 'routes'),
    edge('route:demo:web:api', 'service:demo:api', 'routes'),
  ];
  return { nodes, edges };
}

test('the graph builder lists every route but draws only the requested lanes', () => {
  const { nodes, edges } = ingressTopology();
  const listing = buildNamespaceTrafficGraph('', nodes, edges, 'simple', new Set());
  assert.equal(listing.routes.length, 2);
  assert.equal(listing.nodes.length, 0);

  const shopLane = listing.routes.find((route) => route.host === 'shop.example.test')?.hostLaneId ?? '';
  const drawn = buildNamespaceTrafficGraph('', nodes, edges, 'simple', new Set([shopLane]));
  assert.equal(drawn.routes.length, 2);
  assert.ok(drawn.nodes.length > 0);
  assert.ok(drawn.nodes.some((item) => item.id.startsWith('service:demo:shop')));
  assert.ok(!drawn.nodes.some((item) => item.id.startsWith('service:demo:api')), 'other hosts are not drawn');

  const everything = buildNamespaceTrafficGraph('', nodes, edges, 'simple');
  assert.ok(everything.nodes.length > drawn.nodes.length);
});

test('inventory text and namespace options', () => {
  assert.equal(plural(1, 'pod'), '1 pod');
  assert.equal(plural(2, 'ingress class', 'ingress classes'), '2 ingress classes');
  assert.deepEqual(inventoryParts({ ingressClasses: 1, ingresses: 2, services: 3, pods: 1, nodes: 2 }), [
    '1 ingress class',
    '2 ingresses',
    '3 services',
    '1 pod',
    '2 nodes',
  ]);
  assert.ok(inventoryParts({ gateways: 2, f5Resources: 1 }).includes('2 gateways'));

  const options = namespaceOptionsFromNodes([
    topologyNode('pod:zeta', 'Pod', { namespace: 'zeta' }),
    topologyNode('ingress:beta', 'Ingress', { namespace: 'beta' }),
    topologyNode('pod:alpha', 'Pod', { namespace: 'alpha' }),
    topologyNode('lb:f5', 'LoadBalancer', { namespace: 'zeta', properties: { provider: 'F5' } }),
  ]);
  assert.deepEqual(
    options.map((option) => `${option.name}:${option.ingressCount}`),
    ['beta:1', 'zeta:1', 'alpha:0'],
  );
  assert.deepEqual(
    filterNamespaceOptions(options, ' ZE ').map((option) => option.name),
    ['zeta'],
  );
});

test('empty canvas text explains what is missing', () => {
  const base = {
    hasSnapshot: true,
    connected: true,
    hasTopology: true,
    hasRoutes: true,
    hasFilteredRoutes: true,
    routeSelected: false,
    namespace: '',
    unsynced: [],
  };
  assert.equal(emptyStateText({ ...base, hasSnapshot: false, unsynced: ['pods'] }).description.startsWith('Waiting for Kubernetes caches: pods.'), true);
  assert.equal(emptyStateText(base).title, 'Select a host route to draw topology');
  assert.equal(emptyStateText({ ...base, hasRoutes: false, namespace: 'demo' }).title, 'No traffic routes found in this namespace');
  assert.equal(emptyStateText({ ...base, hasRoutes: false, hasTopology: false }).title, 'Snapshot received, but no supported topology objects were found');
});
