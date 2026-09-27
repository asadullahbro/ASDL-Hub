'use client';

import { useCallback, useEffect, useState } from 'react';
import { Database, Bell, Puzzle, ExternalLink, Trash2 } from 'lucide-react';
import { api } from '@/lib/api';
import type { Plugin, DatabaseEntry } from '@/types';
import { useAuth } from '@/components/providers/AuthProvider';

const icons: Record<string, typeof Puzzle> = { databases: Database, notifications: Bell };

export default function PluginsPage() {
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    try {
      setPlugins(await api.getPlugins());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load plugins');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong');
    }
  };

  if (loading) {
    return <div className="flex items-center justify-center h-96 text-text-muted text-sm">Loading plugins…</div>;
  }

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-lg font-medium text-text-primary">Plugins</h1>
        <p className="text-xs text-text-muted mt-1 max-w-2xl">
          Optional features for your Hub. A plugin's files are only fetched onto the nodes you choose when you install it,
          so Hubs and nodes that don't use it carry none of it. Installing from here is coming soon.
        </p>
      </div>

      {error && <div className="text-xs text-status-red">{error}</div>}

      <div className="grid gap-4 md:grid-cols-2">
        {plugins.map(p => (
          <PluginCard key={p.id} plugin={p} isAdmin={isAdmin} act={act} />
        ))}
      </div>

      {isAdmin && <AddCustomPlugin act={act} />}
    </div>
  );
}

function StatusBadge({ p }: { p: Plugin }) {
  const [text, cls] =
    p.status === 'coming_soon'
      ? ['Coming soon', 'bg-border text-text-muted']
      : p.installed
        ? ['Installed', 'bg-status-green/15 text-status-green']
        : ['Not installed', 'bg-surface-hover text-text-secondary'];
  return <span className={`px-2 py-0.5 rounded text-xs font-medium ${cls}`}>{text}</span>;
}

