'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Node } from '@/types';
import Link from 'next/link';
import { Activity, Cpu, Plus, RefreshCw, Server } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { Badge, StatusBadge } from '@/components/ui/Badge';
import { EmptyState } from '@/components/ui/Card';

function gb(bytes?: number) {
  return bytes ? `${(bytes / 1024 ** 3).toFixed(bytes > 100 * 1024 ** 3 ? 0 : 1)} GB` : '—';
}

function Meter({ label, used, total }: { label: string; used?: number; total?: number }) {
  const pct = used && total ? Math.round((used / total) * 100) : 0;
  return (
    <div>
      <div className="flex justify-between text-xs text-text-secondary">
        <span>{label}</span>
        <span>{used && total ? `${gb(used)} / ${gb(total)}` : '—'}</span>
      </div>
      <div className="w-full h-1.5 bg-surface-hover rounded-full overflow-hidden mt-1">
        <div
          className={`h-full rounded-full ${pct > 90 ? 'bg-status-red' : pct > 75 ? 'bg-status-yellow' : 'bg-status-green'}`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

export default function NodesPage() {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function loadNodes() {
    try {
      const data = await api.getNodes();
      const sorted = data.sort((a, b) => {
        if (a.online !== b.online) {
          return a.online ? -1 : 1;
        }
        return a.vpn_ip.localeCompare(b.vpn_ip, undefined, { numeric: true });
      });
      setNodes(sorted);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load nodes');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadNodes();
    const interval = setInterval(loadNodes, 15000);
    return () => clearInterval(interval);
  }, []);

  if (loading) {
    return <div className="flex items-center justify-center h-96 text-sm text-text-secondary">Loading…</div>;
  }

  if (error) {
    return <div className="flex items-center justify-center h-96 text-sm text-status-red">{error}</div>;
  }

  const online = nodes.filter(n => n.online).length;

  return (
    <div className="space-y-6">
      <PageHeader
        icon={Server}
        title="Nodes"
        description={`The machines that run your apps: ${online} of ${nodes.length} online.`}
        actions={
          <>
            <Button variant="ghost" icon={RefreshCw} onClick={loadNodes}>Refresh</Button>
            <a href="https://docs.asdl.website/hub/add-a-node/" target="_blank" rel="noreferrer">
              <Button variant="primary" icon={Plus}>Add a node</Button>
            </a>
          </>
        }
      />

      {nodes.length === 0 ? (
        <EmptyState>No nodes yet. Add one with the installer command from the docs.</EmptyState>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {nodes.map(node => {
            const score = node.health_score || 0;
            const state = !node.online ? 'offline' : node.maintenance ? 'maintenance' : 'online';
            return (
              <Link
                key={node.id}
                href={`/nodes/detail?id=${node.id}`}
                className={`block bg-surface border border-border rounded-lg p-5 transition-colors hover:border-border-strong ${
                  node.online ? '' : 'opacity-70'
                }`}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium text-text-primary truncate">{node.hostname}</div>
                    <div className="text-xs text-text-secondary font-mono mt-0.5">{node.vpn_ip}</div>
                  </div>
                  <StatusBadge status={state} />
                </div>

                <div className="flex items-center gap-4 mt-4 text-xs text-text-secondary">
                  <span className="flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />{node.os} · {node.architecture}</span>
                  <span className="flex items-center gap-1.5"><Cpu className="h-3.5 w-3.5" />{node.cpu_cores} cores</span>
                  {node.online && (
                    <span className={`flex items-center gap-1.5 ml-auto ${score >= 80 ? 'text-status-green' : score >= 50 ? 'text-status-yellow' : 'text-status-red'}`}>
                      <Activity className="h-3.5 w-3.5" />
                      {score}
                    </span>
                  )}
                </div>

                <div className="space-y-2.5 mt-4 pt-4 border-t border-border">
                  <Meter label="Memory" used={node.memory_used} total={node.memory_total} />
                  <Meter label="Disk" used={node.disk_used} total={node.disk_total} />
                </div>

                {node.capabilities.length > 0 && (
                  <div className="mt-4 flex flex-wrap gap-1.5">
                    {node.capabilities.map(cap => (
                      <Badge key={cap}>{cap}</Badge>
                    ))}
                  </div>
                )}
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}
