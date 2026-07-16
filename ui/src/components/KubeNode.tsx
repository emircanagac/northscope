import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { TopologyNode, NodeKind } from '../hooks/useTopologyStream';

function cx(...classes: Array<string | false | null | undefined>): string {
  return classes.filter(Boolean).join(' ');
}

function kindTone(kind: NodeKind): {
  card: string;
  icon: string;
  badge: string;
  initials: string;
} {
  if (kind === 'ExternalEdge') {
    return {
      card: 'border-slate-400 bg-white shadow-slate-200 dark:border-slate-600 dark:bg-slate-900 dark:shadow-none',
      icon: 'bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-950',
      badge: 'bg-slate-900 text-white dark:bg-slate-800 dark:text-slate-100',
      initials: 'EX',
    };
  }

  if (kind === 'F5') {
    return {
      card: 'border-slate-400 bg-white shadow-slate-200 dark:border-slate-600 dark:bg-slate-900 dark:shadow-none',
      icon: 'bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-950',
      badge: 'bg-slate-900 text-white dark:bg-slate-800 dark:text-slate-100',
      initials: 'F5',
    };
  }

  if (kind === 'Ingress') {
    return {
      card: 'border-blue-300 bg-blue-50 shadow-blue-100 dark:border-blue-700 dark:bg-blue-950/45 dark:shadow-none',
      icon: 'bg-blue-600 text-white',
      badge: 'bg-blue-100 text-blue-700 dark:bg-blue-900/70 dark:text-blue-100',
      initials: 'IN',
    };
  }

  if (kind === 'DNS') {
    return {
      card: 'border-slate-300 bg-slate-50 shadow-slate-100 dark:border-slate-700 dark:bg-slate-900 dark:shadow-none',
      icon: 'bg-slate-700 text-white',
      badge: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-100',
      initials: 'DN',
    };
  }

  if (kind === 'Controller') {
    return {
      card: 'border-amber-300 bg-amber-50 shadow-amber-100 dark:border-amber-700 dark:bg-amber-950/45 dark:shadow-none',
      icon: 'bg-amber-600 text-white',
      badge: 'bg-amber-100 text-amber-700 dark:bg-amber-900/70 dark:text-amber-100',
      initials: 'CT',
    };
  }

  if (kind === 'Gateway') {
    return {
      card: 'border-teal-300 bg-teal-50 shadow-teal-100 dark:border-teal-700 dark:bg-teal-950/45 dark:shadow-none',
      icon: 'bg-teal-700 text-white',
      badge: 'bg-teal-100 text-teal-700 dark:bg-teal-900/70 dark:text-teal-100',
      initials: 'GW',
    };
  }

  if (kind === 'Route') {
    return {
      card: 'border-cyan-300 bg-cyan-50 shadow-cyan-100 dark:border-cyan-700 dark:bg-cyan-950/45 dark:shadow-none',
      icon: 'bg-cyan-700 text-white',
      badge: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/70 dark:text-cyan-100',
      initials: 'RT',
    };
  }

  if (kind === 'LoadBalancer') {
    return {
      card: 'border-sky-300 bg-sky-50 shadow-sky-100 dark:border-sky-700 dark:bg-sky-950/45 dark:shadow-none',
      icon: 'bg-sky-600 text-white',
      badge: 'bg-sky-100 text-sky-700 dark:bg-sky-900/70 dark:text-sky-100',
      initials: 'LB',
    };
  }

  if (kind === 'NodePort') {
    return {
      card: 'border-orange-300 bg-orange-50 shadow-orange-100 dark:border-orange-700 dark:bg-orange-950/45 dark:shadow-none',
      icon: 'bg-orange-600 text-white',
      badge: 'bg-orange-100 text-orange-700 dark:bg-orange-900/70 dark:text-orange-100',
      initials: 'NP',
    };
  }

  if (kind === 'Node') {
    return {
      card: 'border-zinc-300 bg-zinc-50 shadow-zinc-100 dark:border-zinc-700 dark:bg-zinc-900 dark:shadow-none',
      icon: 'bg-zinc-700 text-white',
      badge: 'bg-zinc-100 text-zinc-700 dark:bg-zinc-800 dark:text-zinc-100',
      initials: 'NO',
    };
  }

  if (kind === 'Service') {
    return {
      card: 'border-emerald-300 bg-emerald-50 shadow-emerald-100 dark:border-emerald-700 dark:bg-emerald-950/45 dark:shadow-none',
      icon: 'bg-emerald-600 text-white',
      badge: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/70 dark:text-emerald-100',
      initials: 'SV',
    };
  }

  if (kind === 'Pod') {
    return {
      card: 'border-violet-300 bg-violet-50 shadow-violet-100 dark:border-violet-700 dark:bg-violet-950/45 dark:shadow-none',
      icon: 'bg-violet-600 text-white',
      badge: 'bg-violet-100 text-violet-700 dark:bg-violet-900/70 dark:text-violet-100',
      initials: 'PO',
    };
  }

  if (kind === 'PodGroup') {
    return {
      card: 'border-violet-300 bg-violet-50 shadow-violet-100 dark:border-violet-700 dark:bg-violet-950/45 dark:shadow-none',
      icon: 'bg-violet-600 text-white',
      badge: 'bg-violet-100 text-violet-700 dark:bg-violet-900/70 dark:text-violet-100',
      initials: 'PG',
    };
  }

  if (kind === 'Endpoint' || kind === 'EndpointSlice') {
    return {
      card: 'border-cyan-300 bg-cyan-50 shadow-cyan-100 dark:border-cyan-700 dark:bg-cyan-950/45 dark:shadow-none',
      icon: 'bg-cyan-700 text-white',
      badge: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/70 dark:text-cyan-100',
      initials: 'EP',
    };
  }

  return {
    card: 'border-slate-300 bg-white shadow-slate-100 dark:border-slate-700 dark:bg-slate-900 dark:shadow-none',
    icon: 'bg-slate-700 text-white',
    badge: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-100',
    initials: kind.slice(0, 2).toUpperCase(),
  };
}

