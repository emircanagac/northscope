import type { ClusterInventory, TopologyEdge, TopologyNode, TopologySnapshot } from './hooks/useTopologyStream';
import type { SnapshotHealth } from './watchHealth';

// TopologyDelta mirrors models.TopologyDelta: it turns the snapshot with
// baseVersion into the one with version. The backend only sends one to a
// connection that asked for deltas and already has the base version.
export interface TopologyDelta {
  type: 'delta';
  instance: string;
  version: number;
  baseVersion: number;
  generatedAt: string;
  inventory?: ClusterInventory;
  health?: SnapshotHealth;
  upsertNodes?: TopologyNode[];
  removeNodes?: string[];
  upsertEdges?: TopologyEdge[];
  removeEdges?: string[];
}

export type TopologyMessage = TopologySnapshot | TopologyDelta;

export function isTopologyDelta(message: TopologyMessage): message is TopologyDelta {
  return (message as TopologyDelta).type === 'delta';
}

export interface AppliedMessage {
  snapshot: TopologySnapshot | null;
  // resync is set when a delta does not fit the current snapshot. The
  // connection must then start over with a full snapshot.
  resync: boolean;
}

// Versions only increase within one backend process. A new connection can
// reach a restarted process or another replica whose counter starts again at
// 1, so the first snapshot of a connection and any snapshot from a different
// instance replace the current one; only the rest must have a newer version.
export function acceptSnapshot(
  current: TopologySnapshot | null,
  incoming: TopologySnapshot,
  firstOnConnection: boolean,
): TopologySnapshot {
  if (!current || firstOnConnection || incoming.instance !== current.instance) {
    return incoming;
  }
  if (incoming.version <= current.version) {
    return current;
  }
  return incoming;
}

function byId(left: { id: string }, right: { id: string }): number {
  if (left.id < right.id) return -1;
  if (left.id > right.id) return 1;
  return 0;
}

function merge<T extends { id: string }>(items: T[], upserts: T[] = [], removals: string[] = []): T[] {
  if (upserts.length === 0 && removals.length === 0) {
    return items;
  }
  const merged = new Map(items.map((item) => [item.id, item]));
  for (const id of removals) {
    merged.delete(id);
  }
  for (const item of upserts) {
    merged.set(item.id, item);
  }
  // The backend sorts nodes and edges by ID; keep the same order so a delta
  // and a full snapshot of the same version are identical.
  return Array.from(merged.values()).sort(byId);
}

export function applyDelta(current: TopologySnapshot, delta: TopologyDelta): TopologySnapshot {
  return {
    instance: delta.instance,
    version: delta.version,
    generatedAt: delta.generatedAt,
    inventory: delta.inventory,
    health: delta.health,
    nodes: merge(current.nodes, delta.upsertNodes, delta.removeNodes),
    edges: merge(current.edges, delta.upsertEdges, delta.removeEdges),
  };
}

export function applyTopologyMessage(
  current: TopologySnapshot | null,
  message: TopologyMessage,
  firstOnConnection: boolean,
): AppliedMessage {
  if (!isTopologyDelta(message)) {
    return { snapshot: acceptSnapshot(current, message, firstOnConnection), resync: false };
  }
  if (!current || firstOnConnection || current.instance !== message.instance || current.version !== message.baseVersion) {
    return { snapshot: current, resync: true };
  }
  return { snapshot: applyDelta(current, message), resync: false };
}
