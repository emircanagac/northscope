import { Check, Link, LogOut, Moon, Sun, Telescope } from 'lucide-react';
import { useEffect, useState } from 'react';
import { LOGOUT_PATH, type SessionInfo } from '../auth';
import { copyText } from '../clipboard';
import type { StreamStatus, TopologySnapshot } from '../hooks/useTopologyStream';
import type { TopologyMode } from '../trafficGraph';
import { streamStatusLabel } from '../watchHealth';
import { SnapshotAge } from './SnapshotAge';
import { Button, SegmentedControl, cx } from './ui';

function CopyLinkButton() {
  const [result, setResult] = useState<'copied' | 'failed' | ''>('');
  useEffect(() => {
    if (!result) {
      return;
    }
    const timer = window.setTimeout(() => setResult(''), 1600);
    return () => window.clearTimeout(timer);
  }, [result]);

  return (
    <Button
      data-testid="copy-route-link"
      icon={result === 'copied' ? Check : Link}
      title="Copy a link that opens this route"
      onClick={() => void copyText(window.location.href).then((copied) => setResult(copied ? 'copied' : 'failed'))}
    >
      <span aria-live="polite">{result === 'copied' ? 'Link copied' : result === 'failed' ? 'Copy failed' : 'Copy link'}</span>
    </Button>
  );
}

function StreamStatusLabel({ status, hasSnapshot, degraded }: { status: StreamStatus; hasSnapshot: boolean; degraded: boolean }) {
  const live = status === 'connected' && hasSnapshot && !degraded;
  const waiting = status === 'connecting' || status === 'connected';
  return (
    <span
      role="status"
      data-testid="stream-status"
      className={cx(
        'inline-flex h-6 items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 text-xs font-medium',
        live ? 'border-success text-success' : waiting ? 'border-attention text-attention' : 'border-danger text-danger',
      )}
    >
      <span aria-hidden="true" className="h-2 w-2 rounded-full bg-current" />
      {streamStatusLabel(status, hasSnapshot, degraded)}
    </span>
  );
}

export interface AppHeaderProps {
  appVersion: string | null;
  inventory: string;
  topologyMode: TopologyMode;
  onTopologyModeChange: (mode: TopologyMode) => void;
  routeSelected: boolean;
  snapshot: TopologySnapshot | null;
  status: StreamStatus;
  degraded: boolean;
  isDark: boolean;
  onToggleTheme: () => void;
  session: SessionInfo | null;
}

// AppHeader is a Primer-style top bar: product and version, what the cluster
// holds, then view controls and connection state on the right.
export function AppHeader({
  appVersion,
  inventory,
  topologyMode,
  onTopologyModeChange,
  routeSelected,
  snapshot,
  status,
  degraded,
  isDark,
  onToggleTheme,
  session,
}: AppHeaderProps) {
  return (
    <header className="flex min-h-14 shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-b border-line bg-surface px-4 py-2">
      <div className="flex min-w-0 items-center gap-2">
        <Telescope aria-hidden="true" className="h-5 w-5 text-fg" strokeWidth={1.75} />
        <h1 className="text-[15px] font-semibold text-fg">NorthScope</h1>
        {appVersion ? (
          <span data-testid="app-version" className="rounded-full border border-line px-2 text-xs leading-5 text-fg-muted">
            {appVersion}
          </span>
        ) : null}
      </div>
      <p className="hidden min-w-0 flex-1 truncate text-xs text-fg-muted xl:block" title={inventory}>
        {inventory}
      </p>
      <div className="ml-auto flex flex-wrap items-center gap-2">
        <SegmentedControl
          label="Layout"
          value={topologyMode}
          onChange={onTopologyModeChange}
          options={[
            { value: 'simple', label: 'Simple' },
            { value: 'expanded', label: 'Expanded' },
          ]}
        />
        {routeSelected ? <CopyLinkButton /> : null}
        {snapshot ? <SnapshotAge generatedAt={snapshot.generatedAt} version={snapshot.version} /> : null}
        <StreamStatusLabel status={status} hasSnapshot={Boolean(snapshot)} degraded={degraded} />
        <Button
          variant="invisible"
          className="w-8 justify-center px-0"
          icon={isDark ? Sun : Moon}
          onClick={onToggleTheme}
          aria-label={isDark ? 'Switch to light theme' : 'Switch to dark theme'}
          title={isDark ? 'Switch to light theme' : 'Switch to dark theme'}
        />
        {session?.authEnabled ? (
          <form method="post" action={LOGOUT_PATH}>
            <Button type="submit" icon={LogOut} title={session.username ? `Signed in as ${session.username}` : undefined}>
              Sign out
            </Button>
          </form>
        ) : null}
      </div>
    </header>
  );
}
