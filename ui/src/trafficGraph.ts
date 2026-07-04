import type { TopologyEdge, TopologyNode } from './hooks/useTopologyStream';
import { nodeDisplayName } from './topologyView';

export type TopologyMode = 'simple' | 'expanded';

export interface RouteItem {
  id: string;
  ingressId: string;
  serviceId: string;
  name: string;
  host: string;
  path: string;
  hostLaneId: string;
  ingress: string;
  backend: string;
  status: string;
  severity: string;
}

export interface RouteGroup {
  id: string;
  host: string;
  ingress: string;
  routes: RouteItem[];
}

function kindOf(node?: TopologyNode): string {
  return String(node?.data.kind ?? '').toLowerCase();
}

function nodeColumn(node: TopologyNode): string {
  const kind = kindOf(node);
  if (kind === 'f5' || kind === 'externaledge') return 'f5';
  if (kind === 'podgroup') return 'pod';
  return kind;
}

export function layoutTrafficPath(nodes: TopologyNode[], mode: TopologyMode): TopologyNode[] {
  const columnOrder =
    mode === 'simple'
      ? ['f5', 'nodeport', 'controller', 'ingress', 'service', 'pod']
      : ['f5', 'nodeport', 'controller', 'ingress', 'dns', 'route', 'service', 'pod', 'node'];
  const columnX: Record<string, number> =
    mode === 'simple'
      ? {
          f5: 0,
          nodeport: 240,
          controller: 500,
          ingress: 760,
          service: 1020,
          pod: 1280,
        }
      : {
          f5: 0,
          nodeport: 280,
          controller: 580,
          ingress: 880,
          dns: 1180,
          route: 1480,
          service: 1800,
          pod: 2120,
          node: 2440,
        };
  const rowGap = mode === 'simple' ? 190 : 230;
  const columns = new Map<string, TopologyNode[]>();

  for (const node of nodes) {
    const kind = nodeColumn(node);
    const column = columnOrder.includes(kind) ? kind : 'route';
    columns.set(column, [...(columns.get(column) ?? []), node]);
  }

  return nodes.map((node) => {
    const kind = nodeColumn(node);
    const column = columnOrder.includes(kind) ? kind : 'route';
    const columnNodes = [...(columns.get(column) ?? [])].sort((left, right) => {
      const leftLane = String(left.data.properties?.visualLane ?? '');
      const rightLane = String(right.data.properties?.visualLane ?? '');
      const laneOrder = leftLane.localeCompare(rightLane);
      if (laneOrder !== 0) return laneOrder;
      return nodeDisplayName(left).localeCompare(nodeDisplayName(right));
    });
    const rowIndex = Math.max(0, columnNodes.findIndex((item) => item.id === node.id));

    return {
      ...node,
      position: {
        x: columnX[column],
        y: rowIndex * rowGap,
      },
      data: {
        ...node.data,
        properties: {
          ...(node.data.properties ?? {}),
          viewMode: mode,
        },
      },
    };
  });
}

export function severityRank(severity: string): number {
  const normalized = severity.toLowerCase();
  if (normalized === 'error') return 0;
  if (normalized === 'warning') return 1;
  if (normalized === 'ok') return 2;
  return 3;
}

export function severityClass(severity: string): string {
  const normalized = severity.toLowerCase();
  if (normalized === 'error') {
    return 'border-red-200 bg-red-50 text-red-800';
  }
  if (normalized === 'warning') {
    return 'border-amber-200 bg-amber-50 text-amber-800';
  }
  if (normalized === 'ok') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-800';
  }
  return 'border-slate-200 bg-slate-50 text-slate-700';
}

export function routeGroupSeverity(group: RouteGroup): string {
  return group.routes.reduce((current, route) => {
    return severityRank(route.severity) < severityRank(current) ? route.severity : current;
  }, group.routes[0]?.severity ?? 'unknown');
}

export function routeGroupStatus(group: RouteGroup): string {
  const statuses = Array.from(new Set(group.routes.map((route) => route.status).filter(Boolean)));
  if (statuses.length === 1) {
    return statuses[0];
  }
  if (group.routes.some((route) => route.severity === 'error')) {
    return 'Needs attention';
  }
  if (group.routes.some((route) => route.severity === 'warning')) {
    return 'Warning';
  }
  return `${group.routes.length} paths`;
}

