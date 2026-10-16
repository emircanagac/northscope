import { useEffect, useState } from 'react';
import { formatSnapshotAge } from '../watchHealth';

// SnapshotAge ticks on its own so the rest of the app does not re-render every
// second.
export function SnapshotAge({ generatedAt, version }: { generatedAt: string; version: number }) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [generatedAt]);

  const age = formatSnapshotAge(generatedAt, now);
  if (!age) {
    return null;
  }

  return (
    <span
      data-testid="snapshot-age"
      title={`Snapshot v${version} generated ${new Date(generatedAt).toLocaleString()}. NorthScope sends a new snapshot when topology or watch health changes.`}
      className="whitespace-nowrap text-[11px] font-semibold text-slate-500 dark:text-slate-400"
    >
      Snapshot {age}
    </span>
  );
}