function PluginCard({ plugin: p, isAdmin, act }: { plugin: Plugin; isAdmin: boolean; act: (fn: () => Promise<unknown>) => void }) {
  const Icon = icons[p.id] ?? Puzzle;
  return (
    <div className="bg-surface border border-border rounded-lg p-5 flex flex-col gap-3">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-2">
          <Icon className="h-4 w-4 text-accent" />
          <h2 className="text-sm font-semibold text-text-primary">{p.name}</h2>
          {p.custom && <span className="px-1.5 py-0.5 rounded text-[10px] bg-accent/15 text-accent">Custom</span>}
        </div>
        <StatusBadge p={p} />
      </div>
      {p.description && <p className="text-xs text-text-secondary">{p.description}</p>}
      {p.footprint && (
        <div className="text-xs">
          <span className="text-text-muted">Installs: </span>
          <span className="text-text-secondary">{p.footprint}</span>
        </div>
      )}
      {p.source && (
        <a href={p.source} target="_blank" rel="noreferrer" className="text-xs text-accent hover:underline inline-flex items-center gap-1 w-fit">
          {p.source.replace(/^https:\/\//, '')} <ExternalLink className="h-3 w-3" />
        </a>
      )}

      {p.id === 'databases' && <DatabasesList plugin={p} isAdmin={isAdmin} act={act} />}

      <div className="flex items-center gap-3 mt-auto pt-2 border-t border-border">
        <button
          disabled
          title="Installing plugins from the Hub is coming soon; nothing is downloaded until then."
          className="text-xs font-medium px-3 py-1.5 rounded bg-accent text-background opacity-40 cursor-not-allowed"
        >
          Install
        </button>
        {isAdmin && p.status === 'available' && (
          <button
            onClick={() => act(() => api.setPluginInstalled(p.id, !p.installed, true))}
            className="text-xs text-text-muted hover:text-text-primary"
            title="For a plugin you set up by hand on your nodes"
          >
            {p.installed ? 'Mark as not installed' : 'Mark as installed by hand'}
          </button>
        )}
        {isAdmin && p.custom && (
          <button
            onClick={() => window.confirm(`Remove ${p.name} from the plugin list?`) && act(() => api.removeCustomPlugin(p.id))}
            className="ml-auto text-text-muted hover:text-status-red"
            title="Remove from the list"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

const emptyDb: DatabaseEntry = { name: '', engine: '', primary_node: '', primary_endpoint: '', standby_node: '', standby_endpoint: '', backups: '', projects: [] };

function DatabasesList({ plugin, isAdmin, act }: { plugin: Plugin; isAdmin: boolean; act: (fn: () => Promise<unknown>) => void }) {
  const dbs = ((plugin.config as { databases?: DatabaseEntry[] } | undefined)?.databases) ?? [];
  const [form, setForm] = useState<DatabaseEntry | null>(null);
  const [projects, setProjects] = useState('');

  const save = (list: DatabaseEntry[]) => act(() => api.setDatabases(list));

  return (
    <div className="space-y-2">
      {dbs.length === 0 && <div className="text-xs text-text-muted">No databases added yet.</div>}
      {dbs.map(d => (
        <div key={d.name} className="rounded border border-border p-3 text-xs space-y-1">
          <div className="flex items-center justify-between">
            <span className="font-medium text-text-primary">{d.name}</span>
            {isAdmin && (
              <button onClick={() => window.confirm(`Remove ${d.name}?`) && save(dbs.filter(x => x.name !== d.name))} className="text-text-muted hover:text-status-red" title="Remove">
                <Trash2 className="h-3 w-3" />
              </button>
            )}
          </div>
          {d.engine && <div className="text-text-muted">{d.engine}</div>}
          <div><span className="text-text-muted">Primary: </span><span className="text-text-secondary">{d.primary_node || '—'}</span> <span className="font-mono text-text-muted">{d.primary_endpoint}</span></div>
          {d.standby_endpoint && <div><span className="text-text-muted">Standby: </span><span className="text-text-secondary">{d.standby_node || '—'}</span> <span className="font-mono text-text-muted">{d.standby_endpoint}</span></div>}
          {d.backups && <div><span className="text-text-muted">Backups: </span><span className="text-text-secondary">{d.backups}</span></div>}
          {d.projects.length > 0 && <div><span className="text-text-muted">Used by: </span><span className="text-text-secondary">{d.projects.join(', ')}</span></div>}
        </div>
      ))}
      {isAdmin && !form && (
        <button onClick={() => { setForm({ ...emptyDb }); setProjects(''); }} className="text-xs text-accent hover:underline">+ Add a database</button>
      )}
      {isAdmin && form && (
        <div className="rounded border border-border p-3 space-y-2">
          {([
            ['name', 'Name', 'gamer-ak'],
            ['engine', 'Engine', 'Supabase (Postgres 15)'],
            ['primary_node', 'Primary node', 'abdullah'],
            ['primary_endpoint', 'Primary endpoint', '10.101.0.4:54322'],
            ['standby_node', 'Standby node (optional)', 'asadullahs-pc'],
            ['standby_endpoint', 'Standby endpoint (optional)', '10.101.0.3:54332'],
            ['backups', 'Backups (optional)', 'nightly on abdullah, copied to asadullahs-pc'],
          ] as const).map(([key, label, ph]) => (
            <label key={key} className="block text-xs">
              <span className="text-text-muted">{label}</span>
              <input
                value={(form[key] as string) ?? ''}
                placeholder={ph}
                onChange={e => setForm({ ...form, [key]: e.target.value })}
                className="mt-0.5 w-full bg-background border border-border rounded px-2 py-1.5 text-text-primary focus:outline-none focus:border-accent"
              />
            </label>
          ))}
          <label className="block text-xs">
            <span className="text-text-muted">Used by projects (comma separated)</span>
            <input value={projects} placeholder="gamer-ak-bot" onChange={e => setProjects(e.target.value)} className="mt-0.5 w-full bg-background border border-border rounded px-2 py-1.5 text-text-primary focus:outline-none focus:border-accent" />
          </label>
          <div className="flex gap-3 justify-end">
            <button onClick={() => setForm(null)} className="text-xs text-text-muted hover:text-text-primary">Cancel</button>
            <button
              onClick={() => {
                const entry = { ...form, projects: projects.split(',').map(s => s.trim()).filter(Boolean) };
                save([...dbs.filter(x => x.name !== entry.name), entry]);
                setForm(null);
              }}
              className="text-xs font-medium px-3 py-1.5 rounded bg-accent text-background hover:opacity-90"
            >
              Save
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function AddCustomPlugin({ act }: { act: (fn: () => Promise<unknown>) => void }) {
  const [open, setOpen] = useState(false);
  const [f, setF] = useState({ id: '', name: '', description: '', source: '', footprint: '' });
  const field = (key: keyof typeof f, label: string, ph: string, hint?: string) => (
    <label className="block text-xs">
      <span className="text-text-muted">{label}</span>
      <input
        value={f[key]}
        placeholder={ph}
        onChange={e => setF({ ...f, [key]: key === 'id' ? e.target.value.toLowerCase() : e.target.value })}
        className="mt-0.5 w-full bg-background border border-border rounded px-2 py-1.5 text-text-primary focus:outline-none focus:border-accent"
      />
      {hint && <span className="text-[11px] text-text-muted">{hint}</span>}
    </label>
  );

  return (
    <div className="bg-surface border border-border rounded-lg p-5">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">Add a custom plugin</h2>
          <p className="text-xs text-text-muted mt-0.5">
            Add your own plugin to this list. Only its description is stored; nothing is downloaded until it's installed.
          </p>
        </div>
        {!open && (
          <button onClick={() => setOpen(true)} className="text-xs font-medium px-3 py-1.5 rounded border border-border text-text-primary hover:bg-surface-hover">
            Add plugin
          </button>
        )}
      </div>
      {open && (
        <div className="mt-4 grid gap-3 md:grid-cols-2">
          {field('name', 'Name', 'Uptime monitor')}
          {field('id', 'ID', 'uptime-monitor', 'Lowercase letters, digits and dashes')}
          <div className="md:col-span-2">{field('description', 'What it does', 'Checks my sites every minute and shows their uptime')}</div>
          <div className="md:col-span-2">{field('source', 'Where its files live', 'https://github.com/you/your-plugin', 'An https:// link to its repository or manifest')}</div>
          <div className="md:col-span-2">{field('footprint', 'What installing it adds, and where', 'One container on a node you pick')}</div>
          <div className="md:col-span-2 flex justify-end gap-3">
            <button onClick={() => setOpen(false)} className="text-xs text-text-muted hover:text-text-primary">Cancel</button>
            <button
              onClick={() =>
                act(async () => {
                  await api.addCustomPlugin(f);
                  setF({ id: '', name: '', description: '', source: '', footprint: '' });
                  setOpen(false);
                })
              }
              className="text-xs font-medium px-3 py-1.5 rounded bg-accent text-background hover:opacity-90"
            >
              Add to list
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
