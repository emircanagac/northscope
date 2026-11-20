import type { ClusterInventory, TopologyNode } from './hooks/useTopologyStream';
import type { NamespaceOption } from './uiState';

export function plural(count: number, singular: string, pluralForm = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : pluralForm}`;
}

// inventoryParts lists what NorthScope watches, leaving out Gateway API and
// F5 resources the cluster does not have.
export function inventoryParts(summary: Partial<ClusterInventory>): string[] {
  const parts = [
    plural(summary.ingressClasses ?? 0, 'ingress class', 'ingress classes'),
    plural(summary.ingresses ?? 0, 'ingress', 'ingresses'),
  ];
  if ((summary.gatewayClasses ?? 0) > 0) parts.push(plural(summary.gatewayClasses ?? 0, 'gateway class', 'gateway classes'));
  if ((summary.gateways ?? 0) > 0) parts.push(plural(summary.gateways ?? 0, 'gateway'));
  if ((summary.gatewayRoutes ?? 0) > 0) parts.push(plural(summary.gatewayRoutes ?? 0, 'gateway route'));
  if ((summary.f5Resources ?? 0) > 0) parts.push(plural(summary.f5Resources ?? 0, 'F5 resource'));
  parts.push(plural(summary.services ?? 0, 'service'), plural(summary.pods ?? 0, 'pod'), plural(summary.nodes ?? 0, 'node'));
  return parts;
}

function isTrafficRoot(node: TopologyNode): boolean {
  const kind = String(node.data.kind ?? '').toLowerCase();
  if (kind === 'ingress' || kind === 'gateway') {
    return true;
  }
  return kind === 'loadbalancer' && String(node.data.properties?.provider ?? '').toLowerCase() === 'f5';
}

// namespaceOptionsFromNodes lists namespaces with traffic roots first, then
// the rest, each alphabetically.
export function namespaceOptionsFromNodes(nodes: TopologyNode[]): NamespaceOption[] {
  const roots = new Map<string, number>();
  for (const node of nodes) {
    const namespace = node.data.namespace;
    if (!namespace) {
      continue;
    }
    roots.set(namespace, (roots.get(namespace) ?? 0) + (isTrafficRoot(node) ? 1 : 0));
  }
  return Array.from(roots, ([name, ingressCount]) => ({ name, ingressCount })).sort((left, right) => {
    const rooted = Number(right.ingressCount > 0) - Number(left.ingressCount > 0);
    return rooted !== 0 ? rooted : left.name.localeCompare(right.name);
  });
}

export function filterNamespaceOptions(options: NamespaceOption[], query: string): NamespaceOption[] {
  const normalized = query.trim().toLowerCase();
  return normalized ? options.filter((option) => option.name.toLowerCase().includes(normalized)) : options;
}
