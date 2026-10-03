'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Project, Node } from '@/types';
import { Heart, RefreshCw } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { StatusBadge } from '@/components/ui/Badge';
import { Card, EmptyState, StatCard } from '@/components/ui/Card';
import { tdClass, thClass } from '@/components/ui/Modal';

interface HealthStatus {
  project_id: string;
  name: string;
  status: string;
  health: string;
  node_id: string;
  last_check: string;
}

export default function HealthPage() {
  const [, setProjects] = useState<Project[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [healthStatuses, setHealthStatuses] = useState<HealthStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 15000);
    return () => clearInterval(interval);
  }, []);

  async function loadData() {
    try {
      const [projectsResponse, nodesData] = await Promise.all([
        api.getProjects(1, 100),  // Get all projects
        api.getNodes(),
      ]);
      
      // Extract data from paginated response
      const projectsData = projectsResponse.data || [];
      setProjects(projectsData);
      setNodes(nodesData);

      // Fetch health for each project
      const healthData: HealthStatus[] = [];
      for (const project of projectsData) {
        try {
          const health = await api.getProjectHealth(project.id);
          healthData.push({
            project_id: project.id,
            name: project.name,
            status: project.status,
            health: health.health || 'unknown',
            node_id: project.node_id,
            last_check: health.last_check || new Date().toISOString(),
          });
        } catch {
          healthData.push({
            project_id: project.id,
            name: project.name,
            status: project.status,
            health: 'unknown',
            node_id: project.node_id,
            last_check: new Date().toISOString(),
          });
        }
      }
      setHealthStatuses(healthData);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data');
    } finally {
      setLoading(false);
    }
  }

  const getNodeName = (nodeId: string) => nodes.find(n => n.id === nodeId)?.hostname ?? '—';
  const count = (h: string) => healthStatuses.filter(x => x.health === h).length;

  const refresh = async () => {
    setRefreshing(true);
    await loadData();
    setRefreshing(false);
  };

  if (loading) {
    return <div className="flex items-center justify-center h-96 text-sm text-text-secondary">Loading…</div>;
  }

  const online = nodes.filter(n => n.online).length;

  return (
    <div className="space-y-6">
      <PageHeader
        icon={Heart}
        title="Health"
        description="The Hub checks every app every 10 seconds. After three failed checks in a row, the app is moved to another node."
        actions={<Button variant="ghost" icon={RefreshCw} loading={refreshing} onClick={refresh}>Refresh</Button>}
      />

      {error && <div className="text-sm text-status-red">{error}</div>}

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard label="Healthy apps" value={count('healthy')} tone={count('healthy') ? 'success' : undefined} />
        <StatCard label="Degraded" value={count('degraded')} sub="failing checks, not moved yet" tone={count('degraded') ? 'warning' : undefined} />
        <StatCard label="Unhealthy" value={count('unhealthy')} tone={count('unhealthy') ? 'danger' : undefined} />
        <StatCard label="Nodes online" value={`${online} / ${nodes.length}`} tone={online < nodes.length ? 'warning' : undefined} />
      </div>

      <Card title="Apps" flush>
        {healthStatuses.length === 0 ? (
          <EmptyState>No apps to check yet.</EmptyState>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th className={thClass}>App</th>
                  <th className={thClass}>Status</th>
                  <th className={thClass}>Health</th>
                  <th className={thClass}>Node</th>
                  <th className={thClass}>Last check</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {healthStatuses.map(st => (
                  <tr key={st.project_id} className="hover:bg-surface-hover transition-colors">
                    <td className={`${tdClass} text-text-primary`}>{st.name}</td>
                    <td className={tdClass}><StatusBadge status={st.status} /></td>
                    <td className={tdClass}><StatusBadge status={st.health} /></td>
                    <td className={`${tdClass} text-text-secondary`}>{getNodeName(st.node_id)}</td>
                    <td className={`${tdClass} text-text-secondary text-xs`}>{new Date(st.last_check).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}
