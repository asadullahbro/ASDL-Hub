'use client';

import { RefreshCw } from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';

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
    <Card
      title="Hub version"
      description="The Hub checks GitHub for new releases every hour."
      actions={<Button icon={RefreshCw} loading={checking} onClick={check}>Check for updates</Button>}
    >
      <div className="text-sm space-y-1">
        <div>
          <span className="text-text-secondary">Running </span>
          <span className="font-mono text-text-primary">{v?.current ?? '…'}</span>
          {v?.checked_at && !v.checked_at.startsWith('0001') && (
            <span className="text-text-secondary"> · last checked {new Date(v.checked_at).toLocaleString()}</span>
          )}
        </div>
        <div className={`text-xs ${v?.update_available ? 'text-accent' : v?.check_error ? 'text-status-yellow' : 'text-text-secondary'}`}>{status}</div>
        {error && <div className="text-xs text-status-red">{error}</div>}
      </div>
    </Card>
  );
}
