'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Node } from '@/types';
import Link from 'next/link';
import { Server, Cpu, HardDrive, Wifi, Activity } from 'lucide-react';

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
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted">Loading nodes...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-status-red">{error}</div>
      </div>
    );
  }

  if (nodes.length === 0) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted">No nodes registered</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-text-primary">Nodes</h1>
        <div className="flex items-center gap-4 text-sm text-text-muted">
          <span className="flex items-center gap-1">
            <span className="h-2 w-2 rounded-full bg-status-green inline-block" />
            Online: {nodes.filter(n => n.online).length}
          </span>
          <span className="flex items-center gap-1">
            <span className="h-2 w-2 rounded-full bg-status-red inline-block" />
            Offline: {nodes.filter(n => !n.online).length}
          </span>
          <span>Total: {nodes.length}</span>
          <button
            onClick={loadNodes}
            className="text-xs text-text-muted hover:text-text-primary transition-colors"
          >
            Refresh
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {nodes.map((node) => {
          const memPercent = node.memory_total > 0 
            ? Math.round(((node.memory_used || 0) / node.memory_total) * 100) 
            : 0;
          const healthScore = node.health_score || 0;

          return (
            <Link
              key={node.id}
              href={`/nodes/detail?id=${node.id}`}
              className={`block bg-surface border rounded-lg p-5 transition-colors ${
                node.online ? 'border-border hover:border-text-muted' : 'border-red-500/30 opacity-60'
              }`}
            >
              <div className="flex items-start justify-between mb-3">
                <div>
                  <div className="font-medium text-text-primary">{node.hostname}</div>
                  <div className="text-xs text-text-muted font-mono">{node.vpn_ip}</div>
                </div>
                <div className="flex flex-col items-end gap-1">
                  <div className="flex items-center gap-1.5">
                    <span
                      className={`inline-block h-2 w-2 rounded-full ${
                        node.online ? 'bg-status-green' : 'bg-status-red'
                      }`}
                    />
                    <span className={`text-xs ${node.online ? 'text-status-green' : 'text-status-red'}`}>
                      {node.online ? 'Online' : 'Offline'}
                    </span>
                  </div>
                  <div className="flex items-center gap-1">
                    <Activity className="h-3 w-3 text-text-muted" />
                    <span className={`text-xs font-medium ${
                      healthScore >= 80 ? 'text-status-green' : 
                      healthScore >= 50 ? 'text-status-yellow' : 
                      'text-status-red'
                    }`}>
                      {healthScore}%
                    </span>
                  </div>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-2 text-sm">
                <div className="flex items-center gap-2 text-text-secondary">
                  <Server className="h-3.5 w-3.5" />
                  <span>{node.os}</span>
                </div>
                <div className="flex items-center gap-2 text-text-secondary">
                  <Cpu className="h-3.5 w-3.5" />
                  <span>{node.cpu_cores} cores</span>
                </div>
                <div className="flex items-center gap-2 text-text-secondary">
                  <HardDrive className="h-3.5 w-3.5" />
                  <span>{Math.round(node.disk_total / 1024 / 1024 / 1024)}GB</span>
                </div>
                <div className="flex items-center gap-2 text-text-secondary">
                  <Wifi className="h-3.5 w-3.5" />
                  <span>{node.architecture}</span>
                </div>
              </div>

              {/* RAM Usage */}
              <div className="mt-3 pt-3 border-t border-border">
                <div className="flex justify-between text-xs text-text-muted">
                  <span>Memory</span>
                  <span>
                    {node.memory_used && node.memory_total 
                      ? `${Math.round(node.memory_used / 1024 / 1024 / 1024)}GB / ${Math.round(node.memory_total / 1024 / 1024 / 1024)}GB`
                      : 'N/A'
                    }
                  </span>
                </div>
                {node.memory_used && node.memory_total && (
                  <div className="w-full h-1.5 bg-surface-hover rounded-full overflow-hidden mt-1">
                    <div 
                      className={`h-full rounded-full ${
                        memPercent > 90 ? 'bg-status-red' :
                        memPercent > 75 ? 'bg-status-yellow' :
                        'bg-status-green'
                      }`}
                      style={{ width: `${memPercent}%` }}
                    />
                  </div>
                )}
              </div>

              {node.capabilities.length > 0 && (
                <div className="mt-3 pt-3 border-t border-border flex flex-wrap gap-1.5">
                  {node.capabilities.map((cap) => (
                    <span
                      key={cap}
                      className="px-2 py-0.5 bg-surface-hover rounded text-xs text-text-muted font-mono"
                    >
                      {cap}
                    </span>
                  ))}
                </div>
              )}
            </Link>
          );
        })}
      </div>
    </div>
  );
}