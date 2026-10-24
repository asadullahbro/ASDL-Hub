'use client';

import { useEffect, useState } from 'react';
import { CheckCircle2, AlertTriangle, XCircle, Lock } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { api, ApiError } from '@/lib/api';
import type { AppState, StatusReport } from '@/types';
import { formatDuration, uptimeColor } from '@/components/nodes/NodeOutages';

// Each state carries an icon and a word as well as its colour, so it never rests on colour alone.
const STATES: Record<AppState, { label: string; icon: LucideIcon; text: string; headline: string }> = {
  operational: { label: 'Operational', icon: CheckCircle2, text: 'text-status-green', headline: 'All systems operational' },
  degraded: { label: 'Degraded', icon: AlertTriangle, text: 'text-status-yellow', headline: 'Some systems are having problems' },
  down: { label: 'Down', icon: XCircle, text: 'text-status-red', headline: 'Systems are down' },
};

function when(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function uptime(v: number | null): string {
  return v == null ? '—' : `${v.toFixed(2)}%`;
}

type Load = { kind: 'loading' } | { kind: 'private' } | { kind: 'error' } | { kind: 'ok'; report: StatusReport; internal: boolean };

// Public once an admin turns it on (asdl-hub status-page on); until then only signed-in users see it.
export default function StatusPage() {
  const [state, setState] = useState<Load>({ kind: 'loading' });

  useEffect(() => {
    let live = true;
    const load = async () => {
      try {
        const report = await api.getPublicStatus();
        if (live) setState({ kind: 'ok', report, internal: false });
        return;
      } catch (e) {
        if (!(e instanceof ApiError) || e.status !== 404) {
          if (live) setState(s => (s.kind === 'ok' ? s : { kind: 'error' }));
          return;
        }
      }
      // Not public: a signed-in user still gets the page, with every app.
      try {
        const report = await api.getInternalStatus();
        if (live) setState({ kind: 'ok', report, internal: true });
        return;
      } catch {
        /* not signed in: the private message */
      }
      if (live) setState({ kind: 'private' });
    };
    load();
    const t = setInterval(load, 30_000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, []);

  return (
    <div className="min-h-full bg-background text-text-primary">
      <div className="mx-auto max-w-2xl px-4 py-10">
        <h1 className="text-lg font-semibold mb-6">Status</h1>

        {state.kind === 'loading' && <p className="text-sm text-text-secondary">Loading…</p>}

        {state.kind === 'error' && <p className="text-sm text-text-secondary">The status could not be loaded. Trying again shortly.</p>}

        {state.kind === 'private' && (
          <div className="flex items-start gap-3 rounded-lg border border-border bg-surface p-5">
            <Lock className="h-4 w-4 mt-0.5 text-text-secondary" />
            <p className="text-sm text-text-secondary">This status page is private. Sign in to the dashboard to see it.</p>
          </div>
        )}

        {state.kind === 'ok' && <Report report={state.report} internal={state.internal} />}
      </div>
    </div>
  );
}

function Report({ report, internal }: { report: StatusReport; internal: boolean }) {
  const overall = STATES[report.overall] ?? STATES.operational;
  const Overall = overall.icon;
  return (
    <div className="space-y-6">
      {internal && (
        <p className="text-xs text-text-muted">Private view: every app is listed. Visitors only see this page once it is made public, and then only apps that have a domain.</p>
      )}

      <section className="flex items-center gap-3 rounded-lg border border-border bg-surface p-5" aria-live="polite">
        <Overall className={`h-6 w-6 ${overall.text}`} />
        <div>
          <p className="text-sm font-semibold">{overall.headline}</p>
          <p className="text-xs text-text-secondary">Updated {when(report.updated_at)}</p>
        </div>
      </section>

      <section className="rounded-lg border border-border bg-surface">
        {report.apps.length === 0 ? (
          <p className="p-5 text-sm text-text-secondary">No apps to show yet.</p>
        ) : (
          <ul className="divide-y divide-border">
            {report.apps.map(a => {
              const s = STATES[a.status] ?? STATES.operational;
              const Icon = s.icon;
              return (
                <li key={a.name} className="flex items-center justify-between gap-4 px-5 py-3.5">
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{a.name}</p>
                    <p className="text-xs text-text-secondary tabular-nums">
                      <span className={uptimeColor(a.uptime_7d)}>{uptime(a.uptime_7d)}</span> over 7 days ·{' '}
                      <span className={uptimeColor(a.uptime_30d)}>{uptime(a.uptime_30d)}</span> over 30 days
                    </p>
                  </div>
                  <span className={`flex items-center gap-1.5 text-sm flex-shrink-0 ${s.text}`}>
                    <Icon className="h-4 w-4" />
                    <span className="text-text-primary">{s.label}</span>
                  </span>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <section>
        <h2 className="text-sm font-semibold mb-2">Recent incidents</h2>
        {report.incidents.length === 0 ? (
          <p className="text-sm text-text-secondary">No incidents in the last 30 days.</p>
        ) : (
          <ul className="divide-y divide-border rounded-lg border border-border bg-surface">
            {report.incidents.map((i, n) => (
              <li key={n} className="flex items-center justify-between gap-4 px-5 py-3 text-sm">
                <span className="min-w-0 truncate">
                  <span className="font-medium">{i.app}</span> <span className="text-text-secondary">· {when(i.started_at)}</span>
                </span>
                <span className="font-mono text-text-secondary flex-shrink-0">
                  {i.ended_at ? formatDuration(i.seconds) : <span className="text-status-red">ongoing, {formatDuration(i.seconds)}</span>}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <p className="text-xs text-text-muted">
        Availability is counted from {when(report.tracked_since)}, when the Hub began recording app outages. Earlier downtime was not recorded.
      </p>
    </div>
  );
}
