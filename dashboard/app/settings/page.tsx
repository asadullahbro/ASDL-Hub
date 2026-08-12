'use client';

import { useEffect, useState, useCallback } from 'react';
import { api } from '@/lib/api';
import { useAuth } from '@/components/providers/AuthProvider';
import { Node, User, PermanentToken, EnrollmentToken } from '@/types';

// --- Types ---
type Section = 'tokens' | 'github' | 'users' | 'master-node' | 'nginx' | 'agents';

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

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70">
      <div className="bg-surface border border-border rounded-lg w-full max-w-sm p-6">
        <h2 className="text-sm font-semibold text-text-primary mb-1">{title}</h2>
        <p className="text-xs text-text-muted mb-5">Confirm your admin password to continue.</p>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1.5">
              Password
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full h-9 px-3 bg-background border border-border rounded-md text-sm text-text-primary font-mono focus:outline-none focus:border-border-strong focus:ring-1 focus:ring-accent/20 transition-colors"
              autoFocus
              required
            />
          </div>
          {error && (
            <p className="text-xs text-status-red font-mono">{error}</p>
          )}
          <div className="flex gap-2 pt-1">
            <button
              type="button"
              onClick={onCancel}
              className="flex-1 h-9 border border-border rounded-md text-xs text-text-secondary hover:bg-surface-hover transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={loading}
              className="flex-1 h-9 bg-accent text-black text-xs font-semibold rounded-md hover:bg-accent-hover transition-colors disabled:opacity-50"
            >
              {loading ? 'Verifying...' : 'Confirm'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// --- Generic Modal ---
function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70">
      <div className="bg-surface border border-border rounded-lg w-full max-w-sm p-6">
        <div className="flex items-center justify-between mb-5">
          <h2 className="text-sm font-semibold text-text-primary">{title}</h2>
          <button
            onClick={onClose}
            className="text-text-muted hover:text-text-primary transition-colors text-lg leading-none"
          >
            ×
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

// --- Section wrapper ---
function SectionCard({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="border border-border rounded-lg overflow-hidden">
      <div className="px-5 py-4 border-b border-border bg-surface">
        <h2 className="text-sm font-medium text-text-primary">{title}</h2>
        {description && (
          <p className="text-xs text-text-muted mt-0.5">{description}</p>
        )}
      </div>
      <div className="p-5 bg-background">{children}</div>
    </div>
  );
}

// --- Field ---
function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label className="block text-[10px] uppercase tracking-wider text-text-secondary mb-1.5">
        {label}
      </label>
      {children}
    </div>
  );
}

function Input({
  ...props
}: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={`w-full h-9 px-3 bg-surface border border-border rounded-md text-sm text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:border-border-strong focus:ring-1 focus:ring-accent/20 transition-colors ${props.className ?? ''}`}
    />
  );
}

function Btn({
  variant = 'default',
  loading,
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'default' | 'primary' | 'danger';
  loading?: boolean;
}) {
  const base = 'h-9 px-4 rounded-md text-xs font-medium transition-colors disabled:opacity-50 flex items-center gap-2';
  const variants = {
    default: 'border border-border text-text-secondary hover:bg-surface-hover hover:text-text-primary',
    primary: 'bg-accent text-black font-semibold hover:bg-accent-hover',
    danger: 'border border-status-red/30 text-status-red hover:bg-status-red/10',
  };
  return (
    <button {...props} disabled={loading || props.disabled} className={`${base} ${variants[variant]}`}>
      {loading ? <span className="animate-spin">↻</span> : null}
      {children}
    </button>
  );
}

