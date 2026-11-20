import { useEffect, useMemo, useRef, useState } from 'react';
import type { Edge, Node } from '@xyflow/react';
import type { SnapshotHealth } from '../watchHealth';
import { checkSession, redirectToLogin } from '../auth.ts';
import { applyTopologyMessage, type TopologyMessage } from '../topologyDelta.ts';

export { acceptSnapshot } from '../topologyDelta.ts';

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

function resolveWebSocketURL(endpoint: string): string {
  if (endpoint.startsWith('ws://') || endpoint.startsWith('wss://')) {
    return endpoint;
  }

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}${endpoint}`;
}

// The stream asks for deltas: after the first full snapshot the backend sends
// only the nodes and edges that changed, which on a large cluster is bytes
// instead of megabytes per change.
export function useTopologyStream(endpoint = '/ws?delta=1'): TopologyStreamState {
  const [snapshot, setSnapshot] = useState<TopologySnapshot | null>(null);
  const snapshotRef = useRef<TopologySnapshot | null>(null);
  const [status, setStatus] = useState<StreamStatus>('connecting');
  const [error, setError] = useState<string | null>(null);
  const reconnectTimer = useRef<number | null>(null);

  useEffect(() => {
    let socket: WebSocket | null = null;
    let closedByReact = false;
    let retryDelayMs = 1000;

    const connect = () => {
      setStatus('connecting');
      const current = new WebSocket(resolveWebSocketURL(endpoint));
      socket = current;
      let firstOnConnection = true;
      let resyncing = false;

      current.onopen = () => {
        retryDelayMs = 1000;
        setStatus('connected');
        setError(null);
      };

      current.onmessage = (event) => {
        if (resyncing) {
          return;
        }
        try {
          const message = JSON.parse(event.data) as TopologyMessage;
          const applied = applyTopologyMessage(snapshotRef.current, message, firstOnConnection);
          firstOnConnection = false;
          if (applied.resync) {
            // A delta that does not fit the current snapshot means this
            // connection's state is unknown; a new connection starts with a
            // full snapshot.
            resyncing = true;
            current.close();
            return;
          }
          if (applied.snapshot !== snapshotRef.current) {
            snapshotRef.current = applied.snapshot;
            setSnapshot(applied.snapshot);
          }
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Invalid topology payload');
        }
      };

      current.onerror = () => {
        setError('WebSocket connection failed');
      };

      current.onclose = () => {
        if (closedByReact) {
          return;
        }
        if (resyncing) {
          connect();
          return;
        }

        setStatus('disconnected');
        // A browser cannot read the status of a rejected WebSocket handshake,
        // so ask the API whether the session is gone before reconnecting.
        void checkSession().then((session) => {
          if (closedByReact) {
            return;
          }
          if (session === 'unauthenticated') {
            redirectToLogin();
            return;
          }
          reconnectTimer.current = window.setTimeout(connect, retryDelayMs);
          retryDelayMs = Math.min(retryDelayMs * 2, 10000);
        });
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
