import type { TopologyEdge, TopologyNode } from './hooks/useTopologyStream';

export interface KindSummary {
  dns: number;
  loadBalancers: number;
  nodePorts: number;
  nodes: number;
  gateways: number;
  controllers: number;
  ingresses: number;
  routes: number;
  services: number;
  pods: number;
}

export function nodeMatchesSearch(node: TopologyNode, query: string): boolean {
  if (!query) {
    return true;
  }

  const data = node.data;
  const haystack = [
    data.label,
    data.name,
    data.namespace,
    data.kind,
    data.status,
    data.phase,
    ...Object.values(data.metadata ?? {}),
    ...Object.values(data.properties ?? {}),
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();

  return haystack.includes(query);
}

export function nodeDisplayName(node: TopologyNode): string {
  if (node.data.namespace) {
    return `${node.data.namespace}/${node.data.name}`;
  }

  return node.data.name;
}

export function isIngressNode(node: TopologyNode): boolean {
  return String(node.data.kind).toLowerCase() === 'ingress';
}

export function isControllerNode(node: TopologyNode): boolean {
  return String(node.data.kind).toLowerCase() === 'controller';
}

export function collectIngressSubgraphNodeIds(
  startId: string,
  nodes: TopologyNode[],
  edges: TopologyEdge[],
): Set<string> {
  const nodeMap = buildNodeMap(nodes);
  const { incoming, outgoing } = buildEdgeMaps(edges);
  const visited = new Set<string>();
  const stack = [startId];

  while (stack.length > 0) {
    const currentId = stack.pop();
    if (!currentId || visited.has(currentId)) {
      continue;
    }

    visited.add(currentId);
    const currentNode = nodeMap.get(currentId);
    const currentKind = String(currentNode?.data.kind ?? '').toLowerCase();

    for (const edge of outgoing.get(currentId) ?? []) {
      const edgeKind = String(edge.data?.kind ?? '').toLowerCase();

      if (currentKind === 'ingress' && edgeKind === 'routes') {
        stack.push(edge.target);
      } else if (currentKind === 'gateway' && edgeKind === 'attaches') {
        stack.push(edge.target);
      } else if (currentKind === 'route' && edgeKind === 'routes') {
        stack.push(edge.target);
      } else if (currentKind === 'service' && (edgeKind === 'selects' || edgeKind === 'endpointslice' || edgeKind === 'endpoint' || edgeKind === 'externalname')) {
        stack.push(edge.target);
      } else if (currentKind === 'nodeport' && edgeKind === 'forwards') {
        stack.push(edge.target);
      } else if (currentKind === 'loadbalancer' && edgeKind === 'exposes') {
        stack.push(edge.target);
      }
    }

    for (const edge of incoming.get(currentId) ?? []) {
      const edgeKind = String(edge.data?.kind ?? '').toLowerCase();

      if (currentKind === 'ingress' && (edgeKind === 'controls' || edgeKind === 'fronts')) {
        stack.push(edge.source);
      } else if (currentKind === 'gateway' && (edgeKind === 'controls' || edgeKind === 'resolves')) {
        stack.push(edge.source);
      } else if (currentKind === 'controller' && (edgeKind === 'forwards' || edgeKind === 'exposes')) {
        stack.push(edge.source);
      } else if (currentKind === 'route' && (edgeKind === 'attaches' || edgeKind === 'resolves')) {
        stack.push(edge.source);
      } else if (currentKind === 'service' && (edgeKind === 'balances' || edgeKind === 'routes')) {
        stack.push(edge.source);
      }
    }
  }

  return visited;
}

export function findControllerForIngress(ingressId: string, edges: TopologyEdge[]): string {
  for (const edge of edges) {
    if (edge.target === ingressId && String(edge.data?.kind ?? '').toLowerCase() === 'controls') {
      return edge.source;
    }
  }

  return '';
}

export function summarizeKinds(nodes: TopologyNode[]): KindSummary {
  return nodes.reduce(
    (summary, node) => {
      const kind = String(node.data.kind).toLowerCase();
      if (kind === 'dns') summary.dns += 1;
      if (kind === 'loadbalancer') summary.loadBalancers += 1;
      if (kind === 'nodeport') summary.nodePorts += 1;
      if (kind === 'node') summary.nodes += 1;
      if (kind === 'gateway') summary.gateways += 1;
      if (kind === 'controller') summary.controllers += 1;
      if (kind === 'ingress') summary.ingresses += 1;
      if (kind === 'route') summary.routes += 1;
      if (kind === 'service') summary.services += 1;
      if (kind === 'pod') summary.pods += 1;
      return summary;
    },
    {
      dns: 0,
      loadBalancers: 0,
      nodePorts: 0,
      nodes: 0,
      gateways: 0,
      controllers: 0,
      ingresses: 0,
      routes: 0,
      services: 0,
      pods: 0,
    },
  );
}

export function layoutHorizontally(nodes: TopologyNode[]): TopologyNode[] {
  const columns = new Map<number, TopologyNode[]>();

  for (const node of nodes) {
    const column = kindColumn(String(node.data.kind));
    if (!columns.has(column)) {
      columns.set(column, []);
    }
    columns.get(column)?.push(node);
  }

  const columnIndexes = Array.from(columns.keys()).sort((a, b) => a - b);
  const columnSpacing = 280;
  const rowSpacing = 170;

  return nodes.map((node) => {
    const column = kindColumn(String(node.data.kind));
    const visibleColumnIndex = columnIndexes.indexOf(column);
    const columnNodes = columns.get(column) ?? [];
    const sortedColumnNodes = [...columnNodes].sort((left, right) => {
      const leftName = `${left.data.namespace ?? ''}/${left.data.name}`.toLowerCase();
      const rightName = `${right.data.namespace ?? ''}/${right.data.name}`.toLowerCase();
      return leftName.localeCompare(rightName);
    });
    const rowIndex = sortedColumnNodes.findIndex((item) => item.id === node.id);

    return {
      ...node,
      position: {
        x: Math.max(0, visibleColumnIndex) * columnSpacing,
        y: Math.max(0, rowIndex) * rowSpacing,
      },
    };
  });
}

export function uniqueById(nodes: TopologyNode[]): TopologyNode[] {
  const seen = new Set<string>();
  return nodes.filter((node) => {
    if (seen.has(node.id)) {
      return false;
    }
    seen.add(node.id);
    return true;
  });
}

function buildNodeMap(nodes: TopologyNode[]): Map<string, TopologyNode> {
  return new Map(nodes.map((node) => [node.id, node]));
}

function buildEdgeMaps(edges: TopologyEdge[]): {
  incoming: Map<string, TopologyEdge[]>;
  outgoing: Map<string, TopologyEdge[]>;
} {
  const incoming = new Map<string, TopologyEdge[]>();
  const outgoing = new Map<string, TopologyEdge[]>();

  for (const edge of edges) {
    if (!outgoing.has(edge.source)) {
      outgoing.set(edge.source, []);
    }
    if (!incoming.has(edge.target)) {
      incoming.set(edge.target, []);
    }

    outgoing.get(edge.source)?.push(edge);
    incoming.get(edge.target)?.push(edge);
  }

  return { incoming, outgoing };
}

function kindColumn(kind: string): number {
  const normalized = kind.toLowerCase();
  const order = ['dns', 'loadbalancer', 'nodeport', 'node', 'controller', 'gateway', 'ingress', 'route', 'service', 'pod'];
  const index = order.indexOf(normalized);
  return index === -1 ? order.length : index;
}
