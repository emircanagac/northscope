import {
  Box,
  Boxes,
  Cable,
  Globe,
  ListTree,
  Network,
  Route,
  Server,
  Settings2,
  Shield,
  Signpost,
  Split,
  Waypoints,
  type LucideIcon,
} from 'lucide-react';
import type { NodeKind } from '../hooks/useTopologyStream';
import { cx } from './ui';

// Icons come from Lucide (ISC License, https://lucide.dev). Each resource kind
// has its own glyph so kinds stay distinguishable without color.
const kindIcons: Record<NodeKind, LucideIcon> = {
  F5: Shield,
  ExternalEdge: Shield,
  Internet: Globe,
  DNS: Signpost,
  Gateway: Split,
  Ingress: Waypoints,
  Controller: Settings2,
  LoadBalancer: Network,
  NodePort: Cable,
  Node: Server,
  Route: Route,
  Service: ListTree,
  Endpoint: Box,
  EndpointSlice: Box,
  Pod: Box,
  PodGroup: Boxes,
};

export function kindLabel(kind: NodeKind): string {
  switch (kind) {
    case 'ExternalEdge':
      return 'Entry';
    case 'PodGroup':
      return 'Pods';
    case 'EndpointSlice':
      return 'Endpoint';
    case 'LoadBalancer':
      return 'Load balancer';
    default:
      return kind;
  }
}

export function KindIcon({ kind, className }: { kind: NodeKind; className?: string }) {
  const Icon = kindIcons[kind] ?? Box;
  return <Icon aria-hidden="true" className={cx('h-4 w-4 shrink-0', className)} strokeWidth={1.75} />;
}
