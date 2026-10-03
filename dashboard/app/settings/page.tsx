'use client';

import { useEffect, useState, useCallback } from 'react';
import { HubVersion } from '@/components/settings/HubVersion';
import { api } from '@/lib/api';
import { useAuth } from '@/components/providers/AuthProvider';
import { Node, User, PermanentToken, EnrollmentToken } from '@/types';
import { Settings as SettingsIcon } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { Badge, StatusBadge } from '@/components/ui/Badge';
import { Card } from '@/components/ui/Card';
import { Field as UIField, Modal as UIModal, inputClass } from '@/components/ui/Modal';

// --- Types ---
type Section = 'tokens' | 'users' | 'master-node' | 'nginx' | 'agents';

// --- Sudo Modal ---
function SudoModal({
  title,
  onConfirm,
  onCancel,
}: {
  title: string;
  onConfirm: (password: string) => Promise<void>;
  onCancel: () => void;
}) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (e?: React.FormEvent) => {
    e?.preventDefault();
    setLoading(true);
    setError('');
    try {
      await onConfirm(password);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Incorrect password');
    } finally {
      setLoading(false);
    }
  };

  return (
    <UIModal
      title={title}
      size="sm"
      onClose={onCancel}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>Cancel</Button>
          <Button variant="primary" loading={loading} onClick={() => submit()}>Confirm</Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4">
        <p className="text-xs text-text-secondary">Confirm your admin password to continue.</p>
        <UIField label="Password">
          <input type="password" value={password} onChange={e => setPassword(e.target.value)} className={inputClass} autoFocus required />
        </UIField>
        {error && <p className="text-xs text-status-red">{error}</p>}
      </form>
    </UIModal>
  );
}

// --- Generic Modal ---
function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <UIModal title={title} size="sm" onClose={onClose}>
      {children}
    </UIModal>
  );
}

// --- Section wrapper ---
function SectionCard({ title, description, children }: { title: string; description?: string; children: React.ReactNode }) {
  return (
    <Card title={title} description={description}>
      {children}
    </Card>
  );
}

const Field = UIField;

function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`${inputClass} ${props.className ?? ''}`} />;
}

function Btn({
  variant = 'default',
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'default' | 'primary' | 'danger';
  loading?: boolean;
}) {
  return <Button variant={variant === 'default' ? 'secondary' : variant} {...props} />;
}

