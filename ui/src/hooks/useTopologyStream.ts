import { useEffect, useMemo, useRef, useState } from 'react';
import type { Edge, Node } from '@xyflow/react';
import type { SnapshotHealth } from '../watchHealth';

export type NodeKind =
  | 'F5'
  | 'ExternalEdge'
  | 'Internet'
  | 'DNS'
  | 'Gateway'
  | 'Ingress'
  | 'Controller'
  | 'LoadBalancer'
  | 'NodePort'
  | 'Node'
  | 'Route'
  | 'Service'
  | 'Endpoint'
  | 'EndpointSlice'
  | 'Pod'
  | 'PodGroup';

export interface TopologyNodeData extends Record<string, unknown> {
  label: string;
  kind: NodeKind;
  namespace?: string;
  name: string;
  status?: string;
  phase?: string;
  metadata?: Record<string, string>;
  properties?: Record<string, string>;
}

export interface TopologyEdgeData extends Record<string, unknown> {
  kind: string;
  metadata?: Record<string, string>;
  properties?: Record<string, string>;
}

export type TopologyNode = Node<TopologyNodeData>;
export type TopologyEdge = Edge<TopologyEdgeData>;

export interface ClusterInventory {
  ingressClasses: number;
  ingresses: number;
  gatewayClasses: number;
  gateways: number;
  gatewayRoutes: number;
  f5Resources: number;
  services: number;
  pods: number;
  nodes: number;
}

export interface TopologySnapshot {
  instance: string;
  version: number;
  generatedAt: string;
  inventory?: ClusterInventory;
  health?: SnapshotHealth;
  nodes: TopologyNode[];
  edges: TopologyEdge[];
}

export type StreamStatus = 'connecting' | 'connected' | 'disconnected';

export interface TopologyStreamState {
  snapshot: TopologySnapshot | null;
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  status: StreamStatus;
  error: string | null;
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

function resolveWebSocketURL(endpoint: string): string {
  if (endpoint.startsWith('ws://') || endpoint.startsWith('wss://')) {
    return endpoint;
  }

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}${endpoint}`;
}

export function useTopologyStream(endpoint = '/ws'): TopologyStreamState {
  const [snapshot, setSnapshot] = useState<TopologySnapshot | null>(null);
  const [status, setStatus] = useState<StreamStatus>('connecting');
  const [error, setError] = useState<string | null>(null);
  const reconnectTimer = useRef<number | null>(null);

  useEffect(() => {
    let socket: WebSocket | null = null;
    let closedByReact = false;
    let retryDelayMs = 1000;

    const connect = () => {
      setStatus('connecting');
      socket = new WebSocket(resolveWebSocketURL(endpoint));
      let firstOnConnection = true;

      socket.onopen = () => {
        retryDelayMs = 1000;
        setStatus('connected');
        setError(null);
      };

      socket.onmessage = (event) => {
        try {
          const incoming = JSON.parse(event.data) as TopologySnapshot;
          const first = firstOnConnection;
          firstOnConnection = false;
          setSnapshot((current) => acceptSnapshot(current, incoming, first));
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Invalid topology payload');
        }
      };

      socket.onerror = () => {
        setError('WebSocket connection failed');
      };

      socket.onclose = () => {
        if (closedByReact) {
          return;
        }

        setStatus('disconnected');
        reconnectTimer.current = window.setTimeout(connect, retryDelayMs);
        retryDelayMs = Math.min(retryDelayMs * 2, 10000);
      };
    };

    connect();

    return () => {
      closedByReact = true;
      if (reconnectTimer.current !== null) {
        window.clearTimeout(reconnectTimer.current);
      }
      socket?.close();
    };
  }, [endpoint]);

  const nodes = useMemo(() => snapshot?.nodes ?? [], [snapshot]);
  const edges = useMemo(() => snapshot?.edges ?? [], [snapshot]);

  return {
    snapshot,
    nodes,
    edges,
    status,
    error,
  };
}
