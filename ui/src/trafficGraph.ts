import type { TopologyEdge, TopologyNode } from './hooks/useTopologyStream';

export type TopologyMode = 'simple' | 'expanded';

// Size of a topology node card (KubeNode).
export const NODE_CARD_WIDTH = 260;
export const NODE_CARD_HEIGHT = 132;

export interface RouteItem {
  id: string;
  topologyId: string;
  ingressId: string;
  rootKind: string;
  serviceId: string;
  namespace: string;
  name: string;
  host: string;
  path: string;
  hostLaneId: string;
  ingress: string;
  backend: string;
  status: string;
  severity: string;
  diagnosis: string;
  nextStep: string;
  kubectl: string;
  notes: string[];
}

export interface RouteGroup {
  id: string;
  namespace: string;
  ingress: string;
  rootKind: string;
  routes: RouteItem[];
}

export interface RouteHostGroup {
  id: string;
  ingressId: string;
  ingress: string;
  host: string;
  routes: RouteItem[];
}

function kindOf(node?: TopologyNode): string {
  return String(node?.data.kind ?? '').toLowerCase();
}

function nodeColumn(node: TopologyNode): string {
  const kind = kindOf(node);
  if (kind === 'f5' || kind === 'externaledge' || kind === 'loadbalancer') return 'f5';
  if (kind === 'podgroup') return 'pod';
  if (kind === 'endpoint') return 'endpoint';
  if (kind === 'endpointslice') return 'endpoint';
  return kind;
}

function nodeDisplayNameForLayout(node: TopologyNode): string {
  if (node.data.namespace) {
    return `${node.data.namespace}/${node.data.name}`;
  }

  return node.data.name;
}