// ============================================================
// MAIN PAGE
// ============================================================
export default function SettingsPage() {
  const { user } = useAuth();
  const installCmd = `curl -fsSL ${typeof window !== 'undefined' ? window.location.origin : ''}/install | sudo bash`;
  const [nodes, setNodes] = useState<Node[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [tokens, setTokens] = useState<PermanentToken[]>([]);
  const [masterNode, setMasterNode] = useState<Node | null>(null);
  const [enrollmentTokens, setEnrollmentTokens] = useState<EnrollmentToken[]>([]);
  const [enrollLabel, setEnrollLabel] = useState('');

  // Modal state
  const [sudo, setSudo] = useState<{
    title: string;
    onConfirm: (pw: string) => Promise<void>;
  } | null>(null);
  const [modal, setModal] = useState<Section | null>(null);

  // Form state
  const [tokenName, setTokenName] = useState('');
  const [newToken, setNewToken] = useState('');
  const [newUser, setNewUser] = useState({ username: '', email: '', password: '', role: 'viewer' });
  const [pwTarget, setPwTarget] = useState<User | null>(null);
  const [newPw, setNewPw] = useState('');
  const [selectedMaster, setSelectedMaster] = useState('');

  const [actionLoading, setActionLoading] = useState('');
  const [feedback, setFeedback] = useState<{ type: 'ok' | 'err'; msg: string } | null>(null);

  const toast = (type: 'ok' | 'err', msg: string) => {
    setFeedback({ type, msg });
    setTimeout(() => setFeedback(null), 4000);
  };

  const load = useCallback(async () => {
    const [nodesRes, usersRes, tokensRes, masterRes, enrollRes] = await Promise.allSettled([
    api.getNodes(),
    api.listUsers(),
    api.listTokens(),
    api.getMasterNode(),
    api.listEnrollmentTokens(),
]);

    if (enrollRes.status === 'fulfilled') setEnrollmentTokens(enrollRes.value);
    if (nodesRes.status === 'fulfilled') setNodes(nodesRes.value);
    if (usersRes.status === 'fulfilled') setUsers(usersRes.value);
    if (tokensRes.status === 'fulfilled') setTokens(tokensRes.value);
    if (masterRes.status === 'fulfilled') setMasterNode(masterRes.value.master_node);
  }, []);

  useEffect(() => { load(); }, [load]);

  // --- Token generation ---
  const handleGenerateToken = async (password: string) => {
    const res = await api.generateToken(tokenName, password);
    setNewToken(res.token);
    setTokens(prev => [res.meta, ...prev]);
    setTokenName('');
    setSudo(null);
  }

  // --- Users ---
  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    setActionLoading('create-user');
    try {
      const u = await api.createUser(newUser);
      setUsers(prev => [...prev, u]);
      setNewUser({ username: '', email: '', password: '', role: 'viewer' });
      setModal(null);
      toast('ok', `User ${u.username} created`);
    } catch (err) {
      toast('err', err instanceof Error ? err.message : 'Failed');
    } finally {
      setActionLoading('');
    }
  };

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pwTarget) return;
    setActionLoading('change-pw');
    try {
      await api.changePassword(pwTarget.id, newPw);
      setPwTarget(null);
      setNewPw('');
      toast('ok', 'Password updated');
    } catch (err) {
      toast('err', err instanceof Error ? err.message : 'Failed');
    } finally {
      setActionLoading('');
    }
  };

  const handleChangeRole = async (userId: string, role: string) => {
    try {
      await api.changeRole(userId, role);
      setUsers(prev => prev.map(u => u.id === userId ? { ...u, role } : u));
      toast('ok', 'Role updated');
    } catch (err) {
      toast('err', err instanceof Error ? err.message : 'Failed');
    }
  };

  const handleDeleteUser = async (userId: string) => {
    try {
      await api.deleteUser(userId);
      setUsers(prev => prev.filter(u => u.id !== userId));
      toast('ok', 'User deleted');
    } catch (err) {
      toast('err', err instanceof Error ? err.message : 'Failed');
    }
  };

  const handleCreateEnrollmentToken = async (e: React.FormEvent) => {
  e.preventDefault();
  if (!enrollLabel) return;
  setActionLoading('enroll-token');
  try {
    const t = await api.createEnrollmentToken(enrollLabel);
    setEnrollmentTokens(prev => [t, ...prev]);
    setEnrollLabel('');
    toast('ok', 'Enrollment token created');
  } catch (err) {
    toast('err', err instanceof Error ? err.message : 'Failed');
  } finally {
    setActionLoading('');
  }
};
  // --- Master node ---
  const handleSetMaster = async () => {
    if (!selectedMaster) return;
    setActionLoading('master');
    try {
      await api.setMasterNode(selectedMaster);
      const node = nodes.find(n => n.id === selectedMaster) ?? null;
      setMasterNode(node);
      setModal(null);
      toast('ok', `Master node set to ${node?.hostname}`);
    } catch (err) {
      toast('err', err instanceof Error ? err.message : 'Failed');
    } finally {
      setActionLoading('');
    }
  };

  const handleClearMaster = async () => {
    setActionLoading('clear-master');
    try {
      await api.clearMasterNode();
      setMasterNode(null);
      toast('ok', 'Master node cleared');
    } catch (err) {
      toast('err', err instanceof Error ? err.message : 'Failed');
    } finally {
      setActionLoading('');
    }
  };

  // --- Nginx ---
  const handleNginx = async () => {
    setActionLoading('nginx');
    try {
      await api.updateNginx();
      toast('ok', 'Nginx config updated');
    } catch (err) {
      toast('err', err instanceof Error ? err.message : 'Failed');
    } finally {
      setActionLoading('');
    }
  };

  // --- Agents ---
  const handleAgentDeploy = async () => {
    setActionLoading('agents');
    try {
      const res = await api.deployAgents();
      toast('ok', res.message);
    } catch (err) {
      toast('err', err instanceof Error ? err.message : 'Failed');
    } finally {
      setActionLoading('');
    }
  };

  return (
    <div className="space-y-6">
      <PageHeader
        icon={SettingsIcon}
        title="Settings"
        description="Tokens, users, the master node, routes, agents and Hub updates."
        actions={
          feedback && (
            <Badge tone={feedback.type === 'ok' ? 'success' : 'danger'} className="text-xs py-1">
              {feedback.msg}
            </Badge>
          )
        }
      />

      {/* 1. Permanent Tokens */}
      <SectionCard
        title="Permanent tokens"
        description="Long-lived API tokens for agents and integrations. Only shown once on creation."
      >
        <div className="space-y-3">
          {tokens.length === 0 ? (
            <p className="text-sm text-text-secondary">No tokens yet.</p>
          ) : (
            <div className="divide-y divide-border border border-border rounded-md overflow-hidden">
              {tokens.map(t => (
                <div key={t.id} className="flex items-center gap-3 px-3 py-2.5 bg-surface">
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium text-text-primary">{t.name}</div>
                    <div className="text-xs text-text-secondary mt-0.5">
                      {t.token_hint} · created {new Date(t.created_at).toLocaleDateString()}
                    </div>
                  </div>
                  <Btn
                    variant="danger"
                    onClick={() => api.revokeToken(t.id).then(() => {
                      setTokens(prev => prev.filter(x => x.id !== t.id));
                      toast('ok', 'Token revoked');
                    })}
                  >
                    Revoke
                  </Btn>
                </div>
              ))}
            </div>
          )}

          {newToken && (
            <div className="bg-status-green/5 border border-status-green/20 rounded-md p-3">
              <p className="text-xs text-status-green mb-2">Token created. Copy it now: it won&apos;t be shown again.</p>
              <code className="text-xs text-text-primary font-mono break-all block mb-3">{newToken}</code>
              <Btn onClick={() => { navigator.clipboard.writeText(newToken); toast('ok', 'Copied'); }}>Copy</Btn>
            </div>
          )}

          <div className="flex gap-2 pt-1">
            <Input
              placeholder="Token name e.g. ci-deploy"
              value={tokenName}
              onChange={e => setTokenName(e.target.value)}
            />
            <Btn
              variant="primary"
              disabled={!tokenName}
              onClick={() => setSudo({
                title: 'Generate permanent token',
                onConfirm: handleGenerateToken,
              })}
            >
              Generate
            </Btn>
          </div>
        </div>
      </SectionCard>

      {/* 3. Users */}
      <SectionCard
        title="Users"
        description="Manage access. Admins have full control, operators can trigger actions, viewers are read-only."
      >
        <div className="space-y-3">
          <div className="divide-y divide-border border border-border rounded-md overflow-hidden">
            {users.map(u => (
              <div key={u.id} className="flex items-center gap-3 px-3 py-2.5 bg-surface">
                <div className="w-6 h-6 rounded-full bg-accent/10 flex items-center justify-center flex-shrink-0">
                  <span className="text-accent text-[10px] font-semibold uppercase">
                    {u.username[0]}
                  </span>
                </div>
                <div className="flex-1 min-w-0">
                  <div className="text-xs font-medium text-text-primary">{u.username}</div>
                  <div className="text-xs text-text-secondary mt-0.5">{u.email}</div>
                </div>
                <StatusBadge status={u.role} />
                <select
                  value={u.role}
                  onChange={e => handleChangeRole(u.id, e.target.value)}
                  disabled={u.id === user?.id}
                  className="h-7 px-2 bg-background border border-border rounded text-xs text-text-secondary font-mono focus:outline-none disabled:opacity-40"
                >
                  <option value="admin">admin</option>
                  <option value="operator">operator</option>
                  <option value="viewer">viewer</option>
                </select>
                <Btn
                  variant="default"
                  onClick={() => { setPwTarget(u); setNewPw(''); }}
                >
                  Password
                </Btn>
                {u.id !== user?.id && (
                  <Btn variant="danger" onClick={() => handleDeleteUser(u.id)}>
                    Delete
                  </Btn>
                )}
              </div>
            ))}
          </div>
          <Btn onClick={() => setModal('users')}>Add user</Btn>
        </div>
      </SectionCard>

      {/* 4. Master Node */}
      <SectionCard
        title="Master node"
        description="All projects always migrate to this node when it's online, and never away from it."
      >
        <div className="flex items-center justify-between">
          <div>
            {masterNode ? (
              <div>
                <div className="flex items-center gap-2">
                  <span className={`w-1.5 h-1.5 rounded-full ${masterNode.online ? 'bg-status-green' : 'bg-status-red'}`} />
                  <span className="text-xs font-medium text-text-primary font-mono">{masterNode.hostname}</span>
                </div>
                <p className="text-xs text-text-secondary font-mono mt-1">{masterNode.vpn_ip}</p>
              </div>
            ) : (
              <span className="text-xs text-text-muted">No master node set — failover picks healthiest.</span>
            )}
          </div>
          <div className="flex gap-2">
            <Btn onClick={() => { setSelectedMaster(masterNode?.id ?? ''); setModal('master-node'); }}>
              {masterNode ? 'Change' : 'Set master'}
            </Btn>
            {masterNode && (
              <Btn
                variant="danger"
                loading={actionLoading === 'clear-master'}
                onClick={handleClearMaster}
              >
                Clear
              </Btn>
            )}
          </div>
        </div>
      </SectionCard>

      {/* 5. Nginx */}
      <SectionCard
        title="Nginx"
        description="Regenerate and reload the Nginx reverse proxy config from current running projects."
      >
        <div className="flex items-center justify-between">
          <p className="text-xs text-text-secondary">
            Triggers a config update across all nodes with active projects.
          </p>
          <Btn
            variant="primary"
            loading={actionLoading === 'nginx'}
            onClick={handleNginx}
          >
            Update config
          </Btn>
        </div>
      </SectionCard>

      <HubVersion />

      {/* 6. Agents */}
      <SectionCard
        title="Agent update"
        description="Dispatch an update job to all online nodes. Agents will download and restart."
      >
        <div className="flex items-center justify-between">
          <p className="text-xs text-text-secondary">
            Dispatches to {nodes.filter(n => n.online).length} online node
            {nodes.filter(n => n.online).length !== 1 ? 's' : ''}.
          </p>
          <Btn
            variant="primary"
            loading={actionLoading === 'agents'}
            onClick={handleAgentDeploy}
          >
            Deploy agents
          </Btn>
        </div>
      </SectionCard>
        {/* 7. Node Enrollment */}
<SectionCard
  title="Node enrollment"
  description="Generate one-time tokens to onboard new nodes. Run the install command on any Linux server."
>
  <div className="space-y-4">
    {/* Install command */}
    <div className="bg-background border border-border rounded-md p-3">
      <p className="text-xs text-text-secondary mb-2">Install command</p>
      <div className="flex items-center gap-2">
        <code className="text-xs text-text-primary font-mono flex-1 truncate">
          {installCmd}
        </code>
        <Btn
          onClick={() => {
            navigator.clipboard.writeText(installCmd);
            toast('ok', 'Copied');
          }}
        >
          Copy
        </Btn>
      </div>
    </div>

    {/* Token list */}
    {enrollmentTokens.length > 0 && (
      <div className="divide-y divide-border border border-border rounded-md overflow-hidden">
        {enrollmentTokens.map(t => {
          const expired = new Date(t.expires_at) < new Date();
          return (
            <div key={t.id} className="flex items-center gap-3 px-3 py-2.5 bg-surface">
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
                  <code className="text-xs text-accent font-mono bg-accent/5 border border-accent/20 px-2 py-0.5 rounded">
                    {t.token}
                  </code>
                  <Btn onClick={() => { navigator.clipboard.writeText(t.token); toast('ok', 'Token copied'); }}>Copy</Btn>
                </div>
              )}
              <StatusBadge status={t.used ? 'completed' : expired ? 'failed' : 'running'} label={t.used ? 'used' : expired ? 'expired' : 'active'} />
              {!t.used && (
                <Btn
                  variant="danger"
                  onClick={() => api.revokeEnrollmentToken(t.id).then(() => {
                    setEnrollmentTokens(prev => prev.filter(x => x.id !== t.id));
                    toast('ok', 'Token revoked');
                  })}
                >
                  Revoke
                </Btn>
              )}
            </div>
          );
        })}
      </div>
    )}

    {/* Create token */}
    <form onSubmit={handleCreateEnrollmentToken} noValidate className="flex gap-2">
      <Input
        placeholder="Label e.g. home-server"
        value={enrollLabel}
        onChange={e => setEnrollLabel(e.target.value)}
      />
      <Btn
        variant="primary"
        type="submit"
        loading={actionLoading === 'enroll-token'}
        disabled={!enrollLabel}
      >
        Generate
      </Btn>
    </form>
  </div>
