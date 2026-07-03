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
  if (kind === 'F5') {
    return {
      card: 'border-slate-400 bg-white shadow-slate-200',
      icon: 'bg-slate-900 text-white',
      badge: 'bg-slate-900 text-white',
      initials: 'F5',
    };
  }

  if (kind === 'Ingress') {
    return {
      card: 'border-blue-300 bg-blue-50 shadow-blue-100',
      icon: 'bg-blue-600 text-white',
      badge: 'bg-blue-100 text-blue-700',
      initials: 'IN',
    };
  }

  if (kind === 'DNS') {
    return {
      card: 'border-slate-300 bg-slate-50 shadow-slate-100',
      icon: 'bg-slate-700 text-white',
      badge: 'bg-slate-100 text-slate-700',
      initials: 'DN',
    };
  }

  if (kind === 'Controller') {
    return {
      card: 'border-amber-300 bg-amber-50 shadow-amber-100',
      icon: 'bg-amber-600 text-white',
      badge: 'bg-amber-100 text-amber-700',
      initials: 'CT',
    };
  }

  if (kind === 'Gateway') {
    return {
      card: 'border-teal-300 bg-teal-50 shadow-teal-100',
      icon: 'bg-teal-700 text-white',
      badge: 'bg-teal-100 text-teal-700',
      initials: 'GW',
    };
  }

  if (kind === 'Route') {
    return {
      card: 'border-cyan-300 bg-cyan-50 shadow-cyan-100',
      icon: 'bg-cyan-700 text-white',
      badge: 'bg-cyan-100 text-cyan-700',
      initials: 'RT',
    };
  }

  if (kind === 'LoadBalancer') {
    return {
      card: 'border-sky-300 bg-sky-50 shadow-sky-100',
      icon: 'bg-sky-600 text-white',
      badge: 'bg-sky-100 text-sky-700',
      initials: 'LB',
    };
  }

  if (kind === 'NodePort') {
    return {
      card: 'border-orange-300 bg-orange-50 shadow-orange-100',
      icon: 'bg-orange-600 text-white',
      badge: 'bg-orange-100 text-orange-700',
      initials: 'NP',
    };
  }

  if (kind === 'Node') {
    return {
      card: 'border-zinc-300 bg-zinc-50 shadow-zinc-100',
      icon: 'bg-zinc-700 text-white',
      badge: 'bg-zinc-100 text-zinc-700',
      initials: 'NO',
    };
  }

  if (kind === 'Service') {
    return {
      card: 'border-emerald-300 bg-emerald-50 shadow-emerald-100',
      icon: 'bg-emerald-600 text-white',
      badge: 'bg-emerald-100 text-emerald-700',
      initials: 'SV',
    };
  }

  if (kind === 'Pod') {
    return {
      card: 'border-violet-300 bg-violet-50 shadow-violet-100',
      icon: 'bg-violet-600 text-white',
      badge: 'bg-violet-100 text-violet-700',
      initials: 'PO',
    };
  }

  return {
    card: 'border-slate-300 bg-white shadow-slate-100',
    icon: 'bg-slate-700 text-white',
    badge: 'bg-slate-100 text-slate-700',
    initials: kind.slice(0, 2).toUpperCase(),
  };
}

function isErrorStatus(status: string): boolean {
  const normalized = status.toLowerCase();
  return ['error', 'failed', 'crash', 'imagepull', 'errimagepull', 'notready'].some((value) =>
    normalized.includes(value),
  );
}

function statusBadgeClass(status: string, hasError: boolean): string {
  if (hasError) {
    return 'bg-red-100 text-red-700';
  }
  if (status.toLowerCase().includes('pending')) {
    return 'bg-amber-100 text-amber-700';
  }
  return 'bg-white/80 text-slate-600';
}

function portSummary(data: TopologyNode['data']): string | null {
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

  return (
    <div
      className={cx(
        'group relative grid w-[250px] gap-3 rounded-lg border p-3 shadow-lg transition duration-150',
        selected && 'ring-2 ring-slate-900/20',
        hasError ? 'border-red-500 bg-red-50 shadow-red-200' : tone.card,
      )}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!h-3 !w-3 !border-2 !border-white !bg-slate-500"
      />
      <div className="flex items-start gap-3">
        <div
          className={cx(
            'grid h-9 w-9 shrink-0 place-items-center rounded-lg text-[11px] font-black tracking-wide',
            hasError ? 'bg-red-600 text-white' : tone.icon,
          )}
        >
          {tone.initials}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className={cx('rounded-full px-2 py-0.5 text-[10px] font-bold uppercase', tone.badge)}>
              {data.kind}
            </span>
            <span
              className={cx(
                'rounded-full px-2 py-0.5 text-[10px] font-bold',
                statusBadgeClass(status, hasError),
              )}
            >
              {status}
            </span>
          </div>
          <div className="mt-2 line-clamp-3 break-words text-sm font-bold leading-tight text-slate-950">{data.name}</div>
          {data.namespace ? <div className="mt-1 truncate text-xs font-medium text-slate-500">{data.namespace}</div> : null}
        </div>
      </div>
      {summaryLine ? (
        <div className="line-clamp-2 break-words rounded-md bg-white/70 px-2 py-1 text-[11px] font-medium text-slate-600">
          {summaryLine}
        </div>
      ) : null}
      <Handle
        type="source"
        position={Position.Right}
        className="!h-3 !w-3 !border-2 !border-white !bg-slate-500"
      />
    </div>
  );
}
