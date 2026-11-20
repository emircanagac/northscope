import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { TopologyNode } from '../hooks/useTopologyStream';
import { KindIcon, kindLabel } from './KindIcon';
import { StateLabel, cx, type Tone } from './ui';

function isErrorStatus(status: string): boolean {
  const normalized = status.toLowerCase();
  if (normalized.startsWith('0 ready') || normalized.includes('no pods')) {
    return true;
  }
  return ['error', 'failed', 'crash', 'imagepull', 'errimagepull', 'notready'].some((value) =>
    normalized.includes(value),
  );
}

function statusTone(status: string, hasError: boolean): Tone {
  if (hasError) {
    return 'danger';
  }
  const normalized = status.toLowerCase();
  if (normalized.includes('pending') || normalized.includes('inferred') || normalized.includes('warning')) {
    return 'attention';
  }
  if (['ready', 'running', 'active', 'accepted', 'programmed'].some((value) => normalized.includes(value))) {
    return 'success';
  }
  return 'neutral';
}

function statusDisplayLabel(status: string): string {
  const normalized = status.toLowerCase();
  const labels: Array<[string, string]> = [
    ['containersnotready', 'Not ready'],
    ['ready=false', 'Not ready'],
    ['notready', 'Not ready'],
    ['containercreating', 'Creating'],
    ['podinitializing', 'Initializing'],
    ['imagepullbackoff', 'Image pull'],
    ['errimagepull', 'Image pull'],
    ['invalidimagename', 'Bad image'],
    ['crashloopbackoff', 'CrashLoop'],
    ['createcontainerconfigerror', 'Config error'],
    ['createcontainererror', 'Create error'],
    ['runcontainererror', 'Run error'],
    ['oomkilled', 'OOMKilled'],
    ['ready=true', 'Ready'],
  ];

  for (const [needle, label] of labels) {
    if (normalized.includes(needle)) {
      return label;
    }
  }

  return status;
}

function shouldShowStatus(data: TopologyNode['data'], status: string): boolean {
  if (data.kind === 'Ingress' || data.kind === 'NodePort') {
    return false;
  }
  if (data.kind === 'Controller') {
    return status === 'Inferred' || isErrorStatus(status);
  }
  return Boolean(status && status !== 'Unknown');
}

function nodeTitle(data: TopologyNode['data']): string {
  if (data.kind === 'NodePort') {
    const servicePort = data.properties?.servicePort;
    const nodePort = data.properties?.nodePort;
    const protocol = data.properties?.protocol;
    if (servicePort && nodePort) {
      return `${servicePort} -> ${nodePort}${protocol ? `/${protocol}` : ''}`;
    }
  }

  return data.name;
}

function shortResourceName(value: string): string {
  return value.split('/').filter(Boolean).pop() ?? value;
}

function selectedPathCount(value?: string): number {
  if (!value) {
    return 0;
  }
  return value
    .split(',')
    .map((path) => path.trim())
    .filter(Boolean).length;
}

