'use client';

import { useEffect, useState } from 'react';
import { Link2 } from 'lucide-react';
import { api } from '@/lib/api';
import type { NodeConnection as Conn } from '@/types';
import { Card } from '@/components/ui/Card';
import { StatusBadge } from '@/components/ui/Badge';

function ago(ts?: string): { text: string; secs: number } {
  if (!ts || ts.startsWith('0001')) return { text: 'never', secs: Infinity };
  const secs = Math.max(0, Math.floor((Date.now() - new Date(ts).getTime()) / 1000));
  if (secs < 60) return { text: `${secs}s ago`, secs };
  if (secs < 3600) return { text: `${Math.floor(secs / 60)}m ago`, secs };
  if (secs < 86400) return { text: `${Math.floor(secs / 3600)}h ago`, secs };
  return { text: `${Math.floor(secs / 86400)}d ago`, secs };
}

// How the Hub last heard from the node: its API heartbeats (every 30s) and
// the WireGuard tunnel's handshake (renewed about every 2 minutes).
export function NodeConnection({ nodeId }: { nodeId: string }) {
  const [c, setC] = useState<Conn | null>(null);

  useEffect(() => {
    const load = () => api.getNodeConnection(nodeId).then(setC).catch(() => {});
    load();
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, [nodeId]);

  if (!c) return null;
  const hb = ago(c.last_heartbeat);
  const wg = ago(c.wg_handshake);
  const hbOk = hb.secs < 90;
  const wgOk = wg.secs < 180;

  let verdict: string;
  if (hbOk && wgOk) verdict = 'Connected';
  else if (!wgOk && !hbOk) verdict = 'No contact: the machine may be off, or can’t reach the Hub’s WireGuard port';
  else if (wgOk && !hbOk) verdict = 'Tunnel up but no heartbeats: the agent may be stopped';
  else verdict = 'Heartbeats arriving; tunnel handshake is stale';

  return (
    <Card icon={Link2} title={<>Connection to Hub <StatusBadge status={hbOk && wgOk ? 'connected' : 'degraded'} label={hbOk && wgOk ? 'connected' : 'check'} /></>}>
      {!(hbOk && wgOk) && <div className="text-sm text-status-yellow mb-4">{verdict}</div>}
      <dl className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
        <Item label="Last heartbeat" value={hb.text} ok={hbOk} />
        <Item label="WireGuard handshake" value={c.wg_error ? 'unknown' : wg.text} ok={wgOk} title={c.wg_error} />
        <Item label="Ping to Hub" value={c.ping_latency > 0 ? `${c.ping_latency.toFixed(1)} ms` : '—'} ok />
        <Item label="Agent" value={c.agent_version || 'unknown'} ok mono />
      </dl>
    </Card>
  );
}

function Item({ label, value, ok, mono, title }: { label: string; value: string; ok: boolean; mono?: boolean; title?: string }) {
  return (
    <div title={title}>
      <dt className="text-xs text-text-secondary mb-0.5">{label}</dt>
      <dd className={`${ok ? 'text-text-primary' : 'text-status-yellow'} ${mono ? 'font-mono' : ''}`}>{value}</dd>
    </div>
  );
}
