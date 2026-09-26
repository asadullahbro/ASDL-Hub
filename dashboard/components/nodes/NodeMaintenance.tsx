'use client';

import { useState } from 'react';
import { Wrench } from 'lucide-react';
import { api } from '@/lib/api';
import type { MaintenanceResult, Node } from '@/types';

// Maintenance mode: no new apps on this node, never a failover target, and
// the apps on it are moved elsewhere (new copy first, then the old one is
// removed) when it's switched on.
export function NodeMaintenance({ node, canEdit, onChange }: { node: Node; canEdit: boolean; onChange: () => void }) {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<MaintenanceResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const on = !!node.maintenance;

  const toggle = async () => {
    const msg = on
      ? `Take ${node.hostname} out of maintenance?\n\nIt can receive apps again. Apps that were moved away stay where they are.`
      : `Put ${node.hostname} into maintenance?\n\nIts apps are moved to other nodes (each new copy starts before the old one is removed), and it gets no new apps until you switch this off.`;
    if (!window.confirm(msg)) return;
    setBusy(true);
    setError(null);
    try {
      setResult(await api.setNodeMaintenance(node.id, !on));
      onChange();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not change maintenance mode');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={`bg-surface border rounded-lg p-6 ${on ? 'border-accent/40' : 'border-border'}`}>
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-sm font-semibold text-text-primary mb-1 flex items-center gap-2">
            <Wrench className="h-4 w-4" /> Maintenance mode
            {on && <span className="px-2 py-0.5 rounded text-xs font-medium bg-accent/15 text-accent">on</span>}
          </h2>
          <p className="text-xs text-text-muted max-w-xl">
            {on
              ? `Since ${node.maintenance_since ? new Date(node.maintenance_since).toLocaleString() : '—'}${node.maintenance_by ? `, by ${node.maintenance_by}` : ''}. This node gets no new apps and is never used for failover.`
              : 'Before rebooting or working on this machine: moves its apps to other nodes without downtime, and keeps new ones off it until you switch this back.'}
          </p>
        </div>
        {canEdit && (
          <button
            onClick={toggle}
            disabled={busy}
            className={`flex-shrink-0 text-xs font-medium px-3 py-1.5 rounded transition-opacity disabled:opacity-50 ${
              on ? 'border border-border text-text-primary hover:bg-surface-hover' : 'bg-accent text-background hover:opacity-90'
            }`}
          >
            {busy ? 'Working…' : on ? 'End maintenance' : 'Start maintenance'}
          </button>
        )}
      </div>
      {result && result.node.maintenance && (
        <div className="mt-3 text-xs space-y-1">
          {result.moving.length === 0 && result.stays.length === 0 && <div className="text-text-muted">No apps were running here.</div>}
          {result.moving.map(m => (
            <div key={m} className="text-text-secondary">↪ Moving {m}</div>
          ))}
          {result.stays.map(m => (
            <div key={m} className="text-status-yellow">⚠ {m} stays here: no other node is available</div>
          ))}
        </div>
      )}
      {error && <div className="mt-3 text-xs text-status-red">{error}</div>}
    </div>
  );
}
