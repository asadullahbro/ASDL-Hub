'use client';

import { useCallback, useEffect, useState } from 'react';
import { formatDistanceToNow } from 'date-fns';
import { Camera, Download, RefreshCw, RotateCcw, Trash2 } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/Badge';
import { Card, EmptyState, StatCard } from '@/components/ui/Card';
import { Field, Modal, inputClass, tdClass, thClass } from '@/components/ui/Modal';
import { api } from '@/lib/api';
import { useAuth } from '@/components/providers/AuthProvider';
import type { PluginSnapshot, Project, SnapshotOverview, SnapshotPlugin } from '@/types';

type AppSnapshots = { project: Project; overview: SnapshotOverview };

const size = (n: number) => (n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : n >= 1 << 10 ? `${(n / (1 << 10)).toFixed(1)} KB` : `${n} B`);
const ago = (iso: string) => `${formatDistanceToNow(new Date(iso))} ago`;
const WHY: Record<PluginSnapshot['trigger'], string> = { scheduled: 'Scheduled', move: 'Before a move', manual: 'Taken by hand', restore: 'Restored copy' };

// What the data of one plugin is protected by right now.
function protection(p: SnapshotPlugin): { label: string; tone: 'success' | 'danger' | 'warning' | 'neutral'; help: string } {
  if (p.snapshots && p.healthy) return { label: 'Protected', tone: 'success', help: `A copy is sent to the Hub every ${p.interval_minutes} min and put back if the node fails or the app moves.` };
  if (p.snapshots) return { label: 'Behind', tone: 'danger', help: `No recent snapshot, though one is expected every ${p.interval_minutes} min. Check that the app's node is online.` };
  if (p.persist) return { label: 'This node only', tone: 'warning', help: "The data is kept on the node's disk, so it survives restarts, but not the node failing or a move." };
  return { label: 'Not kept', tone: 'neutral', help: 'The data starts empty whenever the plugin starts on a node.' };
}

