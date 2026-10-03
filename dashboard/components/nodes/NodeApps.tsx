'use client';

import { useState } from 'react';
import { Boxes, FileText, RefreshCw, RotateCw } from 'lucide-react';
import { Card, EmptyState } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Badge, StatusBadge } from '@/components/ui/Badge';
import { Modal } from '@/components/ui/Modal';
import { api } from '@/lib/api';
import type { NodeContainer } from '@/types';
import { waitForJobOutput } from './jobOutput';


// The containers the node's agent last reported, with their logs and a
// restart, both run on the node as jobs.
export function NodeApps({ nodeId, containers, canEdit, online }: {
  nodeId: string;
  containers: NodeContainer[];
  canEdit: boolean;
  online: boolean;
}) {
  const [logs, setLogs] = useState<{ name: string; text: string; loading: boolean; error?: string } | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);

  const sorted = [...containers].sort((a, b) => Number(b.managed) - Number(a.managed) || a.name.localeCompare(b.name));

  const showLogs = async (name: string) => {
    setLogs({ name, text: '', loading: true });
    try {
      const { job_id } = await api.requestContainerLogs(nodeId, name, 300);
      const res = await waitForJobOutput(job_id);
      setLogs({ name, text: res.output || '(no output)', loading: false, error: res.ok ? undefined : 'The node reported an error' });
    } catch (err) {
      setLogs({ name, text: '', loading: false, error: err instanceof Error ? err.message : 'Could not fetch logs' });
    }
  };

  const restart = async (name: string) => {
    if (!window.confirm(`Restart ${name}?`)) return;
    setBusy(name);
    setMsg(null);
    try {
      const { job_id } = await api.restartNodeContainer(nodeId, name);
      const res = await waitForJobOutput(job_id);
      setMsg(res.ok ? `${name} restarted.` : `Restart failed: ${res.output}`);
    } catch (err) {
      setMsg(err instanceof Error ? err.message : 'Restart failed');
    } finally {
      setBusy(null);
    }
  };

  return (
    <Card icon={Boxes} title={`Containers on this node (${containers.length})`} description="As the node's agent reports them with each heartbeat." flush>
      {containers.length === 0 ? (
        <EmptyState>No containers reported.</EmptyState>
      ) : (
        <div className="divide-y divide-border">
          {sorted.map(c => (
            <div key={c.name} className="flex items-center justify-between gap-3 px-5 py-3">
              <div className="min-w-0">
                <div className="text-sm text-text-primary flex items-center gap-2">
                  {c.name}
                  {c.managed && <Badge tone="accent">Hub</Badge>}
                </div>
                <div className="text-xs text-text-secondary font-mono truncate mt-0.5">{c.image}</div>
                <div className="text-xs text-text-secondary mt-0.5">
                  {c.status}
                  {c.ports && <span className="font-mono"> · {c.ports}</span>}
                </div>
              </div>
              <div className="flex items-center gap-1 flex-shrink-0">
                <StatusBadge status={c.state === 'exited' ? 'failed' : c.state} label={c.state} />
                {canEdit && (
                  <>
                    <Button variant="ghost" icon={FileText} disabled={!online} title={online ? 'Show the last 300 log lines' : 'Node is offline'} onClick={() => showLogs(c.name)} />
                    <Button variant="ghost" icon={RotateCw} loading={busy === c.name} disabled={!online} title={online ? 'Restart' : 'Node is offline'} onClick={() => restart(c.name)} />
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
      {msg && <div className="px-5 pb-4 text-xs text-text-secondary">{msg}</div>}

      {logs && (
        <Modal
          title={`Logs · ${logs.name}`}
          size="xl"
          onClose={() => setLogs(null)}
          footer={!logs.loading && <Button variant="ghost" icon={RefreshCw} onClick={() => showLogs(logs.name)}>Refresh</Button>}
        >
          {logs.loading ? (
            <div className="text-xs text-text-secondary">Fetching logs from the node…</div>
          ) : (
            <>
              {logs.error && <div className="text-xs text-status-red">{logs.error}</div>}
              <pre className="text-xs font-mono text-text-secondary whitespace-pre-wrap break-all">{logs.text}</pre>
            </>
          )}
        </Modal>
      )}
    </Card>
  );
}
