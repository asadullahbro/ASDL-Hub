'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { api, SystemVersion } from '@/lib/api';
import { useAuth } from '@/components/providers/AuthProvider';

const CHECK_INTERVAL = 30 * 60_000;
const UPGRADE_TIMEOUT = 5 * 60_000;
const INSTALL_COMMAND = 'curl -fsSL https://get.asdl.website/asdl-hub | sudo bash';

type Phase =
  | { kind: 'idle' }
  | { kind: 'upgrading'; target: string; since: number }
  | { kind: 'done'; version: string }
  | { kind: 'error'; message: string };

// Shown above every page when a newer Hub release is out. Admins can install
// it from here; the Hub restarts and the page reloads on the new version.
export function UpdateBanner() {
  const { user } = useAuth();
  const [info, setInfo] = useState<SystemVersion | null>(null);
  const [phase, setPhase] = useState<Phase>({ kind: 'idle' });
  const [dismissed, setDismissed] = useState<string | null>(null);
  const poll = useRef<ReturnType<typeof setInterval> | null>(null);

  const load = useCallback(async () => {
    try {
      const v = await api.getSystemVersion();
      setInfo(v);
      if (v.upgrading) setPhase(p => (p.kind === 'idle' ? { kind: 'upgrading', target: v.upgrading, since: Date.now() } : p));
    } catch {
      /* not signed in yet, or the Hub is restarting */
    }
  }, []);

  useEffect(() => {
    try {
      setDismissed(localStorage.getItem('update-dismissed'));
    } catch {}
    load();
    const t = setInterval(load, CHECK_INTERVAL);
    return () => clearInterval(t);
  }, [load]);

  // While upgrading, wait for the Hub to come back on the new version.
  useEffect(() => {
    if (phase.kind !== 'upgrading') return;
    poll.current = setInterval(async () => {
      if (Date.now() - phase.since > UPGRADE_TIMEOUT) {
        setPhase({
          kind: 'error',
          message: 'The update is taking longer than expected. Check /var/log/asdl-hub-upgrade.log on the server.',
        });
        return;
      }
      try {
        const v = await api.getSystemVersion();
        if (v.current === phase.target) {
          setInfo(v);
          setPhase({ kind: 'done', version: v.current });
          setTimeout(() => window.location.reload(), 2500);
        }
      } catch {
        /* the Hub is restarting */
      }
    }, 4000);
    return () => {
      if (poll.current) clearInterval(poll.current);
    };
  }, [phase]);

  const startUpdate = async () => {
    if (!info) return;
    if (!window.confirm(
      `Update ASDL Hub to ${info.latest}?\n\nThe dashboard will be unavailable for about a minute while the Hub restarts. Your apps keep running.`,
    )) return;
    try {
      const r = await api.startSystemUpdate();
      setPhase({ kind: 'upgrading', target: r.upgrading, since: Date.now() });
    } catch (err) {
      setPhase({ kind: 'error', message: err instanceof Error ? err.message : 'Could not start the update' });
    }
  };

  if (phase.kind === 'upgrading') {
    return (
      <Bar tone="accent">
        <Spinner />
        <span>
          Updating ASDL Hub to <b className="font-mono">{phase.target}</b>. The dashboard will reconnect by itself —
          apps keep running meanwhile.
        </span>
      </Bar>
    );
  }
  if (phase.kind === 'done') {
    return (
      <Bar tone="green">
        <span>✓ Updated to <b className="font-mono">{phase.version}</b>. Reloading…</span>
      </Bar>
    );
  }

  if (!info?.update_available || dismissed === info.latest) {
    return phase.kind === 'error' ? <Bar tone="red"><span>{phase.message}</span></Bar> : null;
  }

  const isAdmin = user?.role === 'admin';
  return (
    <Bar tone="accent">
      <span className="text-base leading-none">⬆</span>
      <span className="flex-1 min-w-0">
        <b>ASDL Hub <span className="font-mono">{info.latest}</span> is available</b>
        <span className="text-text-secondary"> — you&apos;re on <span className="font-mono">{info.current}</span>. </span>
        {info.release_url && (
          <a href={info.release_url} target="_blank" rel="noreferrer" className="underline underline-offset-2 hover:text-accent">
            What&apos;s new
          </a>
        )}
        {phase.kind === 'error' && <span className="block text-status-red mt-1">{phase.message}</span>}
        {isAdmin && !info.can_update && (
          <span className="block text-text-secondary mt-1">
            To enable one-click updates, run the installer once on the server:{' '}
            <code className="font-mono text-text-primary">{INSTALL_COMMAND}</code>
          </span>
        )}
        {!isAdmin && <span className="text-text-secondary"> Ask an admin to update.</span>}
      </span>
      {isAdmin && info.can_update && (
        <button
          onClick={startUpdate}
          className="flex-shrink-0 text-xs font-medium bg-accent text-background px-3 py-1.5 rounded hover:opacity-90 transition-opacity"
        >
          Update now
        </button>
      )}
      <button
        onClick={() => {
          setDismissed(info.latest);
          try {
            localStorage.setItem('update-dismissed', info.latest);
          } catch {}
        }}
        aria-label="Dismiss until the next release"
        title="Dismiss until the next release"
        className="flex-shrink-0 text-text-secondary hover:text-text-primary px-1"
      >
        ×
      </button>
    </Bar>
  );
}

function Bar({ tone, children }: { tone: 'accent' | 'green' | 'red'; children: React.ReactNode }) {
  const tones = {
    accent: 'border-accent/30 bg-accent/10',
    green: 'border-status-green/30 bg-status-green/10',
    red: 'border-status-red/30 bg-status-red/10',
  };
  return (
    <div className={`mb-4 flex items-center gap-3 rounded-lg border px-4 py-2.5 text-xs text-text-primary ${tones[tone]}`}>
      {children}
    </div>
  );
}

function Spinner() {
  return <span className="inline-block h-3.5 w-3.5 flex-shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" />;
}
