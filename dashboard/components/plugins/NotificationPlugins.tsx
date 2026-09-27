'use client';

import { useCallback, useEffect, useState } from 'react';
import { BellRing, Hash, Mail, MessageCircle, Pencil, Send, Slack, Trash2, Webhook, X } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { api } from '@/lib/api';
import type { NotificationChannel, NotificationEvent, NotificationType, Project } from '@/types';
import { useAuth } from '@/components/providers/AuthProvider';

const LOOK: Record<string, { icon: LucideIcon; color: string }> = {
  discord: { icon: MessageCircle, color: '#5865F2' },
  slack: { icon: Slack, color: '#4A154B' },
  telegram: { icon: Send, color: '#26A5E4' },
  ntfy: { icon: BellRing, color: '#338574' },
  email: { icon: Mail, color: '#f29a00' },
  webhook: { icon: Webhook, color: '#52525b' },
};

function TypeIcon({ type, size = 'md' }: { type: string; size?: 'md' | 'lg' }) {
  const look = LOOK[type] ?? { icon: Hash, color: '#52525b' };
  const Icon = look.icon;
  const box = size === 'lg' ? 'h-9 w-9' : 'h-7 w-7';
  return (
    <span className={`${box} flex-shrink-0 rounded-md flex items-center justify-center text-white`} style={{ background: look.color }}>
      <Icon className={size === 'lg' ? 'h-5 w-5' : 'h-4 w-4'} />
    </span>
  );
}

function ago(iso: string | null) {
  if (!iso) return 'never';
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return new Date(iso).toLocaleDateString();
}

type Form = {
  id?: string;
  type: NotificationType;
  name: string;
  config: Record<string, string>;
  events: string[];
  projects: string[];
};

