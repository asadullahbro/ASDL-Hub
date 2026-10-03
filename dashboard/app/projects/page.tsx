'use client';

import { useEffect, useState } from 'react';
import { api } from '../../lib/api';
import { Project, Node, Plugin } from '../../types';
import { Pagination } from '@/components/ui/Pagination';
import { Box, Calendar, Package, Pencil, Plug, Puzzle, RefreshCw, Server, Trash2, X } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { Badge, StatusBadge } from '@/components/ui/Badge';
import { EmptyState } from '@/components/ui/Card';
import { Field, Modal, inputClass } from '@/components/ui/Modal';

// Parses .env text into env vars: KEY=value lines, ignoring blanks and
// comments, allowing "export " and quoted values.
function parseEnv(text: string): { key: string; value: string }[] {
  const vars: { key: string; value: string }[] = [];
  for (const raw of text.split('\n')) {
    const line = raw.trim().replace(/^export\s+/, '');
    if (!line || line.startsWith('#')) continue;
    const eq = line.indexOf('=');
    if (eq <= 0) continue;
    let value = line.slice(eq + 1).trim();
    if (value.length >= 2 && (value[0] === '"' || value[0] === "'") && value[value.length - 1] === value[0]) {
      value = value.slice(1, -1);
    }
    vars.push({ key: line.slice(0, eq).trim(), value });
  }
  return vars;
}

function envText(project: Project): string {
  return (project.env_vars || []).map(e => `${e.key}=${e.value}`).join('\n');
}

