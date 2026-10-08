'use client';

import { ArrowUpCircle, RefreshCw } from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';

import { useEffect, useState } from 'react';
import { api, SystemVersion } from '@/lib/api';
import { useAuth } from '@/components/providers/AuthProvider';

// Event the update banner listens to, so a manual check shows it right away.
export const VERSION_CHECKED_EVENT = 'asdl:version-checked';
// Asks the update banner, which owns the install flow, to start the update. It
// works with the banner dismissed.
export const UPDATE_REQUESTED_EVENT = 'asdl:update-requested';

const RELEASES_URL = 'https://github.com/asadullahbro/ASDL-Hub/releases/tag/';

export function HubVersion() {
  const { user } = useAuth();
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
  else if (v?.update_available) status = `${v.latest} is available.`;
  else if (v?.latest) status = "You're on the latest version.";

  // Development builds have no release to link to.
  const releaseLink = (tag: string) => (tag.startsWith('v') ? RELEASES_URL + encodeURIComponent(tag) : '');
  const canInstall = user?.role === 'admin' && !!v?.update_available && v.can_update && !v.upgrading;

  return (
    <Card
      title="Hub version"
      description="The Hub checks GitHub for new releases every hour."
      actions={
        <div className="flex items-center gap-2">
          {canInstall && (
            <Button
              variant="primary"
              icon={ArrowUpCircle}
              onClick={() => window.dispatchEvent(new Event(UPDATE_REQUESTED_EVENT))}
            >
              Update to {v?.latest}
            </Button>
          )}
          <Button icon={RefreshCw} loading={checking} onClick={check}>Check for updates</Button>
        </div>
      }
    >
      <div className="text-sm space-y-1">
        <div>
          <span className="text-text-secondary">Running </span>
          <VersionTag tag={v?.current} href={v ? releaseLink(v.current) : ''} />
          {v?.checked_at && !v.checked_at.startsWith('0001') && (
            <span className="text-text-secondary"> · last checked {new Date(v.checked_at).toLocaleString()}</span>
          )}
        </div>
        <div className={`text-xs ${v?.update_available ? 'text-accent' : v?.check_error ? 'text-status-yellow' : 'text-text-secondary'}`}>{status}{v?.update_available && !v.check_error && v.release_url && (
            <>
              {' '}
              <a href={v.release_url} target="_blank" rel="noreferrer" className="underline underline-offset-2 hover:text-text-primary">
                What&apos;s new
              </a>
            </>
          )}
          {v?.update_available && user?.role === 'admin' && !v.can_update && ' To enable one-click updates, run the installer once on the server.'}
          {v?.update_available && user?.role !== 'admin' && ' Ask an admin to update.'}
        </div>
        {error && <div className="text-xs text-status-red">{error}</div>}
      </div>
    </Card>
  );
}

// The version, as a link to its release on GitHub when it has one.
function VersionTag({ tag, href }: { tag?: string; href: string }) {
  const cls = 'font-mono text-text-primary';
  if (!tag) return <span className={cls}>…</span>;
  if (!href) return <span className={cls}>{tag}</span>;
  return (
    <a href={href} target="_blank" rel="noreferrer" title="View this release on GitHub" className={`${cls} underline underline-offset-2 decoration-dotted hover:text-accent`}>
      {tag}
    </a>
  );
}
