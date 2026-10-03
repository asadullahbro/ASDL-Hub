'use client';

import { useState } from 'react';
import { Wrench } from 'lucide-react';
import { api } from '@/lib/api';
import type { MaintenanceResult, Node } from '@/types';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { StatusBadge } from '@/components/ui/Badge';

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
    <Card
      icon={Wrench}
      title={<>Maintenance mode {on && <StatusBadge status="maintenance" label="on" />}</>}
      description={
        on
          ? `Since ${node.maintenance_since ? new Date(node.maintenance_since).toLocaleString() : '—'}${node.maintenance_by ? `, by ${node.maintenance_by}` : ''}. This node gets no new apps and is never used for failover.`
          : 'Before rebooting or working on this machine: moves its apps to other nodes without downtime, and keeps new ones off it until you switch this back.'
      }
      actions={
        canEdit && (
          <Button variant={on ? 'secondary' : 'primary'} loading={busy} onClick={toggle}>
            {on ? 'End maintenance' : 'Start maintenance'}
          </Button>
        )
      }
    >
      {(result?.node.maintenance || error) && (
        <div className="text-xs space-y-1">
          {result?.node.maintenance && result.moving.length === 0 && result.stays.length === 0 && (
            <div className="text-text-secondary">No apps were running here.</div>
          )}
          {result?.node.maintenance && result.moving.map(m => <div key={m} className="text-text-secondary">Moving {m}</div>)}
          {result?.node.maintenance && result.stays.map(m => (
            <div key={m} className="text-status-yellow">{m} stays here: no other node is available</div>
          ))}
          {error && <div className="text-status-red">{error}</div>}
        </div>
      )}
    </Card>
  );
}