export default function ProjectsPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pagination, setPagination] = useState({ total: 0, pages: 0, limit: 20 });
  const limit = 20;

  // Edit state
  const [editingProject, setEditingProject] = useState<Project | null>(null);
  const [editForm, setEditForm] = useState({
    name: '',
    description: '',
    domain: '',
    route_path: '',
    node_id: '',
    image: '',
    ports: '',
    env: '',
    status: '',
  });
  const [saving, setSaving] = useState(false);

  // Plugins, to show which are attached to each project
  const [plugins, setPlugins] = useState<Plugin[]>([]);

  // Delete state
  const [deletingProject, setDeletingProject] = useState<Project | null>(null);
  const [deleting, setDeleting] = useState(false);

  async function loadData() {
    try {
      const [projectsResponse, nodesData, pluginList] = await Promise.all([
        api.getProjects(page, limit),
        api.getNodes(),
        api.getPlugins().catch(() => [] as Plugin[]),
      ]);
      setPlugins(pluginList);
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

  const openEdit = (project: Project) => {
    setEditingProject(project);
    setEditForm({
      name: project.name,
      description: project.description || '',
      domain: project.domain || '',
      route_path: project.route_path || '',
      node_id: project.node_id,
      image: project.image || '',
      ports: project.auto_port ? '' : project.ports?.join(', ') || '',
      env: envText(project),
      status: project.status,
    });
  };

  const handleSave = async () => {
    if (!editingProject) return;
    setSaving(true);
    try {
      const update: Partial<Project> = {
        name: editForm.name,
        description: editForm.description,
        domain: editForm.domain,
        route_path: editForm.route_path.trim(),
        node_id: editForm.node_id,
        image: editForm.image,
        status: editForm.status,
      };
      // Only send ports and env when edited: changing them redeploys the app.
      const originalPorts = editingProject.auto_port ? '' : editingProject.ports?.join(', ') || '';
      if (editForm.ports !== originalPorts) {
        update.ports = editForm.ports ? editForm.ports.split(',').map(p => p.trim()).filter(Boolean) : [];
      }
      if (editForm.env !== envText(editingProject)) {
        update.env_vars = parseEnv(editForm.env);
      }
      await api.updateProject(editingProject.id, update);
      setEditingProject(null);
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update project');
    } finally {
      setSaving(false);
    }
  };

  const handleRedeploy = async (project: Project) => {
    try {
      await api.redeployProject(project.id);
      setNotice(`${project.name} is redeploying.`);
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to redeploy project');
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

  const healthy = projects.filter(p => p.health_status === 'healthy').length;
  const unhealthy = projects.filter(p => p.health_status === 'unhealthy' || p.status === 'failed').length;

  if (loading) {
    return <div className="flex items-center justify-center h-96 text-sm text-text-secondary">Loading…</div>;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={Box}
        title="Projects"
        description={
          projects.length
            ? `Your apps: ${healthy} healthy${unhealthy ? `, ${unhealthy} need a look` : ''}. Deploy from GitHub to add one.`
            : 'Your apps. Deploy from GitHub to add one.'
        }
        actions={<Button variant="ghost" icon={RefreshCw} onClick={loadData}>Refresh</Button>}
      />

      {error && <div className="text-sm text-status-red">{error}</div>}
      {notice && <div className="text-sm text-status-green">{notice}</div>}

      {projects.length === 0 ? (
        <EmptyState>No projects yet. Add a repository under GitHub and push to deploy it.</EmptyState>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {projects.map(project => {
            const node = nodes.find(n => n.id === project.node_id);
            const attached = plugins.filter(pl => pl.attached_to.includes(project.name));
            return (
              <div key={project.id} className="bg-surface border border-border rounded-lg p-5 flex flex-col transition-colors hover:border-border-strong">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="font-medium text-text-primary truncate">{project.name}</div>
                    {project.domain && (
                      <a
                        href={`https://${project.domain}${project.route_path && project.route_path !== '/' ? project.route_path : ''}`}
                        target="_blank"
                        rel="noreferrer"
                        className="text-xs text-accent font-mono mt-0.5 block truncate hover:underline"
                      >
                        {project.domain}
                        {project.route_path && project.route_path !== '/' ? project.route_path : ''}
                      </a>
                    )}
                  </div>
                  <StatusBadge status={project.status === 'running' ? project.health_status : project.status} />
                </div>

                {project.description && <p className="text-xs text-text-secondary mt-3">{project.description}</p>}

                <dl className="mt-3 space-y-1.5 text-xs text-text-secondary">
                  <Row icon={Server}>{node?.hostname || 'not running'}</Row>
                  <Row icon={Package}><span className="font-mono break-all">{project.image || '—'}</span></Row>
                  {project.ports && project.ports.length > 0 && (
                    <Row icon={Plug}>
                      <span className="font-mono">{project.ports.join(', ')}</span>
                      {project.auto_port && <span className="text-text-muted"> (auto)</span>}
                    </Row>
                  )}
                  <Row icon={Calendar}>{project.last_deployed ? new Date(project.last_deployed).toLocaleString() : 'never deployed'}</Row>
                  {attached.length > 0 && (
                    <Row icon={Puzzle}>
                      <span className="flex flex-wrap gap-1">
                        {attached.map(pl => (
                          <Badge key={pl.id}>
                            {pl.name}
                            <button
                              title={`Remove ${pl.name} (redeploys ${project.name})`}
                              onClick={async () => {
                                if (!confirm(`Remove ${pl.name} from ${project.name}? It is redeployed without it.`)) return;
                                try {
                                  await api.detachPlugin(project.id, pl.id);
                                  await loadData();
                                } catch (err) {
                                  setError(err instanceof Error ? err.message : 'Failed to remove plugin');
                                }
                              }}
                              className="text-text-muted hover:text-status-red"
                            >
                              <X className="h-3 w-3" />
                            </button>
                          </Badge>
                        ))}
                      </span>
                    </Row>
                  )}
                </dl>

                <div className="mt-auto pt-4">
                  <div className="pt-3 border-t border-border flex items-center gap-1">
                    <Button variant="ghost" icon={Pencil} onClick={() => openEdit(project)}>Edit</Button>
                    <Button variant="ghost" icon={RefreshCw} onClick={() => handleRedeploy(project)}>Redeploy</Button>
                    <Button variant="ghost" icon={Trash2} className="ml-auto hover:text-status-red" onClick={() => setDeletingProject(project)}>
                      Delete
                    </Button>
                  </div>
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

      {editingProject && (
        <Modal
          title={`Edit ${editingProject.name}`}
          onClose={() => setEditingProject(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setEditingProject(null)}>Cancel</Button>
              <Button variant="primary" loading={saving} onClick={handleSave}>Save</Button>
            </>
          }
        >
          {[
            { label: 'Name', key: 'name' },
            { label: 'Description', key: 'description' },
            { label: 'Domain', key: 'domain' },
            { label: 'Path', key: 'route_path', help: 'Optional, e.g. /api/: serve the app under this path of the domain.' },
            { label: 'Image', key: 'image' },
            { label: 'Ports', key: 'ports', help: 'Leave empty to let the Hub pick, or host:container, e.g. 8080:8000.' },
          ].map(({ label, key, help }) => (
            <Field key={key} label={label} help={help}>
              <input
                value={editForm[key as keyof typeof editForm]}
                onChange={e => setEditForm(f => ({ ...f, [key]: e.target.value }))}
                className={inputClass}
              />
            </Field>
          ))}
          <Field label="Environment variables (.env format)" help="Stored encrypted and shown masked. Leave ******** to keep a value; changes redeploy the app.">
            <textarea
              rows={6}
              value={editForm.env}
              onChange={e => setEditForm(f => ({ ...f, env: e.target.value }))}
              placeholder={'DATABASE_URL=postgres://...\nSECRET_KEY=...'}
              spellCheck={false}
              className={`${inputClass} h-auto py-2 font-mono`}
            />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Node">
              <select value={editForm.node_id} onChange={e => setEditForm(f => ({ ...f, node_id: e.target.value }))} className={inputClass}>
                {nodes.map(n => (
                  <option key={n.id} value={n.id}>{n.hostname}</option>
                ))}
              </select>
            </Field>
            <Field label="Status">
              <select value={editForm.status} onChange={e => setEditForm(f => ({ ...f, status: e.target.value }))} className={inputClass}>
                {['running', 'stopped', 'failed', 'deploying'].map(st => (
                  <option key={st} value={st}>{st}</option>
                ))}
              </select>
            </Field>
          </div>
        </Modal>
      )}

      {deletingProject && (
        <Modal
          title={`Delete ${deletingProject.name}?`}
          size="sm"
          onClose={() => setDeletingProject(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setDeletingProject(null)}>Cancel</Button>
              <Button variant="danger" icon={Trash2} loading={deleting} onClick={handleDelete}>Delete project</Button>
            </>
          }
        >
          <p className="text-sm text-text-secondary">
            Its container and plugins are stopped and removed from the node, and its domain stops being served. This can&apos;t be undone.
          </p>
        </Modal>
      )}
    </div>
  );
}

function Row({ icon: Icon, children }: { icon: typeof Box; children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2">
      <Icon className="h-3.5 w-3.5 mt-0.5 flex-shrink-0 text-text-muted" />
      <span className="min-w-0">{children}</span>
    </div>
  );
}
