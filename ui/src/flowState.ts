import type { TopologyEdge, TopologyNode } from './hooks/useTopologyStream';

function stableSerialize(value: unknown): string {
  if (value === null || typeof value !== 'object') {
    return JSON.stringify(value) ?? 'undefined';
  }
  if (Array.isArray(value)) {
    return `[${value.map(stableSerialize).join(',')}]`;
  }

  const record = value as Record<string, unknown>;
  return `{${Object.keys(record)
    .filter((key) => record[key] !== undefined)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableSerialize(record[key])}`)
    .join(',')}}`;
}

function nodePresentation(node: TopologyNode): Record<string, unknown> {
  return {
    id: node.id,
    type: node.type,
    position: node.position,
    data: node.data,
    style: node.style,
    className: node.className,
    hidden: node.hidden,
    draggable: node.draggable,
    selectable: node.selectable,
    connectable: node.connectable,
    deletable: node.deletable,
    sourcePosition: node.sourcePosition,
    targetPosition: node.targetPosition,
    zIndex: node.zIndex,
  };
}

function edgePresentation(edge: TopologyEdge): Record<string, unknown> {
  return {
    id: edge.id,
    source: edge.source,
    target: edge.target,
    sourceHandle: edge.sourceHandle,
    targetHandle: edge.targetHandle,
    type: edge.type,
    label: typeof edge.label === 'string' || typeof edge.label === 'number' ? edge.label : null,
    data: edge.data,
    style: edge.style,
    className: edge.className,
    animated: edge.animated,
    hidden: edge.hidden,
    selectable: edge.selectable,
    deletable: edge.deletable,
    focusable: edge.focusable,
    interactionWidth: edge.interactionWidth,
    labelStyle: edge.labelStyle,
    labelShowBg: edge.labelShowBg,
    labelBgStyle: edge.labelBgStyle,
    labelBgPadding: edge.labelBgPadding,
    labelBgBorderRadius: edge.labelBgBorderRadius,
    zIndex: edge.zIndex,
  };
}

function sameIds<T extends { id: string }>(current: T[], incoming: T[]): boolean {
  return current.length === incoming.length && current.every((item, index) => item.id === incoming[index]?.id);
}

export function reconcileTopologyNodes(
  current: TopologyNode[],
  incoming: TopologyNode[],
  resetLayout: boolean,
): TopologyNode[] {
  if (resetLayout || current.length === 0) {
    return incoming;
  }

  const preservePositions = sameIds(current, incoming);
  const currentById = new Map(current.map((node) => [node.id, node]));
  let changed = current.length !== incoming.length;

  const next = incoming.map((node) => {
    const previous = currentById.get(node.id);
    if (!previous) {
      changed = true;
      return node;
    }

    const desired = preservePositions ? { ...node, position: previous.position } : node;
    if (stableSerialize(nodePresentation(previous)) === stableSerialize(nodePresentation(desired))) {
      return previous;
    }

    changed = true;
    return {
      ...previous,
      ...desired,
      measured: previous.measured,
      selected: previous.selected,
      dragging: previous.dragging,
    };
  });

  return changed ? next : current;
}

export function reconcileTopologyEdges(
  current: TopologyEdge[],
  incoming: TopologyEdge[],
  resetGraph: boolean,
): TopologyEdge[] {
  if (resetGraph || current.length === 0) {
    return incoming;
  }

  const currentById = new Map(current.map((edge) => [edge.id, edge]));
  let changed = current.length !== incoming.length;

  const next = incoming.map((edge) => {
    const previous = currentById.get(edge.id);
    if (!previous) {
      changed = true;
      return edge;
    }

    if (stableSerialize(edgePresentation(previous)) === stableSerialize(edgePresentation(edge))) {
      return previous;
    }

    changed = true;
    return {
      ...previous,
      ...edge,
      selected: previous.selected,
    };
  });

  return changed ? next : current;
}
