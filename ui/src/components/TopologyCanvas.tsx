import { useEffect, useRef, useState } from 'react';
import {
  Background,
  Controls,
  Panel,
  ReactFlow,
  useEdgesState,
  useNodesState,
  type NodeTypes,
  type ReactFlowInstance,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { reconcileTopologyEdges, reconcileTopologyNodes } from '../flowState';
import type { TopologyEdge, TopologyNode } from '../hooks/useTopologyStream';
import type { TopologyMode } from '../trafficGraph';
import type { EmptyStateText } from '../emptyState';
import { KubeNode } from './KubeNode';
import { StableEdge } from './StableEdge';

const nodeTypes = {
  northscopeNode: KubeNode,
} satisfies NodeTypes;

const edgeTypes = {
  northscopeStep: StableEdge,
};

// sameNodeIds tells whether React Flow already holds the nodes of the current
// view, so a fit does not measure the previous view's nodes.
function sameNodeIds(current: TopologyNode[], wanted: TopologyNode[]): boolean {
  return wanted.length > 0 && current.length === wanted.length && current.every((node, index) => node.id === wanted[index].id);
}

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

export interface TopologyCanvasProps {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
  // viewKey changes when the user picks another route, namespace, or layout;
  // only then is the viewport refit and are positions reset. Live updates
  // keep the user's pan, zoom, and node positions.
  viewKey: string;
  mode: TopologyMode;
  isDark: boolean;
  error: string | null;
  emptyState: EmptyStateText | null;
}

export function TopologyCanvas({ nodes, edges, viewKey, mode, isDark, error, emptyState }: TopologyCanvasProps) {
  const [flowInstance, setFlowInstance] = useState<ReactFlowInstance<TopologyNode, TopologyEdge> | null>(null);
  const [flowNodes, setNodes, onNodesChange] = useNodesState<TopologyNode>([]);
  const [flowEdges, setEdges, onEdgesChange] = useEdgesState<TopologyEdge>([]);
  const lastGraphKey = useRef('');
  // fitPending is set when the view changes and cleared once the viewport is
  // fit to the new nodes.
  const fitPending = useRef(false);

  useEffect(() => {
    const reset = lastGraphKey.current !== viewKey;
    lastGraphKey.current = viewKey;
    if (reset) {
      fitPending.current = true;
    }
    setNodes((current) => reconcileTopologyNodes(current, nodes, reset));
    setEdges((current) => reconcileTopologyEdges(current, edges, reset));
  }, [edges, nodes, setEdges, setNodes, viewKey]);

  // Fit after the new nodes reached React Flow: this effect runs in the
  // commit that hands them over, so the frame below sees them instead of the
  // previous view's nodes.
  useEffect(() => {
    if (!fitPending.current || !flowInstance || !sameNodeIds(flowNodes, nodes)) {
      return;
    }
    const frame = window.requestAnimationFrame(() => {
      fitPending.current = false;
      void flowInstance.fitView({ padding: mode === 'simple' ? 0.1 : 0.08, duration: prefersReducedMotion() ? 0 : 160 });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [flowInstance, flowNodes, mode, nodes]);

  return (
    <section aria-label="Traffic topology" className="relative min-w-0 flex-1 bg-canvas">
      <ReactFlow<TopologyNode, TopologyEdge>
        data-testid="topology-flow"
        aria-label="Traffic path graph"
        nodes={flowNodes}
        edges={flowEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onInit={setFlowInstance}
        colorMode={isDark ? 'dark' : 'light'}
        // Expanded routes to a large Service draw hundreds of Pods; only the
        // ones in view are rendered.
        onlyRenderVisibleElements
        minZoom={0.1}
      >
        <Background color="var(--ns-grid)" gap={24} />
        <Controls position="bottom-right" showInteractive={false} />
        {error ? (
          <Panel position="top-right" role="alert" className="rounded-md border border-danger bg-danger-muted px-3 py-2 text-xs text-danger shadow-card">
            {error}
          </Panel>
        ) : null}
      </ReactFlow>

      {emptyState ? (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center p-6">
          <div className="max-w-[520px] rounded-md border border-line bg-surface px-6 py-5 text-center shadow-card">
            <h2 className="text-base font-semibold text-fg">{emptyState.title}</h2>
            <p className="mt-1 text-[13px] text-fg-muted">{emptyState.description}</p>
          </div>
        </div>
      ) : null}
    </section>
  );
}