// ============================================================
// MAIN PAGE
// ============================================================
export default function SettingsPage() {
  const { user } = useAuth();
  const [nodes, setNodes] = useState<Node[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [tokens, setTokens] = useState<PermanentToken[]>([]);
  const [masterNode, setMasterNode] = useState<Node | null>(null);
  const [githubSet, setGithubSet] = useState(false);
  const [githubHint, setGithubHint] = useState('');
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
  const [githubToken, setGithubToken] = useState('');
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
    const [nodesRes, usersRes, tokensRes, masterRes, ghRes, enrollRes] = await Promise.allSettled([
        api.getNodes(),
        api.listUsers(),
        api.listTokens(),
        api.getMasterNode(),
        api.getGitHubToken(),
        api.listEnrollmentTokens(),
]);

    if (enrollRes.status === 'fulfilled') setEnrollmentTokens(enrollRes.value);
    if (nodesRes.status === 'fulfilled') setNodes(nodesRes.value);
    if (usersRes.status === 'fulfilled') setUsers(usersRes.value);
    if (tokensRes.status === 'fulfilled') setTokens(tokensRes.value);
    if (masterRes.status === 'fulfilled') setMasterNode(masterRes.value.master_node);
    if (ghRes.status === 'fulfilled') {
      setGithubSet(ghRes.value.set);
      setGithubHint(ghRes.value.token);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  // --- Token generation ---
  const handleGenerateToken = async (password: string) => {
    const res = await api.generateToken(tokenName, password);
    setNewToken(res.token);
    setTokens(prev => [res.meta, ...prev]);
    setTokenName('');
    setSudo(null);
  };

  // --- GitHub token ---
  const handleSetGitHub = async (password: string) => {
    await api.setGitHubToken(githubToken, password);
    setGithubSet(true);
    setGithubHint('••••••••' + githubToken.slice(-4));
    setGithubToken('');
    setModal(null);
    setSudo(null);
    toast('ok', 'GitHub token saved');
  };

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

  const roleBadge = (role: string) => {
    const map: Record<string, string> = {
      admin:    'badge-completed',
      operator: 'badge-running',
      viewer:   'badge-pending',
    };
    return map[role] ?? 'badge-pending';
  };

  return (
    <div className="space-y-6 max-w-2xl">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-base font-medium text-text-primary">Settings</h1>
        {feedback && (
          <div className={`text-xs font-mono px-3 py-1.5 rounded-md border ${
            feedback.type === 'ok'
              ? 'bg-status-green/10 text-status-green border-status-green/20'
              : 'bg-status-red/10 text-status-red border-status-red/20'
          }`}>
            {feedback.msg}
          </div>
        )}
      </div>

      {/* 1. Permanent Tokens */}
      <SectionCard
        title="Permanent tokens"
        description="Long-lived API tokens for agents and integrations. Only shown once on creation."
      >
        <div className="space-y-3">
          {tokens.length === 0 ? (
            <p className="text-xs text-text-muted font-mono">No tokens yet.</p>
          ) : (
            <div className="divide-y divide-border border border-border rounded-md overflow-hidden">
              {tokens.map(t => (
                <div key={t.id} className="flex items-center gap-3 px-3 py-2.5 bg-surface">
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium text-text-primary">{t.name}</div>
                    <div className="text-[10px] text-text-muted font-mono mt-0.5">
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
              <p className="text-[10px] uppercase tracking-wider text-status-green mb-1.5">
                Token generated — copy it now, it won't be shown again
              </p>
              <code className="text-xs text-text-primary font-mono break-all">{newToken}</code>
              <button
                onClick={() => { navigator.clipboard.writeText(newToken); toast('ok', 'Copied'); }}
                className="mt-2 text-[10px] text-accent hover:underline block"
              >
                Copy to clipboard
              </button>
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

      {/* 2. GitHub Token */}
      <SectionCard
        title="GitHub token"
        description="Used for pulling private repositories during agent deployments."
      >
        <div className="flex items-center justify-between">
          <div>
            <div className="flex items-center gap-2">
              <span className={`w-1.5 h-1.5 rounded-full ${githubSet ? 'bg-status-green' : 'bg-text-muted'}`} />
              <span className="text-xs text-text-secondary">
                {githubSet ? 'Token configured' : 'Not configured'}
              </span>
            </div>
            {githubHint && (
              <p className="text-[10px] text-text-muted font-mono mt-1">{githubHint}</p>
            )}
          </div>
          <Btn onClick={() => setModal('github')}>
            {githubSet ? 'Rotate' : 'Set token'}
          </Btn>
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
                  <div className="text-[10px] text-text-muted mt-0.5">{u.email}</div>
                </div>
                <span className={`badge ${roleBadge(u.role)}`}>{u.role}</span>
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
                <p className="text-[10px] text-text-muted font-mono mt-1">{masterNode.vpn_ip}</p>
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
          <p className="text-xs text-text-muted">
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

      {/* 6. Agents */}
      <SectionCard
        title="Agent update"
        description="Dispatch an update job to all online nodes. Agents will download and restart."
      >
        <div className="flex items-center justify-between">
          <p className="text-xs text-text-muted">
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
    <div className="bg-surface border border-border rounded-md p-3">
      <p className="text-[10px] uppercase tracking-wider text-text-muted mb-2">Install command</p>
      <div className="flex items-center gap-2">
        <code className="text-xs text-text-primary font-mono flex-1 truncate">
          curl -fsSL https://hub.asdl.website/install | sudo bash
        </code>
        <button
          onClick={() => {
            navigator.clipboard.writeText('curl -fsSL https://hub.asdl.website/install | sudo bash');
            toast('ok', 'Copied');
          }}
          className="text-[10px] text-accent hover:underline flex-shrink-0"
        >
          Copy
        </button>
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
                <div className="text-[10px] text-text-muted font-mono mt-0.5">
                  {t.used
                    ? `used · node ${t.used_by.slice(0, 8)}`
                    : expired
                    ? 'expired'
                    : `expires ${new Date(t.expires_at).toLocaleString()}`}
                </div>
              </div>
              {!t.used && !expired && (
                <div className="flex items-center gap-2">
                  <code className="text-[10px] text-accent font-mono bg-accent/5 border border-accent/20 px-2 py-0.5 rounded">
                    {t.token}
                  </code>
                  <button
                    onClick={() => { navigator.clipboard.writeText(t.token); toast('ok', 'Token copied'); }}
                    className="text-[10px] text-text-muted hover:text-accent transition-colors"
                  >
                    Copy
                  </button>
                </div>
              )}
              <span className={`badge ${
                t.used ? 'badge-completed' :
                expired ? 'badge-failed' :
                'badge-running'
              }`}>
                {t.used ? 'used' : expired ? 'expired' : 'active'}
              </span>
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

      {/* GitHub token modal */}
      {modal === 'github' && (
        <Modal title={githubSet ? 'Rotate GitHub token' : 'Set GitHub token'} onClose={() => setModal(null)}>
          <div className="space-y-4">
            <Field label="Personal access token">
              <Input
                type="password"
                placeholder="ghp_••••••••••••••••••••••••••••••••••••••••"
                value={githubToken}
                onChange={e => setGithubToken(e.target.value)}
              />
            </Field>
            <div className="flex gap-2 pt-1">
              <Btn variant="default" onClick={() => setModal(null)}>Cancel</Btn>
              <Btn
                variant="primary"
                disabled={!githubToken}
                onClick={() => setSudo({
                  title: 'Confirm GitHub token update',
                  onConfirm: handleSetGitHub,
                })}
              >
                Save
              </Btn>
            </div>
          </div>
        </Modal>
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
            <p className="text-[10px] text-text-muted">
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