'use client';

import { useEffect, useState } from 'react';
import { api, SystemVersion } from '@/lib/api';

// Event the update banner listens to, so a manual check shows it right away.
export const VERSION_CHECKED_EVENT = 'asdl:version-checked';

export function HubVersion() {
  const [v, setV] = useState<SystemVersion | null>(null);
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.getSystemVersion().then(setV).catch(() => {});
  }, []);

  const check = async () => {
    setChecking(true);
    setError(null);
    try {
      const res = await api.getSystemVersion(true);
      setV(res);
      window.dispatchEvent(new Event(VERSION_CHECKED_EVENT));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not check for updates');
    } finally {
      setChecking(false);
    }
  };

  let status = 'Not checked yet';
  if (v?.check_error) status = `Couldn't reach GitHub: ${v.check_error}`;
  else if (v?.update_available) status = `${v.latest} is available — use Update now at the top of the page.`;
  else if (v?.latest) status = "You're on the latest version.";

  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <div className="px-5 py-4 border-b border-border bg-surface">
        <h2 className="text-sm font-medium text-text-primary">Hub version</h2>
        <p className="text-xs text-text-muted mt-0.5">The Hub checks GitHub for new releases every hour.</p>
      </div>
      <div className="p-5 bg-background flex items-center justify-between gap-4">
        <div className="text-xs space-y-1">
          <div>
            <span className="text-text-muted">Running </span>
            <span className="font-mono text-text-primary">{v?.current ?? '…'}</span>
            {v?.checked_at && !v.checked_at.startsWith('0001') && (
              <span className="text-text-muted"> · last checked {new Date(v.checked_at).toLocaleString()}</span>
            )}
          </div>
          <div className={v?.update_available ? 'text-accent' : v?.check_error ? 'text-status-yellow' : 'text-text-secondary'}>
            {status}
          </div>
          {error && <div className="text-status-red">{error}</div>}
        </div>
        <button
          onClick={check}
          disabled={checking}
          className="flex-shrink-0 text-xs font-medium border border-border text-text-primary px-3 py-1.5 rounded hover:bg-surface-hover disabled:opacity-50"
        >
          {checking ? 'Checking…' : 'Check for updates'}
        </button>
      </div>
    </div>
  );
}