function portSummary(data: TopologyNode['data']): string | null {
  if (data.kind === 'ExternalEdge') {
    return data.properties?.role ?? null;
  }

  if (data.kind === 'NodePort') {
    return shortResourceName(data.properties?.service ?? data.name);
  }

  if (data.kind === 'Pod') {
    return data.properties?.podIP ? `podIP ${data.properties.podIP}` : null;
  }

  if (data.kind === 'PodGroup') {
    return data.properties?.summary ?? null;
  }

  if (data.kind === 'Controller') {
    return data.properties?.controller ?? data.properties?.className ?? null;
  }

  if (data.kind === 'Ingress' && data.properties?.selectedHost) {
    const parts: string[] = [];
    if (data.properties.ingressName) {
      parts.push(`Ingress ${data.properties.ingressName}`);
    }
    if (data.properties.selectedPaths) {
      const pathCount = selectedPathCount(data.properties.selectedPaths);
      parts.push(pathCount > 0 ? `${pathCount} path${pathCount === 1 ? '' : 's'}` : `paths ${data.properties.selectedPaths}`);
    }
    if (data.properties.className) {
      parts.push(`class ${data.properties.className}`);
    }
    return parts.join(' / ');
  }

  if (data.properties?.ports) {
    return data.properties.ports;
  }

  const parts: string[] = [];

  if (data.properties?.addresses) {
    parts.push(data.properties.addresses);
  }
  if (data.properties?.address) {
    parts.push(data.properties.address);
  }
  if (data.properties?.externalName) {
    parts.push(data.properties.externalName);
  }
  if (data.properties?.hostnames) {
    parts.push(data.properties.hostnames);
  }
  if (data.properties?.hosts) {
    parts.push(data.properties.hosts);
  }
  if (data.properties?.className) {
    parts.push(`class ${data.properties.className}`);
  }
  if (data.properties?.listeners) {
    parts.push(data.properties.listeners);
  }
  if (data.properties?.servicePort) {
    parts.push(data.properties.servicePort);
  }
  if (data.properties?.nodePort) {
    parts.push(`nodePort ${data.properties.nodePort}`);
  }
  if (data.properties?.protocol) {
    parts.push(data.properties.protocol);
  }
  if (data.properties?.externalIPs) {
    parts.push(data.properties.externalIPs);
  }
  if (data.properties?.nodeName) {
    parts.push(data.properties.nodeName);
  }
  if (data.properties?.podIP) {
    parts.push(data.properties.podIP);
  }

  if (parts.length === 0) {
    return null;
  }

  return parts.join(' / ');
}

// KubeNodeComponent draws one resource as a Primer-style box: kind and state
// on top, the resource name, its namespace, and one line of addresses or
// ports. Color only marks state; the kind is told by icon and label.
function KubeNodeComponent({ data, selected }: NodeProps<TopologyNode>) {
  const status = String(data.status ?? data.phase ?? 'Unknown');
  const hasError = isErrorStatus(status);
  const tone = statusTone(status, hasError);
  const summaryLine = portSummary(data);
  const showStatus = shouldShowStatus(data, status);
  const title = nodeTitle(data);

  return (
    <div
      data-testid="topology-node-card"
      data-kind={data.kind}
      className={cx(
        'relative flex h-[132px] w-[260px] flex-col overflow-hidden rounded-lg border bg-surface shadow-card',
        hasError ? 'border-danger' : tone === 'attention' ? 'border-attention' : 'border-line',
        selected && 'ring-2 ring-accent',
      )}
    >
      <Handle type="target" position={Position.Left} className="!h-2 !w-2 !border !border-line !bg-surface" />
      <div className="flex min-w-0 items-center gap-2 px-3 pt-2.5">
        <KindIcon kind={data.kind} className={hasError ? 'text-danger' : 'text-fg-muted'} />
        <span className="min-w-0 flex-1 truncate text-xs font-medium text-fg-muted">{kindLabel(data.kind)}</span>
        {showStatus ? (
          <StateLabel tone={tone} title={status} className="max-w-[128px]">
            {statusDisplayLabel(status)}
          </StateLabel>
        ) : null}
      </div>
      <div className="min-w-0 flex-1 px-3 pt-1.5">
        <div title={title} className="line-clamp-2 break-words text-sm font-semibold leading-snug text-fg">
          {title}
        </div>
        {data.namespace ? <div className="mt-0.5 truncate text-xs text-fg-muted">{data.namespace}</div> : null}
      </div>
      {summaryLine ? (
        <div
          title={summaryLine}
          className="truncate border-t border-line-muted bg-surface-muted px-3 py-1.5 font-mono text-[11px] leading-4 text-fg-muted"
        >
          {summaryLine}
        </div>
      ) : null}
      <Handle type="source" position={Position.Right} className="!h-2 !w-2 !border !border-line !bg-surface" />
    </div>
  );
}

export const KubeNode = memo(KubeNodeComponent);
