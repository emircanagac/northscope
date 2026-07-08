import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  filterNodesByRoute,
  groupRoutesByHost,
  layoutTrafficPath,
  type RouteItem,
} from '../src/trafficGraph.ts';
import {
  namespaceInputValue,
  resolveNamespaceEnterSelection,
  routeMatchesSearch,
} from '../src/uiState.ts';
import type { TopologyNode } from '../src/hooks/useTopologyStream.ts';

function route(overrides: Partial<RouteItem>): RouteItem {
  return {
    id: 'route:default',
    ingressId: 'ingress:store',
    serviceId: 'service:frontend',
    namespace: 'northscope',
    name: 'store',
    host: 'shop.demo.localhost',
    path: '/',
    hostLaneId: 'lane:store:shop',
    ingress: 'store',
    backend: 'frontend:http',
    status: 'Healthy',
    severity: 'ok',
    ...overrides,
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
    shop.routes.map((item) => item.id),
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
