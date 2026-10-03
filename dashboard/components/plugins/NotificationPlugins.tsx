'use client';

import { useCallback, useEffect, useState } from 'react';
import { Hash, Mail, Pencil, Plus, Send, Slack, Trash2, Webhook } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { api } from '@/lib/api';
import type { NotificationChannel, NotificationEvent, NotificationType, Project } from '@/types';
import { useAuth } from '@/components/providers/AuthProvider';
import { BRAND_PATHS, BrandIcon } from './BrandIcons';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/Badge';
import { EmptyState } from '@/components/ui/Card';
import { Field, Modal, inputClass } from '@/components/ui/Modal';
import { Choice } from '@/components/ui/Tabs';

// Brand marks where the service has one; generic icons otherwise.
const LOOK: Record<string, { icon?: LucideIcon; brand?: string; color: string }> = {
  discord: { brand: BRAND_PATHS.discord, color: '#5865F2' },
  slack: { icon: Slack, color: '#4A154B' },
  telegram: { brand: BRAND_PATHS.telegram, color: '#26A5E4' },
  ntfy: { brand: BRAND_PATHS.ntfy, color: '#317F6F' },
  email: { icon: Mail, color: '#f29a00' },
  webhook: { icon: Webhook, color: '#52525b' },
};

function TypeIcon({ type, size = 'md' }: { type: string; size?: 'md' | 'lg' }) {
  const look = LOOK[type] ?? { icon: Hash, color: '#52525b' };
  const Icon = look.icon ?? Hash;
  const box = size === 'lg' ? 'h-9 w-9' : 'h-7 w-7';
  const glyph = size === 'lg' ? 'h-5 w-5' : 'h-4 w-4';
  return (
    <span className={`${box} flex-shrink-0 rounded-md flex items-center justify-center text-white`} style={{ background: look.color }}>
      {look.brand ? <BrandIcon path={look.brand} className={glyph} /> : <Icon className={glyph} />}
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
      {error && <div className="text-sm text-status-red break-words">{error}</div>}
      {notice && <div className="text-sm text-status-green">{notice}</div>}

      <section className="space-y-3">
        <h2 className="text-[11px] font-medium uppercase tracking-wider text-text-secondary">Your channels</h2>
        {channels.length === 0 ? (
          <div className="bg-surface border border-border rounded-lg">
            <EmptyState>No channels yet. Add a notification plugin below; it sends a test message right away.</EmptyState>
          </div>
        ) : (
          channels.map(ch => (
            <div key={ch.id} className="bg-surface border border-border rounded-lg p-4 flex flex-col md:flex-row md:items-start gap-4">
              <div className="flex items-start gap-3 flex-1 min-w-0">
                <TypeIcon type={ch.type} size="lg" />
                <div className="min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-sm font-semibold text-text-primary">{ch.name}</span>
                    <Badge>{typeOf(ch.type)?.name ?? ch.type}</Badge>
                    {!ch.enabled && <Badge tone="warning">paused</Badge>}
                  </div>
                  <div className="flex flex-wrap gap-1 mt-2">
                    {ch.events.length === 0 && <span className="text-xs text-text-secondary">No events chosen</span>}
                    {ch.events.map(e => (
                      <Badge key={e}>{eventLabel(e)}</Badge>
                    ))}
                  </div>
                  <div className="text-xs text-text-secondary mt-2">
                    {ch.projects.length ? `Apps: ${ch.projects.map(projectName).join(', ')}` : 'All apps'} · last sent {ago(ch.last_sent_at)}
                  </div>
                  {ch.last_error && <div className="text-xs text-status-red mt-1 break-words">Last attempt failed: {ch.last_error}</div>}
                </div>
              </div>
              <div className="flex items-center gap-1 flex-shrink-0">
                <label className="flex items-center gap-1.5 text-xs text-text-secondary cursor-pointer mr-2">
                  <input type="checkbox" checked={ch.enabled} onChange={() => toggle(ch)} className="accent-[#f59e0b]" />
                  On
                </label>
                <Button icon={Send} loading={busy === ch.id} onClick={() => test(ch)}>Send test</Button>
                <Button variant="ghost" icon={Pencil} aria-label="Edit" onClick={() => openEdit(ch)} />
                <Button variant="ghost" icon={Trash2} aria-label="Remove" className="hover:text-status-red" onClick={() => remove(ch)} />
              </div>
            </div>
          ))
        )}
      </section>

      <section className="space-y-3">
        <h2 className="text-[11px] font-medium uppercase tracking-wider text-text-secondary">Notification plugins</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
          {types.map(t => {
            const used = channels.filter(c => c.type === t.id).length;
            return (
              <div key={t.id} className="bg-surface border border-border rounded-lg p-4 flex flex-col gap-4">
                <div className="flex gap-3 flex-1">
                  <TypeIcon type={t.id} size="lg" />
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-semibold text-text-primary flex items-center gap-2">
                      {t.name}
                      <Badge>{t.builtin ? 'built-in' : 'custom'}</Badge>
                    </div>
                    <p className="text-xs text-text-secondary mt-1">{t.description}</p>
                  </div>
                  {!t.builtin && (
                    <Button variant="ghost" icon={Trash2} aria-label="Remove this plugin" className="self-start hover:text-status-red" onClick={() => removeType(t)} />
                  )}
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-text-secondary">{used ? `${used} channel${used > 1 ? 's' : ''}` : 'Not added yet'}</span>
                  <Button icon={Plus} onClick={() => openNew(t)}>Add</Button>
                </div>
              </div>
            );
          })}
        </div>
      </section>

      {form && (
        <Modal
          title={<><TypeIcon type={form.type.id} /> {form.id ? `Edit ${form.name}` : `Add ${form.type.name}`}</>}
          onClose={() => setForm(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setForm(null)}>Cancel</Button>
              <Button variant="primary" onClick={save}>{form.id ? 'Save' : 'Add and send a test'}</Button>
            </>
          }
        >
          <p className="text-xs text-text-secondary">{form.type.description}</p>
          <Field label="Name">
            <input value={form.name} placeholder={form.type.name} onChange={e => setForm(f => f && { ...f, name: e.target.value })} className={inputClass} />
          </Field>
          {form.type.fields.map(f => (
            <Field key={f.key} label={f.label} help={f.help}>
              <input
                type={f.secret ? 'password' : 'text'}
                value={form.config[f.key] ?? ''}
                placeholder={f.placeholder}
                autoComplete="off"
                onFocus={e => f.secret && e.target.value === '********' && e.target.select()}
                onChange={e => setForm(x => x && { ...x, config: { ...x.config, [f.key]: e.target.value } })}
                className={`${inputClass} font-mono`}
              />
            </Field>
          ))}
          <div>
            <div className="text-xs text-text-secondary mb-2">Send these events</div>
            <div className="space-y-2">
              {events.map(ev => (
                <label key={ev.id} className="flex items-start gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    className="mt-0.5 accent-[#f59e0b]"
                    checked={form.events.includes(ev.id)}
                    onChange={e =>
                      setForm(f => f && { ...f, events: e.target.checked ? [...f.events, ev.id] : f.events.filter(x => x !== ev.id) })
                    }
                  />
                  <span>
                    <span className="text-sm text-text-primary">{ev.label}</span>
                    <span className="block text-[11px] text-text-secondary">{ev.description}</span>
                  </span>
                </label>
              ))}
            </div>
          </div>
          {projects.length > 0 && (
            <div>
              <div className="text-xs text-text-secondary mb-2">App events for</div>
              <div className="flex flex-wrap gap-1.5">
                <Choice selected={form.projects.length === 0} onClick={() => setForm(f => f && { ...f, projects: [] })}>All apps</Choice>
                {projects.map(p => {
                  const on = form.projects.includes(p.id);
                  return (
                    <Choice key={p.id} selected={on} onClick={() => setForm(f => f && { ...f, projects: on ? f.projects.filter(x => x !== p.id) : [...f.projects, p.id] })}>
                      {p.name}
                    </Choice>
                  );
                })}
              </div>
              <p className="text-[11px] text-text-muted mt-1">Node and Hub events are always sent.</p>
            </div>
          )}
          {formError && <div className="text-sm text-status-red break-words">{formError}</div>}
        </Modal>
      )}
    </div>
  );
}
