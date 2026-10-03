'use client';

import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { Migration, Node, Project } from '@/types';
import { Pagination } from '@/components/ui/Pagination';
import { ArrowLeftRight, ArrowRight, Plus } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { JobStatusBadge } from '@/components/ui/Badge';
import { Card, EmptyState } from '@/components/ui/Card';
import { Field, Modal, inputClass, tdClass, thClass } from '@/components/ui/Modal';

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

  const getNodeName = (nodeId: string) => nodes.find(n => n.id === nodeId)?.hostname ?? 'a removed node';
  const getProjectName = (projectId: string) => projects.find(p => p.id === projectId)?.name;

  const handleTriggerMigration = async () => {
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

  if (loading && migrations.length === 0) {
    return <div className="flex items-center justify-center h-96 text-sm text-text-secondary">Loading…</div>;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={ArrowLeftRight}
        title="Migrations"
        description={`Apps moving between nodes: failovers, maintenance and moves you start. ${pagination.total} in total.`}
        actions={
          <Button variant="primary" icon={Plus} onClick={() => setShowCreateModal(true)}>
            Move an app
          </Button>
        }
      />

      {error && !showCreateModal && <div className="text-sm text-status-red">{error}</div>}

      <Card flush>
        {migrations.length === 0 ? (
          <EmptyState>No migrations yet. Apps move here when a node goes down, enters maintenance or you move them.</EmptyState>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th className={thClass}>App</th>
                  <th className={thClass}>Status</th>
                  <th className={thClass}>Move</th>
                  <th className={thClass}>Took</th>
                  <th className={thClass}>Started</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {migrations.map(m => {
                  const took =
                    m.completed_at && m.created_at
                      ? Math.round((new Date(m.completed_at).getTime() - new Date(m.created_at).getTime()) / 1000)
                      : null;
                  const name = getProjectName(m.project_id);
                  return (
                    <tr key={m.id} className="hover:bg-surface-hover transition-colors cursor-pointer" onClick={() => setSelectedMigration(m)}>
                      <td className={`${tdClass} ${name ? 'text-text-primary' : 'text-text-muted italic'}`}>{name ?? 'deleted app'}</td>
                      <td className={tdClass}><JobStatusBadge status={m.status} /></td>
                      <td className={`${tdClass} text-text-secondary whitespace-nowrap`}>
                        <span className="inline-flex items-center gap-1.5">
                          {getNodeName(m.source_node_id)} <ArrowRight className="h-3 w-3 text-text-muted" /> {getNodeName(m.target_node_id)}
                        </span>
                      </td>
                      <td className={`${tdClass} text-text-secondary`}>{took !== null ? `${took}s` : '—'}</td>
                      <td className={`${tdClass} text-text-secondary text-xs whitespace-nowrap`}>
                        {new Date(m.created_at).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Pagination
        currentPage={page}
        totalPages={pagination.pages}
        onPageChange={handlePageChange}
        totalItems={pagination.total}
        itemsPerPage={pagination.limit}
      />

      {showCreateModal && (
        <Modal
          title="Move an app"
          onClose={() => setShowCreateModal(false)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setShowCreateModal(false)}>Cancel</Button>
              <Button variant="primary" loading={migrating} onClick={handleTriggerMigration}>Move</Button>
            </>
          }
        >
          <p className="text-xs text-text-secondary">The new copy starts on the target node before the old one is removed, so the app stays up.</p>
          <Field label="App">
            <select value={selectedProject} onChange={e => setSelectedProject(e.target.value)} className={inputClass}>
              <option value="">Choose an app</option>
              {projects.map(p => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
          </Field>
          <Field label="Move to">
            <select value={selectedTargetNode} onChange={e => setSelectedTargetNode(e.target.value)} className={inputClass}>
              <option value="">Choose a node</option>
              {nodes.filter(n => n.online && !n.maintenance).map(n => (
                <option key={n.id} value={n.id}>{n.hostname} ({n.vpn_ip})</option>
              ))}
            </select>
          </Field>
          {error && <div className="text-sm text-status-red">{error}</div>}
        </Modal>
      )}

      {selectedMigration && (
        <Modal title="Migration" size="lg" onClose={() => setSelectedMigration(null)}>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm">
            {[
              ['App', getProjectName(selectedMigration.project_id) ?? 'deleted app'],
              ['Status', <JobStatusBadge key="s" status={selectedMigration.status} />],
              ['From', getNodeName(selectedMigration.source_node_id)],
              ['To', getNodeName(selectedMigration.target_node_id)],
              ['Started', new Date(selectedMigration.created_at).toLocaleString()],
              ['Finished', selectedMigration.completed_at ? new Date(selectedMigration.completed_at).toLocaleString() : '—'],
            ].map(([label, value]) => (
              <div key={String(label)}>
                <dt className="text-xs text-text-secondary">{label}</dt>
                <dd className="text-text-primary mt-0.5">{value}</dd>
              </div>
            ))}
          </dl>
          {selectedMigration.logs && (
            <pre className="font-mono text-xs text-text-secondary bg-background p-3 rounded-md border border-border overflow-auto max-h-64 whitespace-pre-wrap">
              {selectedMigration.logs}
            </pre>
          )}
        </Modal>
      )}
    </div>
  );
}
