'use client';

import { useEffect, useState } from 'react';
import { api } from '@/lib/api';
import { Project, Node } from '@/types';

interface HealthStatus {
  project_id: string;
  name: string;
  status: string;
  health: string;
  node_id: string;
  last_check: string;
}

export default function HealthPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [healthStatuses, setHealthStatuses] = useState<HealthStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [checkingAll, setCheckingAll] = useState(false);

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

  const getNodeName = (nodeId: string) => {
    const node = nodes.find(n => n.id === nodeId);
    return node ? node.hostname : 'Unknown';
  };

  const getHealthBadge = (health: string) => {
    switch (health) {
      case 'healthy': return 'badge-completed';
      case 'degraded': return 'badge-pending';
      case 'unhealthy': return 'badge-failed';
      default: return 'badge-pending';
    }
  };

  const healthStats = {
    total: healthStatuses.length,
    healthy: healthStatuses.filter(h => h.health === 'healthy').length,
    degraded: healthStatuses.filter(h => h.health === 'degraded').length,
    unhealthy: healthStatuses.filter(h => h.health === 'unhealthy').length,
    unknown: healthStatuses.filter(h => h.health === 'unknown').length,
  };

  const runHealthCheckAll = async () => {
    setCheckingAll(true);
    try {
      for (const project of projects) {
        await api.getProjectHealth(project.id);
      }
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Health check failed');
    } finally {
      setCheckingAll(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted">Loading health status...</div>
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

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-text-primary">Health Status</h1>
        <button
          onClick={runHealthCheckAll}
          disabled={checkingAll}
          className="px-4 py-2 bg-accent text-black rounded-md text-sm font-medium hover:bg-accent-hover transition-colors disabled:opacity-50"
        >
          {checkingAll ? 'Checking...' : '🔄 Check All Health'}
        </button>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-status-green">{healthStats.healthy}</div>
          <div className="text-xs text-text-muted">Healthy</div>
        </div>
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-status-yellow">{healthStats.degraded}</div>
          <div className="text-xs text-text-muted">Degraded</div>
        </div>
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-status-red">{healthStats.unhealthy}</div>
          <div className="text-xs text-text-muted">Unhealthy</div>
        </div>
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-text-muted">{healthStats.unknown}</div>
          <div className="text-xs text-text-muted">Unknown</div>
        </div>
      </div>

      {healthStatuses.length === 0 ? (
        <div className="bg-surface border border-border rounded-lg p-12 text-center">
          <div className="text-text-muted">No projects to monitor</div>
          <div className="text-sm text-text-secondary mt-2">
            Deploy a project to see health status
          </div>
        </div>
      ) : (
        <div className="bg-surface border border-border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border">
                <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                  Project
                </th>
                <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                  Status
                </th>
                <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                  Health
                </th>
                <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                  Node
                </th>
                <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                  Last Check
                </th>
                <th className="text-right py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                  Action
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {healthStatuses.map((status) => (
                <tr key={status.project_id} className="hover:bg-surface-hover transition-colors">
                  <td className="py-3 px-4 text-text-primary font-medium">
                    {status.name}
                  </td>
                  <td className="py-3 px-4">
                    <span className={`badge ${status.status === 'running' ? 'badge-completed' : 'badge-failed'}`}>
                      {status.status}
                    </span>
                  </td>
                  <td className="py-3 px-4">
                    <span className={`badge ${getHealthBadge(status.health)}`}>
                      {status.health}
                    </span>
                  </td>
                  <td className="py-3 px-4 text-text-secondary">
                    {getNodeName(status.node_id)}
                  </td>
                  <td className="py-3 px-4 text-text-muted text-xs">
                    {new Date(status.last_check).toLocaleString()}
                  </td>
                  <td className="py-3 px-4 text-right">
                    <button
                      onClick={() => {
                        const project = projects.find(p => p.id === status.project_id);
                        if (project) {
                          api.getProjectHealth(project.id).then(() => loadData());
                        }
                      }}
                      className="text-xs text-accent hover:text-accent-hover transition-colors"
                    >
                      Check Now
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}