'use client';

import { useEffect, useState } from 'react';
import { Layers } from 'lucide-react';
import { api } from '@/lib/api';
import type { Node } from '@/types';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';

const MAX_PARALLEL = 8;

function gb(bytes?: number): string {
  return bytes ? `${(bytes / 1024 ** 3).toFixed(1)} GB` : 'unknown';
}

// How many jobs (deploys, failovers, clean-ups) the node runs at once. Set from its hardware when it enrolls; the
// Hub still only starts an extra job while the node has memory to spare, and never two for the same app.
export function NodeParallelJobs({ node, canEdit, onChange }: { node: Node; canEdit: boolean; onChange: () => void }) {
  const recommended = node.recommended_parallel_jobs || 1;
  const current = node.parallel_jobs || recommended;
  const [value, setValue] = useState(current);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const supported = (node.capabilities || []).includes('parallel_jobs');
  const cores = node.cpu_cores || node.cpu;

  useEffect(() => setValue(current), [current]);

  const save = async (n: number) => {
    setBusy(true);
    setError(null);
    try {
      await api.setNodeParallelJobs(node.id, n);
      onChange();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save');
    } finally {
      setBusy(false);
    }
  };

  const parallel = value > 1;
  return (
    <Card
      icon={Layers}
      title="Parallel jobs"
      description={
        <>
          How many deploys, failovers and clean-ups this node runs at once. Recommended for its hardware (
          {cores ? `${cores} cores` : 'cores unknown'}, {gb(node.memory_total)} RAM): <b>{recommended}</b> — one per
          4 GB of RAM and per 2 cores, up to 4. An extra job only starts while the node has at least 1.5 GB of memory
          free and isn&apos;t overloaded, and two jobs for the same app never run together.
        </>
      }
      actions={
        canEdit && (
          <Button variant="primary" loading={busy} disabled={value === current} onClick={() => save(value)}>
            Save
          </Button>
        )
      }
    >
      <div className="space-y-2 text-sm">
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={parallel}
            disabled={!canEdit}
            onChange={e => setValue(e.target.checked ? Math.max(2, recommended) : 1)}
          />
          Run jobs in parallel
        </label>
        {parallel && (
          <label className="flex items-center gap-2 pl-6">
            Up to
            <select
              className="rounded border border-border bg-surface px-2 py-1"
              value={value}
              disabled={!canEdit}
              onChange={e => setValue(Number(e.target.value))}
            >
              {Array.from({ length: MAX_PARALLEL - 1 }, (_, i) => i + 2).map(n => (
                <option key={n} value={n}>
                  {n}
                  {n === recommended ? ' (recommended)' : ''}
                </option>
              ))}
            </select>
            jobs at once
          </label>
        )}
        {canEdit && value !== recommended && (
          <button className="text-xs text-accent hover:underline" onClick={() => setValue(recommended)}>
            Use the recommendation ({recommended})
          </button>
        )}
        {!supported && (
          <div className="text-xs text-status-yellow">
            This node&apos;s agent ({node.agent_version || 'unknown version'}) runs one job at a time. Update its agent
            to use this setting.
          </div>
        )}
        {error && <div className="text-xs text-status-red">{error}</div>}
      </div>
    </Card>
  );
}
