'use client';

import { useCallback, useEffect, useState } from 'react';
import { Puzzle, Plus, Trash2 } from 'lucide-react';
import { NotificationPlugins } from '@/components/plugins/NotificationPlugins';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/Badge';
import { Field, Modal, inputClass } from '@/components/ui/Modal';
import { Choice, Tabs } from '@/components/ui/Tabs';
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
    <div className="space-y-6">
      <PageHeader
        icon={Puzzle}
        title="Plugins"
        description={
          tab === 'notifications'
            ? 'Get told about failed deploys, apps going down and nodes going offline, on Discord, Slack, Telegram, ntfy, email or a webhook.'
            : 'Companion services attached to a project, like a cache or a search engine. They run next to it in a private network and move with it between nodes.'
        }
        actions={isAdmin && <Button variant="primary" icon={Plus} onClick={openCustom}>Custom plugin</Button>}
      />

      {isAdmin && (
        <Tabs tabs={[['apps', 'App plugins'], ['notifications', 'Notifications']] as const} value={tab} onChange={showTab} />
      )}

      {error && <div className="text-sm text-status-red">{error}</div>}
      {notice && <div className="text-sm text-status-green">{notice}</div>}

      {tab === 'notifications' && isAdmin ? (
        <NotificationPlugins reloadKey={notifyReload} />
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {plugins.map(p => {
            const available = projects.filter(pr => !p.attached_to.includes(pr.name));
            return (
              <div key={p.id} className="bg-surface border border-border rounded-lg p-5 flex flex-col gap-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-sm font-semibold text-text-primary flex items-center gap-2">
                      {p.name}
                      <Badge>{p.builtin ? 'built-in' : 'custom'}</Badge>
                      {p.public && <Badge tone="info">public</Badge>}
                    </div>
                    <p className="text-xs text-text-secondary mt-1">{p.description}</p>
                  </div>
                  {isAdmin && !p.builtin && (
                    <Button
                      variant="ghost"
                      icon={Trash2}
                      aria-label="Remove this plugin"
                      className="hover:text-status-red"
                      onClick={() => confirm(`Remove the ${p.name} plugin?`) && run(() => api.removeCustomPlugin(p.id), 'Plugin removed.')}
                    />
                  )}
                </div>
                <dl className="text-xs grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5">
                  <dt className="text-text-secondary">Runs</dt>
                  <dd className="font-mono text-text-primary break-all">{p.image}</dd>
                  <dt className="text-text-secondary">Gives the app</dt>
                  <dd className="font-mono text-text-primary">{Object.keys(p.provides ?? {}).join(', ') || '—'}</dd>
                  <dt className="text-text-secondary">Used by</dt>
                  <dd className="text-text-primary">{p.attached_to.length ? p.attached_to.join(', ') : 'no projects yet'}</dd>
                </dl>
                {canAttach && available.length > 0 && (
                  <div className="flex items-center gap-2 mt-auto">
                    <select
                      value={attachTo[p.id] ?? ''}
                      onChange={e => setAttachTo(a => ({ ...a, [p.id]: e.target.value }))}
                      className={inputClass}
                    >
                      <option value="">Add to a project…</option>
                      {available.map(pr => (
                        <option key={pr.id} value={pr.id}>{pr.name}</option>
                      ))}
                    </select>
                    <Button
                      icon={Plus}
                      size="md"
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
                    >
                      Add
                    </Button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {form && (
        <Modal
          title={`Add ${form.plugin.name} to ${form.project.name}`}
          onClose={() => setForm(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setForm(null)}>Cancel</Button>
              <Button variant="primary" onClick={() => attach(form.plugin, form.project, form.vars, form.domain.trim(), form.path.trim())}>
                Add plugin
              </Button>
            </>
          }
        >
          {(form.plugin.vars ?? []).map(v => (
            <Field key={v.key} label={`${v.label}${v.generate ? ' (leave empty to generate)' : v.required ? '' : ' (optional)'}`}>
              <input
                type={v.secret ? 'password' : 'text'}
                value={form.vars[v.key] ?? ''}
                onChange={e => setForm(f => f && { ...f, vars: { ...f.vars, [v.key]: e.target.value } })}
                className={`${inputClass} font-mono`}
              />
            </Field>
          ))}
          {form.plugin.public && (
            <div className="grid grid-cols-2 gap-3">
              <Field label="Domain">
                <input value={form.domain} placeholder="db.example.com" onChange={e => setForm(f => f && { ...f, domain: e.target.value })} className={`${inputClass} font-mono`} />
              </Field>
              <Field label="Path (optional)">
                <input value={form.path} placeholder="/rest/v1/" onChange={e => setForm(f => f && { ...f, path: e.target.value })} className={`${inputClass} font-mono`} />
              </Field>
            </div>
          )}
          <p className="text-xs text-text-secondary">{form.project.name} is redeployed with the plugin. Secret values are stored encrypted.</p>
        </Modal>
      )}

      {adding && (
        <Modal
          title={`Add a custom ${kind === 'app' ? 'app' : 'notification'} plugin`}
          size="lg"
          onClose={() => setAdding(false)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setAdding(false)}>Cancel</Button>
              <Button variant="primary" onClick={addCustom}>Add plugin</Button>
            </>
          }
        >
          <div className="flex gap-1.5">
            {(['app', 'notification'] as const).map(k => (
              <Choice
                key={k}
                selected={kind === k}
                onClick={() => {
                  setKind(k);
                  setManifest(k === 'app' ? EXAMPLE : NOTIFY_EXAMPLE);
                }}
              >
                {k === 'app' ? 'App plugin (a container)' : 'Notification plugin (an HTTP request)'}
              </Choice>
            ))}
          </div>
          {kind === 'notification' ? (
            <p className="text-xs text-text-secondary">
              Describe the service&apos;s settings in <code>fields</code> and the request to send in <code>request</code>{' '}
              (<code>method</code>, <code>url</code>, <code>headers</code>, <code>body</code>). They are Go templates:{' '}
              <code>{'{{.title}}'}</code>, <code>{'{{.message}}'}</code>, <code>{'{{.text}}'}</code> (everything as plain text),{' '}
              <code>{'{{.url}}'}</code>, <code>{'{{.level}}'}</code>, <code>{'{{.emoji}}'}</code>, <code>{'{{.color}}'}</code>,{' '}
              <code>{'{{.event}}'}</code>, <code>{'{{.project}}'}</code>, <code>{'{{.node}}'}</code> and <code>{'{{.config.KEY}}'}</code> for a
              field. Wrap text in <code>{'{{json …}}'}</code> inside a JSON body so it is quoted and escaped. Use the same id again to replace a
              plugin.
            </p>
          ) : (
            <p className="text-xs text-text-secondary">
              Describe the container in JSON. In <code>provides</code>, <code>command</code>, <code>env</code> and <code>files</code> you can use{' '}
              <code>{'{{host}}'}</code> (the plugin&apos;s name in the project&apos;s network), <code>{'{{port}}'}</code> and{' '}
              <code>{'{{var.KEY}}'}</code> for settings declared in <code>vars</code> (e.g.{' '}
              <code>{'{"key": "password", "label": "Password", "secret": true, "generate": 32}'}</code>). The Hub only stores this description;
              the image is downloaded only on nodes that run it.
            </p>
          )}
          <textarea
            rows={14}
            value={manifest}
            onChange={e => setManifest(e.target.value)}
            spellCheck={false}
            className={`${inputClass} h-auto py-2 text-xs font-mono`}
          />
        </Modal>
      )}
    </div>
  );
}