export default function SnapshotsPage() {
  const { user } = useAuth();
  const canManage = user?.role === 'admin' || user?.role === 'operator';
  const [apps, setApps] = useState<AppSnapshots[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [enable, setEnable] = useState<{ app: AppSnapshots; plugin: SnapshotPlugin; minutes: string } | null>(null);
  const [disable, setDisable] = useState<{ app: AppSnapshots; plugin: SnapshotPlugin; deleteData: boolean } | null>(null);
  const [restore, setRestore] = useState<{ app: AppSnapshots; plugin: SnapshotPlugin; snap: PluginSnapshot } | null>(null);
  const [remove, setRemove] = useState<{ app: AppSnapshots; snap: PluginSnapshot } | null>(null);

  const load = useCallback(async () => {
    try {
      const list = (await api.getProjects(1, 100)).data ?? [];
      const overviews = await Promise.all(list.map(async project => ({ project, overview: await api.getSnapshots(project.id).catch(() => null) })));
      setApps(overviews.filter((a): a is AppSnapshots => !!a.overview && a.overview.plugins.length > 0));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load snapshots');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 30000);
    return () => clearInterval(t);
  }, [load]);

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

  const protectedCount = apps.flatMap(a => a.overview.plugins).filter(p => p.snapshots && p.healthy).length;
  const behind = apps.flatMap(a => a.overview.plugins).filter(p => p.snapshots && !p.healthy).length;
  const stored = apps.flatMap(a => a.overview.plugins).reduce((n, p) => n + p.items.length, 0);
  const hubOff = apps.length > 0 && apps.every(a => !a.overview.enabled);

  return (
    <div className="space-y-6">
      <PageHeader
        icon={Camera}
        title="Snapshots"
        description="Copies of a plugin's data (like Redis) kept on the Hub. If a node fails or an app moves, the data is put back on the next node. Off unless you turn it on for a plugin."
        actions={<Button variant="secondary" icon={RefreshCw} onClick={load}>Refresh</Button>}
      />

      {error && <div className="rounded-md border border-status-red/30 bg-status-red/10 px-4 py-3 text-sm text-status-red">{error}</div>}
      {notice && <div className="rounded-md border border-status-green/30 bg-status-green/10 px-4 py-3 text-sm text-status-green">{notice}</div>}
      {hubOff && (
        <div className="rounded-md border border-status-yellow/30 bg-status-yellow/10 px-4 py-3 text-sm text-status-yellow">
          Snapshots are switched off on this Hub. Set <code>WG_HUB_IP</code> or <code>SNAPSHOT_URL</code> in its settings (<code>asdl-hub config set</code>) and restart it.
        </div>
      )}
      {behind > 0 && (
        <div className="rounded-md border border-status-red/30 bg-status-red/10 px-4 py-3 text-sm text-status-red">
          {behind} plugin{behind === 1 ? ' has' : 's have'} no recent snapshot, so {behind === 1 ? 'its' : 'their'} data is not protected right now.
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <StatCard label="Protected plugins" value={protectedCount} tone={protectedCount > 0 ? 'success' : undefined} />
        <StatCard label="Behind schedule" value={behind} tone={behind > 0 ? 'danger' : undefined} />
        <StatCard label="Snapshots stored" value={stored} />
      </div>

      {loading ? (
        <EmptyState>Loading…</EmptyState>
      ) : apps.length === 0 ? (
        <Card>
          <EmptyState>
            No app has a plugin that can keep its data yet. Attach Redis to an app on the Plugins page, then turn snapshots on here.
          </EmptyState>
        </Card>
      ) : (
        apps.map(app =>
          app.overview.plugins.map(pl => {
            const prot = protection(pl);
            return (
              <Card
                key={`${app.project.id}-${pl.plugin_id}`}
                title={<span>{app.project.name} <span className="text-text-secondary font-normal">/ {pl.plugin_id}</span></span>}
                description={prot.help}
                actions={
                  <>
                    <Badge tone={prot.tone}>{prot.label}</Badge>
                    {canManage && pl.snapshots && (
                      <Button variant="secondary" icon={Camera} onClick={() => run(() => api.takeSnapshot(app.project.id, pl.plugin_id), 'Snapshot queued; it shows up here in a moment.')}>
                        Snapshot now
                      </Button>
                    )}
                    {canManage && (
                      pl.snapshots ? (
                        <Button variant="ghost" onClick={() => setDisable({ app, plugin: pl, deleteData: false })}>Turn off</Button>
                      ) : (
                        <Button variant="primary" onClick={() => setEnable({ app, plugin: pl, minutes: '5' })}>Turn on</Button>
                      )
                    )}
                  </>
                }
                flush
              >
                {pl.items.length === 0 ? (
                  <EmptyState>{pl.snapshots ? 'No snapshot yet. The first one is taken within a minute or two.' : 'No stored snapshots.'}</EmptyState>
                ) : (
                  <div className="overflow-x-auto">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="border-b border-border">
                          <th className={thClass}>Generation</th>
                          <th className={thClass}>Taken</th>
                          <th className={thClass}>Size</th>
                          <th className={thClass}>Why</th>
                          <th className={thClass} />
                        </tr>
                      </thead>
                      <tbody>
                        {pl.items.map((s, i) => (
                          <tr key={s.id} className="border-b border-border last:border-0">
                            <td className={`${tdClass} tabular-nums`}>{s.generation}{i === 0 && <Badge tone="info" className="ml-2">newest</Badge>}</td>
                            <td className={tdClass} title={new Date(s.created_at).toLocaleString()}>{ago(s.created_at)}</td>
                            <td className={`${tdClass} tabular-nums`}>{size(s.size_bytes)}</td>
                            <td className={tdClass}>{WHY[s.trigger] ?? s.trigger}</td>
                            <td className={`${tdClass} text-right whitespace-nowrap`}>
                              {canManage && (
                                <>
                                  <a href={api.snapshotDownloadUrl(app.project.id, s.id)} className="inline-flex" title="Download">
                                    <Button variant="ghost" icon={Download}>Download</Button>
                                  </a>
                                  {pl.snapshots && i !== 0 && <Button variant="ghost" icon={RotateCcw} onClick={() => setRestore({ app, plugin: pl, snap: s })}>Restore</Button>}
                                  <Button variant="ghost" icon={Trash2} onClick={() => setRemove({ app, snap: s })}>Delete</Button>
                                </>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
                {pl.items.length > 0 && (
                  <div className="px-4 py-2.5 text-[11px] text-text-muted border-t border-border">
                    The Hub keeps the newest {app.overview.keep}, up to {app.overview.max_mb} MB each.
                  </div>
                )}
              </Card>
            );
          })
        )
      )}

      {enable && (
        <Modal
          title={`Turn on snapshots for ${enable.app.project.name} / ${enable.plugin.plugin_id}`}
          onClose={() => setEnable(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setEnable(null)}>Cancel</Button>
              <Button
                variant="primary"
                onClick={() => {
                  const { app, plugin, minutes } = enable;
                  setEnable(null);
                  run(
                    () => api.updatePluginVars(app.project.id, plugin.plugin_id, [
                      { key: 'persist', value: 'on' }, { key: 'snapshot', value: 'on' }, { key: 'snapshot_minutes', value: String(Math.max(1, parseInt(minutes, 10) || 5)) },
                    ]),
                    `Snapshots are on for ${plugin.plugin_id}. The app is redeploying; the first snapshot follows shortly.`,
                  );
                }}
              >
                Turn on
              </Button>
            </>
          }
        >
          <Field label="Minutes between snapshots" help="Data written after the last snapshot can be lost if the node fails, so shorter is safer. 5 is a good start.">
            <input type="number" min={1} max={1440} value={enable.minutes} onChange={e => setEnable(f => f && { ...f, minutes: e.target.value })} className={inputClass} />
          </Field>
          {!enable.plugin.persist && (
            <p className="text-xs text-status-yellow">
              This makes the plugin keep its data on disk, which restarts it once with an empty data folder: whatever it holds in memory right now is lost this one time.
            </p>
          )}
        </Modal>
      )}

      {disable && (
        <Modal
          title={`Turn off snapshots for ${disable.app.project.name} / ${disable.plugin.plugin_id}`}
          onClose={() => setDisable(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setDisable(null)}>Cancel</Button>
              <Button
                variant="danger"
                onClick={() => {
                  const { app, plugin, deleteData } = disable;
                  setDisable(null);
                  run(async () => {
                    await api.updatePluginVars(app.project.id, plugin.plugin_id, [{ key: 'snapshot', value: 'off' }]);
                    if (deleteData) await api.deleteSnapshots(app.project.id, plugin.plugin_id);
                  }, deleteData ? 'Snapshots are off and the stored copies are deleted.' : 'Snapshots are off. The stored copies were kept.');
                }}
              >
                Turn off
              </Button>
            </>
          }
        >
          <p className="text-sm text-text-secondary">
            The plugin keeps its data on the node&apos;s disk, but it will no longer survive the node failing or a move.
          </p>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={disable.deleteData} onChange={e => setDisable(f => f && { ...f, deleteData: e.target.checked })} />
            Also delete the {disable.plugin.items.length} stored snapshot{disable.plugin.items.length === 1 ? '' : 's'} from the Hub
          </label>
        </Modal>
      )}

      {restore && (
        <Modal
          title="Restore this snapshot?"
          onClose={() => setRestore(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setRestore(null)}>Cancel</Button>
              <Button
                variant="danger"
                onClick={() => {
                  const { app, snap } = restore;
                  setRestore(null);
                  run(() => api.restoreSnapshot(app.project.id, snap.id), `Restoring generation ${snap.generation}; ${app.project.name} is redeploying.`);
                }}
              >
                Restore
              </Button>
            </>
          }
        >
          <p className="text-sm text-text-secondary">
            Generation {restore.snap.generation}, taken {ago(restore.snap.created_at)} ({size(restore.snap.size_bytes)}), replaces the data{' '}
            <strong>{restore.app.project.name} / {restore.plugin.plugin_id}</strong> holds now, and the app redeploys. Anything written since that snapshot is lost.
          </p>
        </Modal>
      )}

      {remove && (
        <Modal
          title="Delete this snapshot?"
          onClose={() => setRemove(null)}
          footer={
            <>
              <Button variant="ghost" onClick={() => setRemove(null)}>Cancel</Button>
              <Button
                variant="danger"
                onClick={() => {
                  const { app, snap } = remove;
                  setRemove(null);
                  run(() => api.deleteSnapshot(app.project.id, snap.id), `Generation ${snap.generation} deleted.`);
                }}
              >
                Delete
              </Button>
            </>
          }
        >
          <p className="text-sm text-text-secondary">Generation {remove.snap.generation} from {ago(remove.snap.created_at)} is removed from the Hub for good.</p>
        </Modal>
      )}
    </div>
  );
}
