import { Check, Copy } from 'lucide-react';
import { useEffect, useState } from 'react';
import { copyText } from '../clipboard';
import type { RouteItem } from '../trafficGraph';
import { StateLabel, toneForSeverity } from './ui';

function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) {
      return;
    }
    const timer = window.setTimeout(() => setCopied(false), 1600);
    return () => window.clearTimeout(timer);
  }, [copied]);

  return (
    <div className="flex items-start gap-1 rounded-md border border-line-muted bg-surface-muted">
      <code className="min-w-0 flex-1 select-all break-all px-2 py-1.5 font-mono text-[11px] leading-4 text-fg">{command}</code>
      <button
        type="button"
        aria-label={copied ? 'Command copied' : 'Copy command'}
        title={copied ? 'Copied' : 'Copy command'}
        onClick={() => void copyText(command).then(setCopied)}
        className="m-0.5 grid h-6 w-6 shrink-0 place-items-center rounded text-fg-muted hover:bg-surface-hover hover:text-fg"
      >
        {copied ? <Check aria-hidden="true" className="h-3.5 w-3.5 text-success" /> : <Copy aria-hidden="true" className="h-3.5 w-3.5" />}
      </button>
    </div>
  );
}

// RouteDetails explains each path of the selected host: the diagnosis, what
// to check next, and a kubectl command to start with.
export function RouteDetails({ routes }: { routes: RouteItem[] }) {
  const details = routes.filter((route) => route.diagnosis || route.nextStep || route.kubectl);
  if (details.length === 0) {
    return null;
  }
  return (
    <div data-testid="route-details" className="space-y-3 border-t border-line-muted bg-surface px-3 py-3">
      {details.map((route) => (
        <div key={route.id} className="space-y-1.5">
          <div className="flex min-w-0 items-center gap-2">
            <span className="min-w-0 truncate font-mono text-xs font-semibold text-fg">{route.path}</span>
            <StateLabel tone={toneForSeverity(route.severity)}>{route.status}</StateLabel>
          </div>
          {route.diagnosis ? <p className="break-words text-[13px] leading-snug text-fg">{route.diagnosis}</p> : null}
          {route.nextStep && route.severity !== 'ok' ? (
            <p className="break-words text-xs leading-snug text-fg-muted">{route.nextStep}</p>
          ) : null}
          {route.kubectl && route.severity !== 'ok' ? <CopyCommand command={route.kubectl} /> : null}
        </div>
      ))}
    </div>
  );
}
