'use client';

import { useState } from 'react';
import { Boxes, FileText, RotateCw, X } from 'lucide-react';
import { api } from '@/lib/api';
import type { NodeContainer } from '@/types';
import { waitForJobOutput } from './jobOutput';

function stateColor(state: string) {
  if (state === 'running') return 'bg-status-green/15 text-status-green';
  if (state === 'restarting') return 'bg-status-yellow/15 text-status-yellow';
  return 'bg-status-red/15 text-status-red';
}

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
    <div className="bg-surface border border-border rounded-lg p-6">
      <h2 className="text-sm font-semibold text-text-primary mb-1 flex items-center gap-2">
        <Boxes className="h-4 w-4" /> Apps on this node ({containers.length})
      </h2>
      <p className="text-xs text-text-muted mb-4">As reported by the node's agent with each heartbeat.</p>
      {containers.length === 0 ? (
        <div className="text-sm text-text-muted">No containers reported (older agents don't report them).</div>
      ) : (
        <div className="space-y-2">
          {sorted.map(c => (
            <div key={c.name} className="flex items-center justify-between gap-3 p-3 rounded border border-border">
              <div className="min-w-0">
                <div className="text-sm font-medium text-text-primary flex items-center gap-2">
                  {c.name}
                  {c.managed && <span className="px-1.5 py-0.5 rounded text-[10px] bg-accent/15 text-accent">Hub</span>}
                </div>
                <div className="text-xs text-text-muted font-mono truncate">{c.image}</div>
                <div className="text-xs text-text-muted mt-0.5">
                  {c.status}
                  {c.ports && <span className="font-mono"> · {c.ports}</span>}
                </div>
              </div>
              <div className="flex items-center gap-2 flex-shrink-0">
                <span className={`px-2 py-0.5 rounded text-xs font-medium ${stateColor(c.state)}`}>{c.state}</span>
                {canEdit && (
                  <>
                    <button
                      onClick={() => showLogs(c.name)}
                      disabled={!online}
                      title={online ? 'Show the last 300 log lines' : 'Node is offline'}
                      className="p-1.5 rounded hover:bg-surface-hover text-text-muted hover:text-text-primary disabled:opacity-40"
                    >
                      <FileText className="h-3.5 w-3.5" />
                    </button>
                    <button
                      onClick={() => restart(c.name)}
                      disabled={!online || busy === c.name}
                      title={online ? 'Restart' : 'Node is offline'}
                      className="p-1.5 rounded hover:bg-surface-hover text-text-muted hover:text-status-yellow disabled:opacity-40"
                    >
                      <RotateCw className={`h-3.5 w-3.5 ${busy === c.name ? 'animate-spin' : ''}`} />
                    </button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
      {msg && <div className="mt-3 text-xs text-text-secondary">{msg}</div>}

      {logs && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={() => setLogs(null)}>
          <div className="bg-surface border border-border rounded-lg w-full max-w-4xl max-h-[85vh] flex flex-col" onClick={e => e.stopPropagation()}>
            <div className="flex items-center justify-between px-4 py-3 border-b border-border">
              <span className="text-sm font-medium text-text-primary">Logs · {logs.name}</span>
              <div className="flex items-center gap-3">
                {!logs.loading && (
                  <button onClick={() => showLogs(logs.name)} className="text-xs text-accent hover:underline">Refresh</button>
                )}
                <button onClick={() => setLogs(null)} aria-label="Close" className="text-text-muted hover:text-text-primary">
                  <X className="h-4 w-4" />
                </button>
              </div>
            </div>
            <div className="overflow-auto p-4">
              {logs.loading ? (
                <div className="text-xs text-text-muted">Fetching logs from the node…</div>
              ) : (
                <>
                  {logs.error && <div className="text-xs text-status-red mb-2">{logs.error}</div>}
                  <pre className="text-xs font-mono text-text-secondary whitespace-pre-wrap break-all">{logs.text}</pre>
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