</SectionCard>
      {/* ---- Modals ---- */}

      {/* Sudo modal */}
      {sudo && (
        <SudoModal
          title={sudo.title}
          onConfirm={sudo.onConfirm}
          onCancel={() => setSudo(null)}
        />
      )}


      {/* Create user modal */}
      {modal === 'users' && (
        <Modal title="Add user" onClose={() => setModal(null)}>
          <form onSubmit={handleCreateUser} className="space-y-4">
            <Field label="Username">
              <Input
                placeholder="johndoe"
                value={newUser.username}
                onChange={e => setNewUser(p => ({ ...p, username: e.target.value }))}
                required
              />
            </Field>
            <Field label="Email">
              <Input
                type="email"
                placeholder="john@example.com"
                value={newUser.email}
                onChange={e => setNewUser(p => ({ ...p, email: e.target.value }))}
                required
              />
            </Field>
            <Field label="Password">
              <Input
                type="password"
                placeholder="••••••••"
                value={newUser.password}
                onChange={e => setNewUser(p => ({ ...p, password: e.target.value }))}
                required
              />
            </Field>
            <Field label="Role">
              <select
                value={newUser.role}
                onChange={e => setNewUser(p => ({ ...p, role: e.target.value }))}
                className="w-full h-9 px-3 bg-surface border border-border rounded-md text-sm text-text-primary font-mono focus:outline-none focus:border-border-strong transition-colors"
              >
                <option value="viewer">viewer</option>
                <option value="operator">operator</option>
                <option value="admin">admin</option>
              </select>
            </Field>
            <div className="flex gap-2 pt-1">
              <Btn variant="default" type="button" onClick={() => setModal(null)}>Cancel</Btn>
              <Btn variant="primary" type="submit" loading={actionLoading === 'create-user'}>
                Create user
              </Btn>
            </div>
          </form>
        </Modal>
      )}

      {/* Change password modal */}
      {pwTarget && (
        <Modal title={`Change password — ${pwTarget.username}`} onClose={() => setPwTarget(null)}>
          <form onSubmit={handleChangePassword} className="space-y-4">
            <Field label="New password">
              <Input
                type="password"
                placeholder="••••••••"
                value={newPw}
                onChange={e => setNewPw(e.target.value)}
                required
                autoFocus
              />
            </Field>
            <div className="flex gap-2 pt-1">
              <Btn variant="default" type="button" onClick={() => setPwTarget(null)}>Cancel</Btn>
              <Btn variant="primary" type="submit" loading={actionLoading === 'change-pw'}>
                Update
              </Btn>
            </div>
          </form>
        </Modal>
      )}

      {/* Master node modal */}
      {modal === 'master-node' && (
        <Modal title="Set master node" onClose={() => setModal(null)}>
          <div className="space-y-4">
            <Field label="Node">
              <select
                value={selectedMaster}
                onChange={e => setSelectedMaster(e.target.value)}
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
              All projects will migrate to this node when it comes online.
              No project will ever be migrated away from it.
            </p>
            <div className="flex gap-2 pt-1">
              <Btn variant="default" onClick={() => setModal(null)}>Cancel</Btn>
              <Btn
                variant="primary"
                disabled={!selectedMaster}
                loading={actionLoading === 'master'}
                onClick={handleSetMaster}
              >
                Set master
              </Btn>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}