export function routeGroupButtonClass(group: RouteGroup, selected: boolean): string {
  return [
    'w-full rounded-md border px-3 py-2 text-left transition',
    selected ? 'border-slate-900 bg-slate-900 text-white shadow-sm' : severityClass(routeGroupSeverity(group)),
  ].join(' ');
}

export function groupRoutes(routes: RouteItem[]): RouteGroup[] {
  const groups = new Map<string, RouteItem[]>();
  for (const route of routes) {
    groups.set(route.hostLaneId, [...(groups.get(route.hostLaneId) ?? []), route]);
  }
  return Array.from(groups.entries())
    .sort(([, leftGroup], [, rightGroup]) => {
      const host = leftGroup[0].host.localeCompare(rightGroup[0].host);
      if (host !== 0) return host;
      return leftGroup[0].ingress.localeCompare(rightGroup[0].ingress);
    })
    .map(([id, group]) => ({
      id,
      host: group[0].host,
      ingress: group[0].ingress,
      routes: [...group].sort((left, right) => {
        const severity = severityRank(left.severity) - severityRank(right.severity);
        if (severity !== 0) return severity;
        return left.path.localeCompare(right.path);
      }),
    }));
}

export function safeVisualId(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9-]+/g, '-');
}

export function ingressHostLaneId(namespace: string, ingress: TopologyNode, host: string): string {
  const ingressName = String(ingress.data.name || ingress.data.label || ingress.id);
  return `ingress-host:${namespace}:${safeVisualId(ingressName)}:${safeVisualId(host)}`;
}

function laneIdForNode(node: TopologyNode): string {
  return String(node.data.properties?.visualLane ?? '');
}

function laneIdFromNodeId(id: string): string {
  const marker = ':lane:';
  const laneIndex = id.lastIndexOf(marker);
  if (laneIndex === -1) {
    return '';
  }
  return id.slice(laneIndex + marker.length);
}

function laneIdsForEdge(edge: TopologyEdge): string[] {
  return [laneIdFromNodeId(edge.source), laneIdFromNodeId(edge.target)].filter(Boolean);
}

function laneMatchesSelection(laneId: string, selectedRouteIds: string[], selectedHostLaneId: string): boolean {
  return laneId === selectedHostLaneId || selectedRouteIds.includes(laneId);
}

export function focusNodesByRoute(nodes: TopologyNode[], selectedRouteIds: string[], selectedHostLaneId: string): TopologyNode[] {
  if (selectedRouteIds.length === 0 && !selectedHostLaneId) {
    return nodes;
  }

  return nodes.map((node) => {
    const laneId = laneIdForNode(node);
    const active = laneMatchesSelection(laneId, selectedRouteIds, selectedHostLaneId);
    return {
      ...node,
      style: {
        ...(node.style ?? {}),
        opacity: active ? 1 : 0.22,
      },
      zIndex: active ? 20 : 0,
    };
  });
}

export function filterNodesByRoute(nodes: TopologyNode[], selectedRouteIds: string[], selectedHostLaneId: string): TopologyNode[] {
  if (selectedRouteIds.length === 0 && !selectedHostLaneId) {
    return nodes;
  }

  return nodes.filter((node) => {
    const laneId = laneIdForNode(node);
    return laneMatchesSelection(laneId, selectedRouteIds, selectedHostLaneId);
  });
}

export function filterEdgesForNodes(edges: TopologyEdge[], nodes: TopologyNode[]): TopologyEdge[] {
  const nodeIds = new Set(nodes.map((node) => node.id));
  return edges.filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target));
}

export function focusEdgesByRoute(edges: TopologyEdge[], selectedRouteIds: string[], selectedHostLaneId: string): TopologyEdge[] {
  if (selectedRouteIds.length === 0 && !selectedHostLaneId) {
    return edges;
  }

  return edges.map((edge) => {
    const laneIds = laneIdsForEdge(edge);
    const active = laneIds.some((laneId) => laneMatchesSelection(laneId, selectedRouteIds, selectedHostLaneId));
    return {
      ...edge,
      animated: Boolean(edge.animated && active),
      style: {
        ...(edge.style ?? {}),
        strokeOpacity: active ? 0.95 : 0.16,
        strokeWidth: active ? 2.6 : 1.2,
      },
      zIndex: active ? 20 : 0,
    };
  });
}