export function layoutTrafficPath(nodes: TopologyNode[], mode: TopologyMode): TopologyNode[] {
  const columnOrder =
    mode === 'simple'
      ? ['f5', 'nodeport', 'controller', 'ingress', 'gateway', 'route', 'service', 'endpoint', 'pod']
      : ['f5', 'nodeport', 'controller', 'ingress', 'gateway', 'dns', 'route', 'service', 'endpoint', 'pod', 'node'];
  const columnGap = 340;
  const rowGap = mode === 'simple' ? 190 : 240;
  const columns = new Map<string, TopologyNode[]>();
  const columnOf = (node: TopologyNode) => {
    const kind = nodeColumn(node);
    return columnOrder.includes(kind) ? kind : 'route';
  };

  for (const node of nodes) {
    const column = columnOf(node);
    const items = columns.get(column);
    if (items) {
      items.push(node);
    } else {
      columns.set(column, [node]);
    }
  }

  const activeColumns = columnOrder.filter((column) => columns.has(column));
  const columnX = activeColumns.reduce<Record<string, number>>((positions, column, index) => {
    positions[column] = index * columnGap;
    return positions;
  }, {});

  // Each column is sorted once, so finding a node's row is a map lookup
  // instead of a sort per node.
  const rowById = new Map<string, number>();
  for (const columnNodes of columns.values()) {
    columnNodes
      .map((node) => ({
        node,
        lane: String(node.data.properties?.visualLane ?? ''),
        name: nodeDisplayNameForLayout(node),
      }))
      .sort((left, right) => left.lane.localeCompare(right.lane) || left.name.localeCompare(right.name))
      .forEach((item, index) => {
        if (!rowById.has(item.node.id)) {
          rowById.set(item.node.id, index);
        }
      });
  }

  return nodes.map((node) => {
    const column = columnOf(node);
    const rowIndex = rowById.get(node.id) ?? 0;

    return {
      ...node,
      // Cards have a fixed size. Giving it up front lets the canvas fit and
      // cull nodes before they are rendered and measured.
      initialWidth: NODE_CARD_WIDTH,
      initialHeight: NODE_CARD_HEIGHT,
      position: {
        x: columnX[column] ?? 0,
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

// routeNotes lists the other Ingresses that share a route's host and path, so
// overlaps stay visible even when another finding is the route's diagnosis.
export function routeNotes(properties: Record<string, string> = {}): string[] {
  const notes: string[] = [];
  if (properties.conflictsWith) {
    notes.push(`Also defined by ${properties.conflictsWith}`);
  }
  if (properties.canaries) {
    notes.push(`Canary: ${properties.canaries}`);
  }
  if (properties.canary === 'true') {
    const primary = properties.canaryOf ? `canary of ${properties.canaryOf}` : 'canary without a primary Ingress';
    notes.push(properties.canaryRule ? `${primary} (${properties.canaryRule})` : primary);
  }
  if (properties.canaryServiceSharedWith) {
    notes.push(`Canary Service also used by ${properties.canaryServiceSharedWith}`);
  }
  if (properties.outsideWatchScope) {
    notes.push(`Outside watch scope: ${properties.outsideWatchScope}`);
  }
  return notes.map((note) => note.charAt(0).toUpperCase() + note.slice(1));
}

export function severityRank(severity: string): number {
  const normalized = severity.toLowerCase();
  if (normalized === 'error') return 0;
  if (normalized === 'warning') return 1;
  if (normalized === 'ok') return 2;
  return 3;
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

export function groupRoutes(routes: RouteItem[]): RouteGroup[] {
  const groups = new Map<string, RouteItem[]>();
  for (const route of routes) {
    const items = groups.get(route.ingressId);
    if (items) {
      items.push(route);
    } else {
      groups.set(route.ingressId, [route]);
    }
  }
  return Array.from(groups.entries())
    .sort(([, leftGroup], [, rightGroup]) => {
      return leftGroup[0].ingress.localeCompare(rightGroup[0].ingress);
    })
    .map(([id, group]) => ({
      id,
      namespace: group[0].namespace,
      ingress: group[0].ingress,
      rootKind: group[0].rootKind,
      routes: [...group].sort((left, right) => {
        const severity = severityRank(left.severity) - severityRank(right.severity);
        if (severity !== 0) return severity;
        const host = left.host.localeCompare(right.host);
        if (host !== 0) return host;
        return left.path.localeCompare(right.path);
      }),
    }));
}

export function groupRoutesByHost(routes: RouteItem[]): RouteHostGroup[] {
  const groups = new Map<string, RouteItem[]>();
  for (const route of routes) {
    const items = groups.get(route.hostLaneId);
    if (items) {
      items.push(route);
    } else {
      groups.set(route.hostLaneId, [route]);
    }
  }
  return Array.from(groups.entries())
    .sort(([, leftGroup], [, rightGroup]) => {
      const ingress = leftGroup[0].ingress.localeCompare(rightGroup[0].ingress);
      if (ingress !== 0) return ingress;
      return leftGroup[0].host.localeCompare(rightGroup[0].host);
    })
    .map(([id, group]) => ({
      id,
      ingressId: group[0].ingressId,
      ingress: group[0].ingress,
      host: group[0].host,
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

function laneMatchesSelection(laneId: string, selectedRouteIds: string[], selectedLaneIds: string[]): boolean {
  return selectedLaneIds.includes(laneId) || selectedRouteIds.includes(laneId);
}

export function focusNodesByRoute(nodes: TopologyNode[], selectedRouteIds: string[], selectedLaneIds: string[]): TopologyNode[] {
  if (selectedRouteIds.length === 0 && selectedLaneIds.length === 0) {
    return nodes;
  }

  return nodes.map((node) => {
    const laneId = laneIdForNode(node);
    const active = laneMatchesSelection(laneId, selectedRouteIds, selectedLaneIds);
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

export function filterNodesByRoute(nodes: TopologyNode[], selectedRouteIds: string[], selectedLaneIds: string[]): TopologyNode[] {
  if (selectedRouteIds.length === 0 && selectedLaneIds.length === 0) {
    return nodes;
  }

  return nodes.filter((node) => {
    const laneId = laneIdForNode(node);
    return laneMatchesSelection(laneId, selectedRouteIds, selectedLaneIds);
  });
}

export function filterEdgesForNodes(edges: TopologyEdge[], nodes: TopologyNode[]): TopologyEdge[] {
  const nodeIds = new Set(nodes.map((node) => node.id));
  return edges.filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target));
}

export function focusEdgesByRoute(edges: TopologyEdge[], selectedRouteIds: string[], selectedLaneIds: string[]): TopologyEdge[] {
  if (selectedRouteIds.length === 0 && selectedLaneIds.length === 0) {
    return edges;
  }

  return edges.map((edge) => {
    const laneIds = laneIdsForEdge(edge);
    const active = laneIds.some((laneId) => laneMatchesSelection(laneId, selectedRouteIds, selectedLaneIds));
    return {
      ...edge,
      animated: Boolean(edge.animated && active),
      style: {
        ...(edge.style ?? {}),
        strokeOpacity: active ? 1 : 0.2,
        strokeWidth: active ? 1.75 : 1,
      },
      zIndex: active ? 20 : 0,
    };
  });
}
