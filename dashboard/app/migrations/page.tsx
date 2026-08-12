'use client';

import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { Migration, Node, Project } from '@/types';
import { Pagination } from '@/components/ui/Pagination';

export default function MigrationsPage() {
  const [migrations, setMigrations] = useState<Migration[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedMigration, setSelectedMigration] = useState<Migration | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [selectedProject, setSelectedProject] = useState('');
  const [selectedTargetNode, setSelectedTargetNode] = useState('');
  const [migrating, setMigrating] = useState(false);
  const [page, setPage] = useState(1);
  const [pagination, setPagination] = useState({ total: 0, pages: 0, limit: 20 });
  const limit = 20;

  const loadData = useCallback(async () => {
    try {
      setLoading(true);
      
      const [migrationsResponse, nodesData, projectsResponse] = await Promise.all([
        api.getMigrations(page, limit),
        api.getNodes(),
        api.getProjects(1, 100),
      ]);

      // Your API returns PaginatedResponse<Migration> for getMigrations
      setMigrations(migrationsResponse.data || []);
      setPagination(migrationsResponse.pagination || { total: 0, pages: 0, limit });
      
      // Nodes returns array directly
      setNodes(Array.isArray(nodesData) ? nodesData : []);
      
      // Projects returns PaginatedResponse
      setProjects(projectsResponse.data || []);
      
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data');
      console.error('Failed to load migrations:', err);
    } finally {
      setLoading(false);
    }
  }, [page, limit]);

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 10000);
    return () => clearInterval(interval);
  }, [loadData]);

  const handlePageChange = (newPage: number) => {
    setPage(newPage);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const getNodeName = (nodeId: string) => {
    const node = nodes.find(n => n.id === nodeId);
    return node ? node.hostname : 'Unknown';
  };

  const getProjectName = (projectId: string) => {
    const project = projects.find(p => p.id === projectId);
    return project ? project.name : 'Unknown';
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'completed': return 'text-status-green';
      case 'running': return 'text-status-blue';
      case 'pending': return 'text-status-yellow';
      case 'failed': return 'text-status-red';
      default: return 'text-text-muted';
    }
  };

  const getStatusBadge = (status: string) => {
    const colors = {
      completed: 'badge-completed',
      running: 'badge-running',
      pending: 'badge-pending',
      failed: 'badge-failed',
    };
    return colors[status as keyof typeof colors] || 'badge-pending';
  };

  const handleTriggerMigration = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedProject || !selectedTargetNode) {
      setError('Please select a project and target node');
      return;
    }

    setMigrating(true);
    setError(null);

    try {
      await api.triggerMigration(selectedProject, selectedTargetNode);
      setShowCreateModal(false);
      setSelectedProject('');
      setSelectedTargetNode('');
      setPage(1); // Reset to first page to see the new migration
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Migration failed');
    } finally {
      setMigrating(false);
    }
  };

  // Use pagination.total for the actual total count, not just current page items
  const statusCounts = {
    total: pagination.total,
    pending: migrations?.filter(m => m.status === 'pending')?.length || 0,
    running: migrations?.filter(m => m.status === 'running')?.length || 0,
    completed: migrations?.filter(m => m.status === 'completed')?.length || 0,
    failed: migrations?.filter(m => m.status === 'failed')?.length || 0,
  };

  if (loading && migrations.length === 0) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted">Loading migrations...</div>
      </div>
    );
  }

  if (error && migrations.length === 0) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-center">
          <div className="text-status-red mb-2">{error}</div>
          <button
            onClick={loadData}
            className="text-sm text-accent hover:text-accent-hover transition-colors"
          >
            Try Again
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Migrations</h1>
          <p className="text-sm text-text-muted mt-1">
            Showing page {page} of {pagination.pages || 1} • {pagination.total} total migrations
          </p>
        </div>
        <div className="flex items-center gap-4">
          <button
            onClick={() => setShowCreateModal(true)}
            className="px-4 py-2 bg-accent text-black rounded-md text-sm font-medium hover:bg-accent-hover transition-colors"
          >
            + New Migration
          </button>
          <div className="text-sm text-text-muted">
            {statusCounts.total} total
          </div>
        </div>
      </div>

      {/* Stats - Note: These show counts for CURRENT PAGE items */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-text-primary">{statusCounts.total}</div>
          <div className="text-xs text-text-muted">Total (All Pages)</div>
        </div>
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-status-yellow">{statusCounts.pending}</div>
          <div className="text-xs text-text-muted">Pending (Page {page})</div>
        </div>
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-status-blue">{statusCounts.running}</div>
          <div className="text-xs text-text-muted">Running (Page {page})</div>
        </div>
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-status-green">{statusCounts.completed}</div>
          <div className="text-xs text-text-muted">Completed (Page {page})</div>
        </div>
        <div className="stat-card text-center">
          <div className="text-xl font-semibold text-status-red">{statusCounts.failed}</div>
          <div className="text-xs text-text-muted">Failed (Page {page})</div>
        </div>
      </div>

      {migrations.length === 0 ? (
        <div className="bg-surface border border-border rounded-lg p-12 text-center">
          <div className="text-text-muted">No migrations found on this page</div>
          <div className="text-sm text-text-secondary mt-2">
            {page > 1 ? (
              <button
                onClick={() => setPage(1)}
                className="text-accent hover:text-accent-hover transition-colors"
              >
                Go to first page
              </button>
            ) : (
              'Migrate a project to move it between nodes'
            )}
          </div>
        </div>
      ) : (
        <>
          <div className="bg-surface border border-border rounded-lg overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                    ID
                  </th>
                  <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                    Project
                  </th>
                  <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                    Status
                  </th>
                  <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                    From
                  </th>
                  <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                    To
                  </th>
                  <th className="text-left py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                    Duration
                  </th>
                  <th className="text-right py-3 px-4 text-xs font-medium text-text-muted uppercase tracking-wider">
                    Action
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {migrations.map((migration) => {
                  const duration = migration.completed_at && migration.created_at
                    ? Math.round((new Date(migration.completed_at).getTime() - new Date(migration.created_at).getTime()) / 1000)
                    : null;

                  return (
                    <tr key={migration.id} className="hover:bg-surface-hover transition-colors">
                      <td className="py-3 px-4 font-mono text-xs text-text-secondary">
                        {migration.id.slice(0, 8)}
                      </td>
                      <td className="py-3 px-4 text-text-secondary">
                        {getProjectName(migration.project_id)}
                      </td>
                      <td className="py-3 px-4">
                        <span className={`badge ${getStatusBadge(migration.status)}`}>
                          {migration.status}
                        </span>
                      </td>
                      <td className="py-3 px-4 text-text-secondary">
                        {getNodeName(migration.source_node_id)}
                      </td>
                      <td className="py-3 px-4 text-text-secondary">
                        {getNodeName(migration.target_node_id)}
                      </td>
                      <td className="py-3 px-4 text-text-secondary">
                        {duration !== null ? `${duration}s` : '-'}
                      </td>
                      <td className="py-3 px-4 text-right">
                        <button
                          onClick={() => setSelectedMigration(migration)}
                          className="text-xs text-accent hover:text-accent-hover transition-colors"
                        >
                          Details
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          {/* Pagination Controls */}
          <div className="flex items-center justify-between">
            <div className="text-sm text-text-muted">
              Showing {((page - 1) * limit) + 1}-{Math.min(page * limit, pagination.total)} of {pagination.total}
            </div>
            <Pagination
              currentPage={page}
              totalPages={pagination.pages}
              onPageChange={handlePageChange}
              totalItems={pagination.total}
              itemsPerPage={pagination.limit}
            />
          </div>
        </>
      )}

      {/* Create Migration Modal */}
      {showCreateModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg max-w-md w-full max-h-[90vh] overflow-hidden">
            <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
              <h3 className="font-medium text-text-primary">Trigger Migration</h3>
              <button
                onClick={() => setShowCreateModal(false)}
                className="text-text-muted hover:text-text-primary transition-colors p-1"
              >
                ✕
              </button>
            </div>
            <form onSubmit={handleTriggerMigration} className="p-5 space-y-4">
              <div>
                <label className="block text-sm text-text-secondary mb-1">
                  Project to Migrate
                </label>
                <select
                  value={selectedProject}
                  onChange={(e) => setSelectedProject(e.target.value)}
                  className="w-full px-3 py-2 bg-background border border-border rounded-md text-text-primary text-sm focus:outline-none focus:ring-2 focus:ring-accent/50"
                  required
                >
                  <option value="">Select a project...</option>
                  {projects.map((project) => (
                    <option key={project.id} value={project.id}>
                      {project.name}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-sm text-text-secondary mb-1">
                  Target Node
                </label>
                <select
                  value={selectedTargetNode}
                  onChange={(e) => setSelectedTargetNode(e.target.value)}
                  className="w-full px-3 py-2 bg-background border border-border rounded-md text-text-primary text-sm focus:outline-none focus:ring-2 focus:ring-accent/50"
                  required
                >
                  <option value="">Select a node...</option>
                  {nodes.filter(n => n.online).map((node) => (
                    <option key={node.id} value={node.id}>
                      {node.hostname} ({node.vpn_ip})
                    </option>
                  ))}
                </select>
              </div>
              {error && (
                <div className="text-status-red text-sm">{error}</div>
              )}
              <div className="flex justify-end gap-3 pt-2">
                <button
                  type="button"
                  onClick={() => setShowCreateModal(false)}
                  className="px-4 py-2 text-sm text-text-secondary hover:text-text-primary transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={migrating}
                  className="px-4 py-2 bg-accent text-black rounded-md text-sm font-medium hover:bg-accent-hover transition-colors disabled:opacity-50"
                >
                  {migrating ? 'Migrating...' : 'Start Migration'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Migration Details Modal */}
      {selectedMigration && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg max-w-2xl w-full max-h-[90vh] overflow-hidden">
            <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
              <h3 className="font-medium text-text-primary">Migration Details</h3>
              <button
                onClick={() => setSelectedMigration(null)}
                className="text-text-muted hover:text-text-primary transition-colors p-1"
              >
                ✕
              </button>
            </div>
            <div className="p-5 overflow-y-auto max-h-[calc(90vh-70px)]">
              <div className="space-y-3 text-sm">
                <div>
                  <span className="text-text-muted">ID</span>
                  <div className="font-mono text-text-secondary">{selectedMigration.id}</div>
                </div>
                <div>
                  <span className="text-text-muted">Project</span>
                  <div className="text-text-secondary">{getProjectName(selectedMigration.project_id)}</div>
                </div>
                <div>
                  <span className="text-text-muted">Status</span>
                  <div className={`font-medium ${getStatusColor(selectedMigration.status)}`}>
                    {selectedMigration.status}
                  </div>
                </div>
                <div>
                  <span className="text-text-muted">From</span>
                  <div className="text-text-secondary">{getNodeName(selectedMigration.source_node_id)}</div>
                </div>
                <div>
                  <span className="text-text-muted">To</span>
                  <div className="text-text-secondary">{getNodeName(selectedMigration.target_node_id)}</div>
                </div>
                <div>
                  <span className="text-text-muted">Created</span>
                  <div className="text-text-secondary">
                    {new Date(selectedMigration.created_at).toLocaleString()}
                  </div>
                </div>
                {selectedMigration.completed_at && (
                  <div>
                    <span className="text-text-muted">Completed</span>
                    <div className="text-text-secondary">
                      {new Date(selectedMigration.completed_at).toLocaleString()}
                    </div>
                  </div>
                )}
                {selectedMigration.logs && (
                  <div>
                    <span className="text-text-muted">Logs</span>
                    <pre className="mt-1 font-mono text-xs text-text-secondary bg-background p-3 rounded border border-border overflow-x-auto max-h-48 overflow-y-auto whitespace-pre-wrap">
                      {selectedMigration.logs}
                    </pre>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}