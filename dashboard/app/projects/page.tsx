'use client';

import { useEffect, useState } from 'react';
import { api } from '../../lib/api';
import { Project, Node } from '../../types';
import { Pagination } from '@/components/ui/Pagination';

export default function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showHealthModal, setShowHealthModal] = useState(false);
  const [projectHealth, setProjectHealth] = useState<any>(null);
  const [checkingHealth, setCheckingHealth] = useState(false);
  const [page, setPage] = useState(1);
  const [pagination, setPagination] = useState({ total: 0, pages: 0, limit: 20 });
  const limit = 20;

  // Edit state
  const [editingProject, setEditingProject] = useState<Project | null>(null);
  const [editForm, setEditForm] = useState({
    name: '',
    description: '',
    domain: '',
    node_id: '',
    image: '',
    ports: '',
    status: '',
  });
  const [saving, setSaving] = useState(false);

  // Delete state
  const [deletingProject, setDeletingProject] = useState<Project | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function loadData() {
    try {
      const [projectsResponse, nodesData] = await Promise.all([
        api.getProjects(page, limit),
        api.getNodes(),
      ]);
      setProjects(projectsResponse.data);
      setPagination(projectsResponse.pagination);
      setNodes(nodesData);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadData();
  }, [page]);

  const handlePageChange = (newPage: number) => {
    setPage(newPage);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  const getNodeName = (nodeId: string) => {
    const node = nodes.find(n => n.id === nodeId);
    return node ? node.hostname : 'Unknown';
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'running': return 'text-status-green';
      case 'stopped': return 'text-status-red';
      case 'deploying': return 'text-status-blue';
      default: return 'text-text-muted';
    }
  };

  const getHealthColor = (health: string) => {
    switch (health) {
      case 'healthy': return 'bg-status-green';
      case 'degraded': return 'bg-status-yellow';
      case 'unhealthy': return 'bg-status-red';
      default: return 'bg-text-muted';
    }
  };

  const checkProjectHealth = async (projectId: string) => {
    setCheckingHealth(true);
    try {
      const health = await api.getProjectHealth(projectId);
      setProjectHealth(health);
      setShowHealthModal(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to check health');
    } finally {
      setCheckingHealth(false);
    }
  };

  const openEdit = (project: Project) => {
    setEditingProject(project);
    setEditForm({
      name: project.name,
      description: project.description || '',
      domain: project.domain || '',
      node_id: project.node_id,
      image: project.image || '',
      ports: project.ports?.join(', ') || '',
      status: project.status,
    });
  };

  const handleSave = async () => {
    if (!editingProject) return;
    setSaving(true);
    try {
      await api.updateProject(editingProject.id, {
        name: editForm.name,
        description: editForm.description,
        domain: editForm.domain,
        node_id: editForm.node_id,
        image: editForm.image,
        ports: editForm.ports ? editForm.ports.split(',').map(p => p.trim()) : [],
        status: editForm.status,
      });
      setEditingProject(null);
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update project');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!deletingProject) return;
    setDeleting(true);
    try {
      await api.deleteProject(deletingProject.id);
      setDeletingProject(null);
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete project');
    } finally {
      setDeleting(false);
    }
  };

  const healthStats = {
    total: projects.length,
    healthy: projects.filter(p => p.health_status === 'healthy').length,
    degraded: projects.filter(p => p.health_status === 'degraded').length,
    unhealthy: projects.filter(p => p.health_status === 'unhealthy').length,
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted">Loading projects...</div>
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
        <h1 className="text-xl font-semibold text-text-primary">Projects</h1>
        <div className="flex items-center gap-4 text-sm text-text-muted">
          <span>✅ {healthStats.healthy} Healthy</span>
          <span>⚠️ {healthStats.degraded} Degraded</span>
          <span>❌ {healthStats.unhealthy} Unhealthy</span>
          <span>Total: {healthStats.total}</span>
        </div>
      </div>

      {projects.length === 0 ? (
        <div className="bg-surface border border-border rounded-lg p-12 text-center">
          <div className="text-text-muted">No projects running</div>
          <div className="text-sm text-text-secondary mt-2">
            Deploy an application to see it here
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {projects.map((project) => {
            const node = nodes.find((n) => n.id === project.node_id);
            return (
              <div
                key={project.id}
                className="bg-surface border border-border rounded-lg p-5 hover:border-text-muted transition-colors"
              >
                <div className="flex items-start justify-between">
                  <div>
                    <div className="font-medium text-text-primary">{project.name}</div>
                    {project.domain && (
                      <div className="text-xs text-accent font-mono mt-1">{project.domain}</div>
                    )}
                  </div>
                  <div className="flex flex-col items-end gap-1">
                    <div className="flex items-center gap-2">
                      <span className={`text-xs font-medium ${getStatusColor(project.status)}`}>
                        {project.status}
                      </span>
                      <div className={`h-2 w-2 rounded-full ${getHealthColor(project.health_status)}`} />
                    </div>
                    <button
                      onClick={() => checkProjectHealth(project.id)}
                      disabled={checkingHealth}
                      className="text-xs text-accent hover:text-accent-hover transition-colors"
                    >
                      Check Health
                    </button>
                  </div>
                </div>

                <div className="mt-3 space-y-1.5 text-sm text-text-secondary">
                  {project.description && (
                    <div className="text-xs text-text-muted">{project.description}</div>
                  )}
                  <div className="flex items-center gap-2">
                    <span className="text-xs">📍</span>
                    <span className="text-xs">{node?.hostname || 'Unknown'}</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-xs">📦</span>
                    <span className="text-xs font-mono">{project.image || 'N/A'}</span>
                  </div>
                  {project.ports && project.ports.length > 0 && (
                    <div className="flex items-center gap-2">
                      <span className="text-xs">🔌</span>
                      <span className="text-xs font-mono">{project.ports.join(', ')}</span>
                    </div>
                  )}
                  <div className="flex items-center gap-2">
                    <span className="text-xs">📅</span>
                    <span className="text-xs">
                      {project.last_deployed ? new Date(project.last_deployed).toLocaleString() : 'N/A'}
                    </span>
                  </div>
                </div>

                {/* Actions */}
                <div className="mt-4 pt-3 border-t border-border flex items-center gap-3">
                  <button
                    onClick={() => openEdit(project)}
                    className="text-xs text-text-muted hover:text-text-primary transition-colors"
                  >
                    Edit
                  </button>
                  <button
                    onClick={() => setDeletingProject(project)}
                    className="text-xs text-status-red hover:opacity-80 transition-opacity"
                  >
                    Delete
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      <Pagination
        currentPage={page}
        totalPages={pagination.pages}
        onPageChange={handlePageChange}
        totalItems={pagination.total}
        itemsPerPage={pagination.limit}
      />

      {/* Edit Modal */}
      {editingProject && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg w-full max-w-lg max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
              <h3 className="font-medium text-text-primary">Edit Project</h3>
              <button
                onClick={() => setEditingProject(null)}
                className="text-text-muted hover:text-text-primary transition-colors p-1"
              >
                ✕
              </button>
            </div>
            <div className="p-5 space-y-4">
              {[
                { label: 'Name', key: 'name' },
                { label: 'Description', key: 'description' },
                { label: 'Domain', key: 'domain' },
                { label: 'Image', key: 'image' },
                { label: 'Ports (comma separated)', key: 'ports' },
              ].map(({ label, key }) => (
                <div key={key}>
                  <label className="block text-xs text-text-muted mb-1">{label}</label>
                  <input
                    type="text"
                    value={editForm[key as keyof typeof editForm]}
                    onChange={e => setEditForm(f => ({ ...f, [key]: e.target.value }))}
                    className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent"
                  />
                </div>
              ))}
              <div>
                <label className="block text-xs text-text-muted mb-1">Node</label>
                <select
                  value={editForm.node_id}
                  onChange={e => setEditForm(f => ({ ...f, node_id: e.target.value }))}
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent"
                >
                  {nodes.map(n => (
                    <option key={n.id} value={n.id}>{n.hostname}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-text-muted mb-1">Status</label>
                <select
                  value={editForm.status}
                  onChange={e => setEditForm(f => ({ ...f, status: e.target.value }))}
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent"
                >
                  {['running', 'stopped', 'failed', 'deploying'].map(s => (
                    <option key={s} value={s}>{s}</option>
                  ))}
                </select>
              </div>
            </div>
            <div className="flex justify-end gap-3 px-5 py-3.5 border-t border-border">
              <button
                onClick={() => setEditingProject(null)}
                className="text-sm text-text-muted hover:text-text-primary transition-colors px-3 py-1.5"
              >
                Cancel
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {saving ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirm Modal */}
      {deletingProject && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg w-full max-w-sm">
            <div className="px-5 py-4 space-y-2">
              <h3 className="font-medium text-text-primary">Delete Project</h3>
              <p className="text-sm text-text-muted">
                Are you sure you want to delete <span className="text-text-primary font-medium">{deletingProject.name}</span>? This cannot be undone.
              </p>
            </div>
            <div className="flex justify-end gap-3 px-5 py-3.5 border-t border-border">
              <button
                onClick={() => setDeletingProject(null)}
                className="text-sm text-text-muted hover:text-text-primary transition-colors px-3 py-1.5"
              >
                Cancel
              </button>
              <button
                onClick={handleDelete}
                disabled={deleting}
                className="text-sm bg-status-red text-white px-4 py-1.5 rounded hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {deleting ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Health Modal */}
      {showHealthModal && projectHealth && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg max-w-md w-full">
            <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
              <h3 className="font-medium text-text-primary">Project Health</h3>
              <button
                onClick={() => setShowHealthModal(false)}
                className="text-text-muted hover:text-text-primary transition-colors p-1"
              >
                ✕
              </button>
            </div>
            <div className="p-5 space-y-3">
              {[
                { label: 'Project', value: projectHealth.name },
                { label: 'Status', value: projectHealth.status },
                { label: 'Node', value: getNodeName(projectHealth.node_id) },
                { label: 'Last Check', value: projectHealth.last_check ? new Date(projectHealth.last_check).toLocaleString() : 'Never' },
              ].map(({ label, value }) => (
                <div key={label}>
                  <span className="text-xs text-text-muted">{label}</span>
                  <div className="text-sm text-text-secondary">{value}</div>
                </div>
              ))}
              <div>
                <span className="text-xs text-text-muted">Health</span>
                <div className={`text-sm font-medium ${
                  projectHealth.health === 'healthy' ? 'text-status-green' :
                  projectHealth.health === 'unhealthy' ? 'text-status-red' :
                  'text-status-yellow'
                }`}>
                  {projectHealth.health || 'Unknown'}
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}