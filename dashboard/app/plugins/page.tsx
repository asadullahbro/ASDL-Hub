'use client';

import { useCallback, useEffect, useState } from 'react';
import { Puzzle, Plus, Trash2, X } from 'lucide-react';
import { NotificationPlugins } from '@/components/plugins/NotificationPlugins';
import { api } from '@/lib/api';
import type { Plugin, Project } from '@/types';
import { useAuth } from '@/components/providers/AuthProvider';

const NOTIFY_EXAMPLE = `{
  "id": "google-chat",
  "name": "Google Chat",
  "description": "Posts to a Google Chat space through an incoming webhook.",
  "fields": [
    { "key": "url", "label": "Webhook URL", "secret": true, "required": true }
  ],
  "request": {
    "url": "{{.config.url}}",
    "body": "{\\"text\\": {{json .text}}}"
  }
}`;

const EXAMPLE = `{
  "id": "memcached",
  "name": "Memcached",
  "description": "A small in-memory cache next to the app.",
  "image": "memcached:1.6-alpine",
  "port": 11211,
  "provides": { "MEMCACHED_URL": "{{host}}:{{port}}" }
}`;

export default function PluginsPage() {
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';
  const canAttach = isAdmin || user?.role === 'operator';
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [manifest, setManifest] = useState(EXAMPLE);
  // Which tab is shown, and which kind of plugin the custom form adds.
  const [tab, setTab] = useState<'apps' | 'notifications'>('apps');
  const [kind, setKind] = useState<'app' | 'notification'>('app');
  const [notifyReload, setNotifyReload] = useState(0);
  const [attachTo, setAttachTo] = useState<Record<string, string>>({});
  // The attach form, for plugins that need settings or a domain.
  const [form, setForm] = useState<{ plugin: Plugin; project: Project; vars: Record<string, string>; domain: string; path: string } | null>(null);

  const needsForm = (p: Plugin) => !!p.public || (p.vars ?? []).some(v => !v.generate && !v.default);

  const attach = (p: Plugin, pr: Project, vars: Record<string, string> = {}, domain = '', path = '') =>
    run(
      () => api.attachPlugin(pr.id, p.id, Object.entries(vars).filter(([, v]) => v !== '').map(([key, value]) => ({ key, value })), p.public ? { domain, route_path: path } : {}),
      `${p.name} added to ${pr.name}; it's redeploying.`,
    ).then(() => setForm(null));

  const load = useCallback(async () => {
    try {
      const [p, pr] = await Promise.all([api.getPlugins(), api.getProjects(1, 100).catch(() => ({ data: [] as Project[] }))]);
      setPlugins(p);
      setProjects(pr.data ?? []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load plugins');
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // Links (e.g. from a test notification) open a tab: /plugins?tab=notifications.
  useEffect(() => {
    if (new URLSearchParams(window.location.search).get('tab') === 'notifications') setTab('notifications');
  }, []);

  const showTab = (t: 'apps' | 'notifications') => {
    setTab(t);
    setError(null);
    setNotice(null);
    window.history.replaceState(null, '', t === 'apps' ? '/plugins' : '/plugins?tab=notifications');
  };

  const openCustom = () => {
    const k = tab === 'notifications' ? 'notification' : 'app';
    setKind(k);
    setManifest(k === 'app' ? EXAMPLE : NOTIFY_EXAMPLE);
    setAdding(true);
  };

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    setError(null);
    setNotice(null);
    try {
      await fn();
      setNotice(ok);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong');
    }
  };

  const addCustom = () => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(manifest);
    } catch {
      setError('That is not valid JSON.');
      return;
    }
    if (kind === 'notification') {
      run(() => api.addNotificationPlugin(parsed), 'Notification plugin added; add it as a channel below.').then(() => {
        setAdding(false);
        setNotifyReload(n => n + 1);
      });
      return;
    }
    run(() => api.addCustomPlugin(parsed), 'Plugin added.').then(() => setAdding(false));
  };

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold text-text-primary flex items-center gap-2">
            <Puzzle className="h-5 w-5" /> Plugins
          </h1>
        </div>
        {isAdmin && (
          <button
            onClick={openCustom}
            className="flex-shrink-0 flex items-center gap-1.5 text-xs font-medium bg-accent text-background px-3 py-1.5 rounded hover:opacity-90"
          >
            <Plus className="h-3.5 w-3.5" /> Custom plugin
          </button>
        )}
      </div>

      {isAdmin && (
        <div className="flex gap-1 border-b border-border">
          {([['apps', 'App plugins'], ['notifications', 'Notifications']] as const).map(([id, label]) => (
            <button
              key={id}
              onClick={() => showTab(id)}
              className={`px-3 py-2 text-sm -mb-px border-b-2 ${tab === id ? 'border-accent text-text-primary' : 'border-transparent text-text-muted hover:text-text-primary'}`}
            >
              {label}
            </button>
          ))}
        </div>
      )}

      {error && <div className="text-sm text-status-red">{error}</div>}
      {notice && <div className="text-sm text-status-green">{notice}</div>}

      {tab === 'notifications' && isAdmin ? (
        <NotificationPlugins reloadKey={notifyReload} />
      ) : (
      <>
      <p className="text-sm text-text-muted max-w-2xl">
        An app plugin is a companion service attached to a project, like a cache or a search engine. It runs on the same
        node as the project, in a private network only they share, and moves with it on deploys, moves, failovers and
        maintenance. The project gets its address as an environment variable.
      </p>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {plugins.map(p => {
          const available = projects.filter(pr => !p.attached_to.includes(pr.name));
          return (
            <div key={p.id} className="bg-surface border border-border rounded-lg p-5 flex flex-col gap-3">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <div className="text-sm font-semibold text-text-primary flex items-center gap-2">
                    {p.name}
                    <span className="px-1.5 py-0.5 rounded text-[10px] bg-surface-hover text-text-muted">
                      {p.builtin ? 'built-in' : 'custom'}
                    </span>
                  </div>
                  <p className="text-xs text-text-muted mt-1">{p.description}</p>
                </div>
                {isAdmin && !p.builtin && (
                  <button
                    title="Remove this plugin"
                    onClick={() => confirm(`Remove the ${p.name} plugin?`) && run(() => api.removeCustomPlugin(p.id), 'Plugin removed.')}
                    className="text-text-muted hover:text-status-red"
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                )}
              </div>
              <dl className="text-xs grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
                <dt className="text-text-muted">Runs</dt>
                <dd className="font-mono text-text-secondary break-all">{p.image}</dd>
                <dt className="text-text-muted">Gives the app</dt>
                <dd className="font-mono text-text-secondary">{Object.keys(p.provides ?? {}).join(', ') || '—'}</dd>
                {p.public && (
                  <>
                    <dt className="text-text-muted">Public</dt>
                    <dd className="text-text-secondary">served by the Hub at a domain and path you choose</dd>
                  </>
                )}
                <dt className="text-text-muted">Used by</dt>
                <dd className="text-text-secondary">{p.attached_to.length ? p.attached_to.join(', ') : 'no projects yet'}</dd>
              </dl>
              {canAttach && available.length > 0 && (
                <div className="flex items-center gap-2 pt-1">
                  <select
                    value={attachTo[p.id] ?? ''}
                    onChange={e => setAttachTo(a => ({ ...a, [p.id]: e.target.value }))}
                    className="flex-1 bg-background border border-border rounded px-2 py-1.5 text-xs text-text-primary"
                  >
                    <option value="">Add to a project…</option>
                    {available.map(pr => (
                      <option key={pr.id} value={pr.id}>{pr.name}</option>
                    ))}
                  </select>
                  <button
                    disabled={!attachTo[p.id]}
                    onClick={() => {
                      const pr = projects.find(x => x.id === attachTo[p.id]);
                      if (!pr) return;
                      if (needsForm(p)) {
                        setForm({ plugin: p, project: pr, vars: Object.fromEntries((p.vars ?? []).map(v => [v.key, v.default ?? ''])), domain: '', path: '' });
                        return;
                      }
                      if (!confirm(`Add ${p.name} to ${pr.name}? ${pr.name} is redeployed with it.`)) return;
                      attach(p, pr);
                    }}
                    className="text-xs font-medium border border-border text-text-primary px-3 py-1.5 rounded hover:bg-surface-hover disabled:opacity-40"
                  >
                    Add
                  </button>
                </div>
              )}
            </div>
          );
        })}
      </div>

      </>
      )}

      {form && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg w-full max-w-lg">
            <div className="flex items-center justify-between px-5 py-4 border-b border-border">
              <h2 className="font-medium text-text-primary">Add {form.plugin.name} to {form.project.name}</h2>
              <button onClick={() => setForm(null)} className="text-text-muted hover:text-text-primary">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="p-5 space-y-3">
              {(form.plugin.vars ?? []).map(v => (
                <div key={v.key}>
                  <label className="block text-xs text-text-muted mb-1">
                    {v.label}
                    {v.generate ? ' (leave empty to generate)' : v.required ? '' : ' (optional)'}
                  </label>
                  <input
                    type={v.secret ? 'password' : 'text'}
                    value={form.vars[v.key] ?? ''}
                    onChange={e => setForm(f => f && { ...f, vars: { ...f.vars, [v.key]: e.target.value } })}
                    className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:outline-none focus:border-accent"
                  />
                </div>
              ))}
              {form.plugin.public && (
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-xs text-text-muted mb-1">Domain</label>
                    <input
                      value={form.domain}
                      placeholder="db.example.com"
                      onChange={e => setForm(f => f && { ...f, domain: e.target.value })}
                      className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:outline-none focus:border-accent"
                    />
                  </div>
                  <div>
                    <label className="block text-xs text-text-muted mb-1">Path (optional)</label>
                    <input
                      value={form.path}
                      placeholder="/rest/v1/"
                      onChange={e => setForm(f => f && { ...f, path: e.target.value })}
                      className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:outline-none focus:border-accent"
                    />
                  </div>
                </div>
              )}
              <p className="text-xs text-text-muted">{form.project.name} is redeployed with the plugin. Secret values are stored encrypted.</p>
            </div>
            <div className="flex justify-end gap-3 px-5 py-3.5 border-t border-border">
              <button onClick={() => setForm(null)} className="text-sm text-text-muted hover:text-text-primary px-3 py-1.5">Cancel</button>
              <button
                onClick={() => attach(form.plugin, form.project, form.vars, form.domain.trim(), form.path.trim())}
                className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90"
              >
                Add plugin
              </button>
            </div>
          </div>
        </div>
      )}

      {adding && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg w-full max-w-2xl">
            <div className="flex items-center justify-between px-5 py-4 border-b border-border">
              <h2 className="font-medium text-text-primary">Add a custom {kind === 'app' ? 'app' : 'notification'} plugin</h2>
              <button onClick={() => setAdding(false)} className="text-text-muted hover:text-text-primary">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="p-5 space-y-3">
              <div className="flex gap-1.5">
                {(['app', 'notification'] as const).map(k => (
                  <button
                    key={k}
                    onClick={() => {
                      setKind(k);
                      setManifest(k === 'app' ? EXAMPLE : NOTIFY_EXAMPLE);
                    }}
                    className={`px-2.5 py-1 rounded text-xs border ${kind === k ? 'border-accent text-text-primary' : 'border-border text-text-muted'}`}
                  >
                    {k === 'app' ? 'App plugin (a container)' : 'Notification plugin (an HTTP request)'}
                  </button>
                ))}
              </div>
              {kind === 'notification' ? (
                <p className="text-xs text-text-muted">
                  Describe the service&apos;s settings in <code>fields</code> and the request to send in <code>request</code>{' '}
                  (<code>method</code>, <code>url</code>, <code>headers</code>, <code>body</code>). They are Go templates:{' '}
                  <code>{'{{.title}}'}</code>, <code>{'{{.message}}'}</code>, <code>{'{{.text}}'}</code> (everything as plain
                  text), <code>{'{{.url}}'}</code>, <code>{'{{.level}}'}</code>, <code>{'{{.emoji}}'}</code>,{' '}
                  <code>{'{{.color}}'}</code>, <code>{'{{.event}}'}</code>, <code>{'{{.project}}'}</code>,{' '}
                  <code>{'{{.node}}'}</code> and <code>{'{{.config.KEY}}'}</code> for a field. Wrap text in{' '}
                  <code>{'{{json …}}'}</code> inside a JSON body so it is quoted and escaped. Use the same id again to
                  replace a plugin.
                </p>
              ) : (
              <p className="text-xs text-text-muted">
                Describe the container in JSON. In <code>provides</code>, <code>command</code>, <code>env</code> and{' '}
                <code>files</code> you can use <code>{'{{host}}'}</code> (the plugin&apos;s name in the project&apos;s network),{' '}
                <code>{'{{port}}'}</code> and <code>{'{{var.KEY}}'}</code> for settings declared in <code>vars</code> (e.g.{' '}
                <code>{'{"key": "password", "label": "Password", "secret": true, "generate": 32}'}</code>). The Hub only stores this
                description; the image is downloaded only on nodes that run it.
              </p>
              )}
              <textarea
                rows={14}
                value={manifest}
                onChange={e => setManifest(e.target.value)}
                spellCheck={false}
                className="w-full bg-background border border-border rounded px-3 py-2 text-xs font-mono text-text-primary focus:outline-none focus:border-accent"
              />
            </div>
            <div className="flex justify-end gap-3 px-5 py-3.5 border-t border-border">
              <button onClick={() => setAdding(false)} className="text-sm text-text-muted hover:text-text-primary px-3 py-1.5">
                Cancel
              </button>
              <button onClick={addCustom} className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90">
                Add plugin
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
