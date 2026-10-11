'use client';

import { useEffect, useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { StatusBadge } from '@/components/ui/Badge';
import { Field, Modal } from '@/components/ui/Modal';
import { AdminOnly, Input, List, Row, failed, useSettingsToast } from '@/components/settings/common';
import { api } from '@/lib/api';
import { EnrollmentToken, Node } from '@/types';

function Enrollment() {
  const { toast } = useSettingsToast();
  const installCmd = `curl -fsSL ${typeof window !== 'undefined' ? window.location.origin : ''}/install | sudo bash`;
  const [tokens, setTokens] = useState<EnrollmentToken[]>([]);
  const [label, setLabel] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    api.listEnrollmentTokens().then(setTokens).catch(() => {});
  }, []);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!label) return;
    setLoading(true);
    try {
      const t = await api.createEnrollmentToken(label);
      setTokens(prev => [t, ...prev]);
      setLabel('');
      toast('ok', 'Enrollment token created');
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setLoading(false);
    }
  };

  const revoke = async (t: EnrollmentToken) => {
    try {
      await api.revokeEnrollmentToken(t.id);
      setTokens(prev => prev.filter(x => x.id !== t.id));
      toast('ok', 'Token revoked');
    } catch (err) {
      toast('err', failed(err));
    }
  };

  return (
    <Card
      title="Node enrollment"
      description="Generate one-time tokens to onboard new nodes. Run the install command on any Linux server."
    >
      <div className="space-y-4">
        <div className="bg-background border border-border rounded-md p-3">
          <p className="text-xs text-text-secondary mb-2">Install command</p>
          <div className="flex items-center gap-2">
            <code className="text-xs text-text-primary font-mono flex-1 truncate">{installCmd}</code>
            <Button onClick={() => { navigator.clipboard.writeText(installCmd); toast('ok', 'Copied'); }}>Copy</Button>
          </div>
        </div>

        {tokens.length > 0 && (
          <List>
            {tokens.map(t => {
              const expired = new Date(t.expires_at) < new Date();
              return (
                <Row key={t.id}>
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium text-text-primary">{t.label}</div>
                    <div className="text-xs text-text-secondary mt-0.5">
                      {t.used
                        ? `used · node ${t.used_by.slice(0, 8)}`
                        : expired
                          ? 'expired'
                          : `expires ${new Date(t.expires_at).toLocaleString()}`}
                    </div>
                  </div>
                  {!t.used && !expired && (
                    <div className="flex items-center gap-2">
                      <code className="text-xs text-accent font-mono bg-accent/5 border border-accent/20 px-2 py-0.5 rounded">{t.token}</code>
                      <Button onClick={() => { navigator.clipboard.writeText(t.token); toast('ok', 'Token copied'); }}>Copy</Button>
                    </div>
                  )}
                  <StatusBadge
                    status={t.used ? 'completed' : expired ? 'failed' : 'running'}
                    label={t.used ? 'used' : expired ? 'expired' : 'active'}
                  />
                  {!t.used && <Button variant="danger" onClick={() => revoke(t)}>Revoke</Button>}
                </Row>
              );
            })}
          </List>
        )}

        <form onSubmit={create} noValidate className="flex gap-2">
          <Input placeholder="Label e.g. home-server" value={label} onChange={e => setLabel(e.target.value)} />
          <Button variant="primary" type="submit" loading={loading} disabled={!label}>Generate</Button>
        </form>
      </div>
    </Card>
  );
}

function MasterNode({ nodes }: { nodes: Node[] }) {
  const { toast } = useSettingsToast();
  const [master, setMaster] = useState<Node | null>(null);
  const [choosing, setChoosing] = useState(false);
  const [selected, setSelected] = useState('');
  const [loading, setLoading] = useState('');

  useEffect(() => {
    api.getMasterNode().then(r => setMaster(r.master_node)).catch(() => {});
  }, []);

  const set = async () => {
    if (!selected) return;
    setLoading('set');
    try {
      await api.setMasterNode(selected);
      const node = nodes.find(n => n.id === selected) ?? null;
      setMaster(node);
      setChoosing(false);
      toast('ok', `Master node set to ${node?.hostname}`);
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setLoading('');
    }
  };

  const clear = async () => {
    setLoading('clear');
    try {
      await api.clearMasterNode();
      setMaster(null);
      toast('ok', 'Master node cleared');
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setLoading('');
    }
  };

  return (
    <>
      <Card title="Master node" description="All projects always migrate to this node when it's online, and never away from it.">
        <div className="flex flex-wrap items-center justify-between gap-3">
          {master ? (
            <div>
              <div className="flex items-center gap-2">
                <span className={`w-1.5 h-1.5 rounded-full ${master.online ? 'bg-status-green' : 'bg-status-red'}`} />
                <span className="text-xs font-medium text-text-primary font-mono">{master.hostname}</span>
              </div>
              <p className="text-xs text-text-secondary font-mono mt-1">{master.vpn_ip}</p>
            </div>
          ) : (
            <span className="text-xs text-text-muted">No master node set — failover picks the healthiest.</span>
          )}
          <div className="flex gap-2">
            <Button onClick={() => { setSelected(master?.id ?? ''); setChoosing(true); }}>{master ? 'Change' : 'Set master'}</Button>
            {master && <Button variant="danger" loading={loading === 'clear'} onClick={clear}>Clear</Button>}
          </div>
        </div>
      </Card>

      {choosing && (
        <Modal title="Set master node" size="sm" onClose={() => setChoosing(false)}>
          <div className="space-y-4">
            <Field label="Node">
              <select
                value={selected}
                onChange={e => setSelected(e.target.value)}
                className="w-full h-9 px-3 bg-surface border border-border rounded-md text-sm text-text-primary font-mono focus:outline-none focus:border-border-strong transition-colors"
              >
                <option value="">Select a node...</option>
                {nodes.map(n => (
                  <option key={n.id} value={n.id}>
                    {n.hostname} ({n.vpn_ip}) {n.online ? '● online' : '○ offline'}
                  </option>
                ))}
              </select>
            </Field>
            <p className="text-xs text-text-secondary">
              All projects will migrate to this node when it comes online. No project will ever be migrated away from it.
            </p>
            <div className="flex gap-2 pt-1">
              <Button onClick={() => setChoosing(false)}>Cancel</Button>
              <Button variant="primary" disabled={!selected} loading={loading === 'set'} onClick={set}>Set master</Button>
            </div>
          </div>
        </Modal>
      )}
    </>
  );
}

function AgentUpdate({ nodes }: { nodes: Node[] }) {
  const { toast } = useSettingsToast();
  const [loading, setLoading] = useState(false);
  const online = nodes.filter(n => n.online).length;

  const deploy = async () => {
    setLoading(true);
    try {
      const res = await api.deployAgents();
      toast('ok', res.message);
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Card title="Agent update" description="Dispatch an update job to all online nodes. Agents will download and restart.">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-text-secondary">
          Dispatches to {online} online node{online !== 1 ? 's' : ''}.
        </p>
        <Button variant="primary" loading={loading} onClick={deploy}>Deploy agents</Button>
      </div>
    </Card>
  );
}

function NodesSettingsBody() {
  const [nodes, setNodes] = useState<Node[]>([]);
  useEffect(() => {
    api.getNodes().then(setNodes).catch(() => {});
  }, []);
  return (
    <>
      <Enrollment />
      <MasterNode nodes={nodes} />
      <AgentUpdate nodes={nodes} />
    </>
  );
}

export default function NodesSettings() {
  return (
    <AdminOnly>
      <NodesSettingsBody />
    </AdminOnly>
  );
}