// Notification plugins: the catalog (built-in and custom) and the channels
// made from them. Shown on the Plugins page, for admins.
export function NotificationPlugins({ reloadKey = 0 }: { reloadKey?: number }) {
  const { user } = useAuth();
  const [types, setTypes] = useState<NotificationType[]>([]);
  const [events, setEvents] = useState<NotificationEvent[]>([]);
  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [form, setForm] = useState<Form | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [t, ch, pr] = await Promise.all([
        api.getNotificationTypes(),
        api.getNotificationChannels(),
        api.getProjects(1, 100).catch(() => ({ data: [] as Project[] })),
      ]);
      setTypes(t.types);
      setEvents(t.events);
      setChannels(ch);
      setProjects(pr.data ?? []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load notifications');
    }
  }, []);

  useEffect(() => {
    load();
  }, [load, reloadKey]);

  const typeOf = (id: string) => types.find(t => t.id === id);

  const openNew = (t: NotificationType) => {
    setFormError(null);
    setForm({
      type: t,
      name: '',
      config: Object.fromEntries(t.fields.map(f => [f.key, f.default ?? ''])),
      events: events.map(e => e.id),
      projects: [],
    });
  };

  const openEdit = (ch: NotificationChannel) => {
    const t = typeOf(ch.type);
    if (!t) return;
    setFormError(null);
    setForm({
      id: ch.id,
      type: t,
      name: ch.name,
      config: Object.fromEntries(ch.config.map(c => [c.key, c.value])),
      events: ch.events,
      projects: ch.projects,
    });
  };

  const test = async (ch: NotificationChannel) => {
    setBusy(ch.id);
    setError(null);
    setNotice(null);
    try {
      await api.testNotificationChannel(ch.id);
      setNotice(`Test sent to ${ch.name}. Check that it arrived.`);
    } catch (err) {
      setError(`${ch.name}: ${err instanceof Error ? err.message : 'the test failed'}`);
    } finally {
      setBusy(null);
      load();
    }
  };

  const save = async () => {
    if (!form) return;
    setFormError(null);
    const input = {
      name: form.name.trim(),
      config: Object.entries(form.config).map(([key, value]) => ({ key, value })),
      events: form.events,
      projects: form.projects,
    };
    try {
      const ch = form.id
        ? await api.updateNotificationChannel(form.id, input)
        : await api.createNotificationChannel({ ...input, type: form.type.id });
      setForm(null);
      if (!form.id) {
        // A new channel gets a test right away, so a wrong URL shows up now.
        await test(ch);
      } else {
        setNotice(`${ch.name} saved.`);
        load();
      }
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Could not save');
    }
  };

  const toggle = async (ch: NotificationChannel) => {
    try {
      await api.updateNotificationChannel(ch.id, { enabled: !ch.enabled });
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not change it');
    }
  };

  const remove = async (ch: NotificationChannel) => {
    if (!confirm(`Stop sending notifications to ${ch.name}?`)) return;
    try {
      await api.deleteNotificationChannel(ch.id);
      setNotice(`${ch.name} removed.`);
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not remove it');
    }
  };

  const removeType = async (t: NotificationType) => {
    if (!confirm(`Remove the ${t.name} notification plugin?`)) return;
    try {
      await api.removeNotificationPlugin(t.id);
      setNotice(`${t.name} removed.`);
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not remove it');
    }
  };

  if (user && user.role !== 'admin') {
    return <div className="text-sm text-text-muted">Only admins can manage notifications.</div>;
  }

  const eventLabel = (id: string) => events.find(e => e.id === id)?.label ?? id;
  const projectName = (id: string) => projects.find(p => p.id === id)?.name ?? 'a removed project';

  return (
    <div className="space-y-6">
      <p className="text-sm text-text-muted max-w-2xl">
        Get told when a deploy fails, an app goes down and moves to another node, a node goes offline or a Hub update is
        out. Add a notification plugin as many times as you like; each channel picks its own events and, optionally,
        apps.
      </p>

      {error && <div className="text-sm text-status-red break-words">{error}</div>}
      {notice && <div className="text-sm text-status-green">{notice}</div>}

      <section className="space-y-3">
        <h2 className="text-xs uppercase tracking-widest text-text-muted">Your channels</h2>
        {channels.length === 0 && (
          <div className="bg-surface border border-border rounded-lg p-5 text-sm text-text-muted">
            No channels yet. Add one below; it gets a test message right away.
          </div>
        )}
        {channels.map(ch => (
          <div key={ch.id} className="bg-surface border border-border rounded-lg p-4 flex flex-col md:flex-row md:items-start gap-4">
            <div className="flex items-start gap-3 flex-1 min-w-0">
              <TypeIcon type={ch.type} size="lg" />
              <div className="min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-sm font-semibold text-text-primary">{ch.name}</span>
                  <span className="px-1.5 py-0.5 rounded text-[10px] bg-surface-hover text-text-muted">
                    {typeOf(ch.type)?.name ?? ch.type}
                  </span>
                  {!ch.enabled && <span className="px-1.5 py-0.5 rounded text-[10px] bg-surface-hover text-text-muted">paused</span>}
                </div>
                <div className="flex flex-wrap gap-1 mt-2">
                  {ch.events.length === 0 && <span className="text-xs text-text-muted">No events chosen</span>}
                  {ch.events.map(e => (
                    <span key={e} className="px-1.5 py-0.5 rounded text-[10px] border border-border text-text-secondary">
                      {eventLabel(e)}
                    </span>
                  ))}
                </div>
                <div className="text-xs text-text-muted mt-2">
                  {ch.projects.length ? `Apps: ${ch.projects.map(projectName).join(', ')}` : 'All apps'} · last sent {ago(ch.last_sent_at)}
                </div>
                {ch.last_error && <div className="text-xs text-status-red mt-1 break-words">Last attempt failed: {ch.last_error}</div>}
              </div>
            </div>
            <div className="flex items-center gap-2 flex-shrink-0">
              <label className="flex items-center gap-1.5 text-xs text-text-muted cursor-pointer mr-1">
                <input type="checkbox" checked={ch.enabled} onChange={() => toggle(ch)} className="accent-[#f29a00]" />
                On
              </label>
              <button
                onClick={() => test(ch)}
                disabled={busy === ch.id}
                className="flex items-center gap-1.5 text-xs font-medium border border-border text-text-primary px-2.5 py-1.5 rounded hover:bg-surface-hover disabled:opacity-40"
              >
                <Send className="h-3.5 w-3.5" /> {busy === ch.id ? 'Sending…' : 'Send test'}
              </button>
              <button title="Edit" onClick={() => openEdit(ch)} className="p-1.5 text-text-muted hover:text-text-primary">
                <Pencil className="h-4 w-4" />
              </button>
              <button title="Remove" onClick={() => remove(ch)} className="p-1.5 text-text-muted hover:text-status-red">
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          </div>
        ))}
      </section>

      <section className="space-y-3">
        <h2 className="text-xs uppercase tracking-widest text-text-muted">Notification plugins</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-3">
          {types.map(t => {
            const used = channels.filter(c => c.type === t.id).length;
            return (
              <div key={t.id} className="bg-surface border border-border rounded-lg p-4 flex flex-col gap-3">
                <div className="flex gap-3 flex-1">
                  <TypeIcon type={t.id} size="lg" />
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-semibold text-text-primary flex items-center gap-2">
                      {t.name}
                      <span className="px-1.5 py-0.5 rounded text-[10px] bg-surface-hover text-text-muted">
                        {t.builtin ? 'built-in' : 'custom'}
                      </span>
                    </div>
                    <p className="text-xs text-text-muted mt-1">{t.description}</p>
                  </div>
                  {!t.builtin && (
                    <button title="Remove this plugin" onClick={() => removeType(t)} className="self-start text-text-muted hover:text-status-red">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  )}
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-text-muted">{used ? `${used} channel${used > 1 ? 's' : ''}` : 'Not added yet'}</span>
                  <button
                    onClick={() => openNew(t)}
                    className="text-xs font-medium border border-border text-text-primary px-3 py-1.5 rounded hover:bg-surface-hover"
                  >
                    Add
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      </section>

      {form && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg w-full max-w-xl max-h-[90vh] flex flex-col">
            <div className="flex items-center justify-between px-5 py-4 border-b border-border">
              <h2 className="font-medium text-text-primary flex items-center gap-2">
                <TypeIcon type={form.type.id} />
                {form.id ? `Edit ${form.name}` : `Add ${form.type.name}`}
              </h2>
              <button onClick={() => setForm(null)} className="text-text-muted hover:text-text-primary">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="p-5 space-y-4 overflow-y-auto">
              <p className="text-xs text-text-muted">{form.type.description}</p>
              <div>
                <label className="block text-xs text-text-muted mb-1">Name</label>
                <input
                  value={form.name}
                  placeholder={form.type.name}
                  onChange={e => setForm(f => f && { ...f, name: e.target.value })}
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent"
                />
              </div>
              {form.type.fields.map(f => (
                <div key={f.key}>
                  <label className="block text-xs text-text-muted mb-1">{f.label}</label>
                  <input
                    type={f.secret ? 'password' : 'text'}
                    value={form.config[f.key] ?? ''}
                    placeholder={f.placeholder}
                    autoComplete="off"
                    onFocus={e => f.secret && e.target.value === '********' && e.target.select()}
                    onChange={e => setForm(x => x && { ...x, config: { ...x.config, [f.key]: e.target.value } })}
                    className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:outline-none focus:border-accent"
                  />
                  {f.help && <p className="text-[11px] text-text-muted mt-1">{f.help}</p>}
                </div>
              ))}

              <div>
                <div className="text-xs text-text-muted mb-2">Send these events</div>
                <div className="space-y-2">
                  {events.map(ev => (
                    <label key={ev.id} className="flex items-start gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        className="mt-0.5 accent-[#f29a00]"
                        checked={form.events.includes(ev.id)}
                        onChange={e =>
                          setForm(f => f && {
                            ...f,
                            events: e.target.checked ? [...f.events, ev.id] : f.events.filter(x => x !== ev.id),
                          })
                        }
                      />
                      <span>
                        <span className="text-sm text-text-primary">{ev.label}</span>
                        <span className="block text-[11px] text-text-muted">{ev.description}</span>
                      </span>
                    </label>
                  ))}
                </div>
              </div>

              {projects.length > 0 && (
                <div>
                  <div className="text-xs text-text-muted mb-2">App events for</div>
                  <div className="flex flex-wrap gap-1.5">
                    <button
                      onClick={() => setForm(f => f && { ...f, projects: [] })}
                      className={`px-2 py-1 rounded text-xs border ${form.projects.length === 0 ? 'border-accent text-text-primary' : 'border-border text-text-muted'}`}
                    >
                      All apps
                    </button>
                    {projects.map(p => {
                      const on = form.projects.includes(p.id);
                      return (
                        <button
                          key={p.id}
                          onClick={() =>
                            setForm(f => f && { ...f, projects: on ? f.projects.filter(x => x !== p.id) : [...f.projects, p.id] })
                          }
                          className={`px-2 py-1 rounded text-xs border ${on ? 'border-accent text-text-primary' : 'border-border text-text-muted'}`}
                        >
                          {p.name}
                        </button>
                      );
                    })}
                  </div>
                  <p className="text-[11px] text-text-muted mt-1">Node and Hub events are always sent.</p>
                </div>
              )}
              {formError && <div className="text-sm text-status-red break-words">{formError}</div>}
            </div>
            <div className="flex justify-end gap-3 px-5 py-3.5 border-t border-border">
              <button onClick={() => setForm(null)} className="text-sm text-text-muted hover:text-text-primary px-3 py-1.5">
                Cancel
              </button>
              <button onClick={save} className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90">
                {form.id ? 'Save' : 'Add and send a test'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
