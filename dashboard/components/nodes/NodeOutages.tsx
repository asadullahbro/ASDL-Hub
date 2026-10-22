'use client';

import { useEffect, useState } from 'react';
import { Clock } from 'lucide-react';
import { api } from '@/lib/api';
import type { NodeDetail, NodeOutage } from '@/types';
import { Card, EmptyState } from '@/components/ui/Card';
import { Badge } from '@/components/ui/Badge';

// A length of time in its two largest units: "2d 3h", "4h 5m", "2m 38s", "45s"
export function formatDuration(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
}

export function uptimeColor(pct: number | null | undefined): string {
  if (pct == null) return 'text-text-muted';
  if (pct >= 99.5) return 'text-status-green';
  if (pct >= 95) return 'text-status-yellow';
  return 'text-status-red';
}

const CAUSES: Record<string, { label: string; hint: string }> = {
  reboot: { label: 'restarted', hint: 'The machine restarted: a power cut or a crash.' },
  weak_link: { label: 'weak link', hint: 'The machine stayed up, but its WiFi or ping was poor just before it went quiet.' },
  connection: { label: 'connection', hint: 'The machine stayed up and looked fine, so the network or the agent dropped.' },
};

// When outages tend to start, if they cluster: the busiest three hours of the day (local time) holding at least
// half of at least four outages. Null when there is no pattern to report.
export function outagePattern(outages: NodeOutage[]): string | null {
  if (outages.length < 4) return null;
  const perHour = new Array(24).fill(0);
  for (const o of outages) perHour[new Date(o.started_at).getHours()]++;
  let best = 0;
  let bestStart = 0;
  for (let h = 0; h < 24; h++) {
    if (perHour[h] === 0) continue; // start on an hour that has an outage, so the window is as tight as it can be
    const n = perHour[h] + perHour[(h + 1) % 24] + perHour[(h + 2) % 24];
    if (n > best) {
      best = n;
      bestStart = h;
    }
  }
  if (best * 2 < outages.length) return null;
  const hh = (h: number) => `${String(h % 24).padStart(2, '0')}:00`;
  return `${best} of ${outages.length} outages began between ${hh(bestStart)} and ${hh(bestStart + 3)}`;
}

function when(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

// How available the node has been, and each time it went quiet. Hidden on a Hub too old to record outages.
export function NodeOutages({ nodeId }: { nodeId: string }) {
  const [d, setD] = useState<NodeDetail | null>(null);

  useEffect(() => {
    const load = () => api.getNodeOutages(nodeId).then(setD).catch(() => {});
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  }, [nodeId]);

  if (!d?.uptime) return null;
  const { uptime } = d;
  const outages = d.outages ?? [];
  const pattern = outagePattern(outages);

  return (
    <Card icon={Clock} title="Availability">
      <dl className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
        <Stat label="Uptime, last 7 days" value={uptime.uptime_7d} />
        <Stat label="Uptime, last 30 days" value={uptime.uptime_30d} />
        <div>
          <dt className="text-xs text-text-secondary mb-0.5">Outages, last 7 days</dt>
          <dd className="text-text-primary">{uptime.outages_7d}</dd>
        </div>
        <div>
          <dt className="text-xs text-text-secondary mb-0.5">Last outage</dt>
          <dd className="text-text-primary">
            {!uptime.last_outage
              ? 'none recorded'
              : uptime.last_outage.ended_at
                ? formatDuration(uptime.last_outage.seconds)
                : <span className="text-status-red">ongoing</span>}
          </dd>
        </div>
      </dl>

      {pattern && <p className="mt-4 text-sm text-text-secondary">{pattern}.</p>}

      {outages.length === 0 ? (
        <div className="mt-4"><EmptyState>No outages recorded.</EmptyState></div>
      ) : (
        <div className="mt-4 divide-y divide-border border-t border-border">
          {outages.map(o => (
            <div key={o.id} className="flex items-center justify-between gap-3 py-2.5 text-sm">
              <div className="text-text-primary">
                {when(o.started_at)} <span className="text-text-muted">to</span> {o.ended_at ? when(o.ended_at) : 'now'}
                {o.cause && CAUSES[o.cause] && (
                  <span className="ml-2 text-xs text-text-muted" title={CAUSES[o.cause].hint}>
                    {CAUSES[o.cause].label}
                  </span>
                )}
              </div>
              {o.ended_at ? (
                <span className="font-mono text-text-secondary">{formatDuration(o.seconds)}</span>
              ) : (
                <Badge>offline, {formatDuration((Date.now() - new Date(o.started_at).getTime()) / 1000)}</Badge>
              )}
            </div>
          ))}
        </div>
      )}

      {d.tracked_since && (
        <p className="mt-3 text-xs text-text-muted">
          Counted from {when(d.tracked_since)}, when outage tracking began. Earlier outages were not recorded.
        </p>
      )}
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: number | null }) {
  return (
    <div>
      <dt className="text-xs text-text-secondary mb-0.5">{label}</dt>
      <dd className={uptimeColor(value)}>{value == null ? '—' : `${value.toFixed(2)}%`}</dd>
    </div>
  );
}