function isErrorStatus(status: string): boolean {
  const normalized = status.toLowerCase();
  if (normalized.startsWith('0 ready') || normalized.includes('no pods')) {
    return true;
  }
  return ['error', 'failed', 'crash', 'imagepull', 'errimagepull', 'notready'].some((value) =>
    normalized.includes(value),
  );
}

function statusBadgeClass(status: string, hasError: boolean): string {
  if (hasError) {
    return 'bg-red-100 text-red-700 dark:bg-red-900/70 dark:text-red-100';
  }
  if (status.toLowerCase().includes('pending')) {
    return 'bg-amber-100 text-amber-700 dark:bg-amber-900/70 dark:text-amber-100';
  }
  return 'bg-white/80 text-slate-600 dark:bg-slate-900/80 dark:text-slate-300';
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

function kindLabel(kind: NodeKind): string {
  if (kind === 'ExternalEdge') {
    return 'F5 / LB';
  }
  if (kind === 'PodGroup') {
    return 'Pods';
  }
  if (kind === 'Endpoint' || kind === 'EndpointSlice') {
    return 'Endpoint';
  }
  return kind;
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

export function KubeNode({ data, selected }: NodeProps<TopologyNode>) {
  const status = String(data.status ?? data.phase ?? 'Unknown');
  const hasError = isErrorStatus(status);
  const tone = kindTone(data.kind);
  const summaryLine = portSummary(data);
  const showStatus = shouldShowStatus(data, status);
  const displayStatus = statusDisplayLabel(status);
  const title = nodeTitle(data);
  const cardSize = 'h-[144px] w-[260px] gap-3 p-3.5';
  const iconSize = 'h-9 w-9 text-[11px]';
  const titleSize = 'text-sm';

  return (
    <div
      data-testid="topology-node-card"
      data-kind={data.kind}
      className={cx(
        'group relative flex flex-col justify-between overflow-hidden rounded-lg border shadow-lg transition duration-150',
        cardSize,
        selected && 'ring-2 ring-slate-900/20 dark:ring-slate-100/20',
        hasError ? 'border-red-500 bg-red-50 shadow-red-200 dark:border-red-500 dark:bg-red-950/55 dark:shadow-none' : tone.card,
      )}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!h-3 !w-3 !border-2 !border-white !bg-slate-500 dark:!border-slate-950 dark:!bg-slate-400"
      />
      <div className="flex items-start gap-3">
        <div
          className={cx(
            'grid shrink-0 place-items-center rounded-lg font-black tracking-wide',
            iconSize,
            hasError ? 'bg-red-600 text-white' : tone.icon,
          )}
        >
          {tone.initials}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2 overflow-hidden">
            <span className={cx('shrink-0 rounded-full px-2 py-0.5 text-[10px] font-bold uppercase', tone.badge)}>
              {kindLabel(data.kind)}
            </span>
            {showStatus ? (
              <span
                title={status}
                className={cx(
                  'min-w-0 max-w-[116px] truncate rounded-full px-2 py-0.5 text-[10px] font-bold',
                  statusBadgeClass(status, hasError),
                )}
              >
                {displayStatus}
              </span>
            ) : null}
          </div>
          <div
            title={title}
            className={cx(
              'mt-1.5 line-clamp-2 break-words font-bold leading-tight text-slate-950 dark:text-slate-50',
              titleSize,
            )}
          >
            {title}
          </div>
          {data.namespace ? <div className="mt-1 max-w-full truncate text-xs font-medium text-slate-500 dark:text-slate-400">{data.namespace}</div> : null}
        </div>
      </div>
      {summaryLine ? (
        <div
          title={summaryLine}
          className={cx(
            'min-h-[24px] max-w-full overflow-hidden truncate whitespace-nowrap rounded-md bg-white/75 px-2 py-1 font-medium leading-4 text-slate-600 dark:bg-slate-950/70 dark:text-slate-300',
            'text-[11px]',
            data.kind === 'NodePort' ? 'font-semibold' : '',
          )}
        >
          {summaryLine}
        </div>
      ) : null}
      <Handle
        type="source"
        position={Position.Right}
        className="!h-3 !w-3 !border-2 !border-white !bg-slate-500 dark:!border-slate-950 dark:!bg-slate-400"
      />
    </div>
  );
}
