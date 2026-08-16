'use client';

import { useEffect, useState } from 'react';
import { api } from '../../lib/api';
import { AllowedRepo, GitHubToken, OIDCDeployment } from '../../types';
import { Github, RefreshCw, X, Plus, Trash2, Key } from 'lucide-react';

const hubUrl = typeof window !== 'undefined' && !window.location.hostname.includes('localhost')
  ? window.location.origin
  : process.env.NEXT_PUBLIC_HUB_URL || 'https://your-hub-url';

function statusDot(status: string) {
  const map: Record<string, string> = {
    dispatched: 'bg-status-green',
    pending:    'bg-status-yellow',
    failed:     'bg-status-red',
  };
  return map[status] ?? 'bg-text-muted';
}

function statusText(status: string) {
  const map: Record<string, string> = {
    dispatched: 'text-status-green',
    pending:    'text-status-yellow',
    failed:     'text-status-red',
  };
  return map[status] ?? 'text-text-muted';
}

function shortSHA(sha: string) {
  return sha?.slice(0, 7) ?? '—';
}

function timeAgo(dateStr: string) {
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

export default function GitHubPage() {
  const [allowed, setAllowed]     = useState<AllowedRepo[]>([]);
  const [tokens, setTokens]       = useState<GitHubToken[]>([]);
  const [history, setHistory]     = useState<OIDCDeployment[]>([]);
  const [loading, setLoading]     = useState(true);
  const [error, setError]         = useState<string | null>(null);

  const [showRepoForm, setShowRepoForm]   = useState(false);
  const [showTokenForm, setShowTokenForm] = useState(false);
  const [saving, setSaving]               = useState(false);
  const [deletingId, setDeletingId]       = useState<string | null>(null);

  const [repoForm, setRepoForm] = useState({ repository: '', environment: 'production' });
  const [tokenForm, setTokenForm] = useState({ label: '', token: '' });

  async function loadData() {
    try {
      const [allowedData, tokensData, historyData] = await Promise.all([
        api.listAllowed(),
        api.listGitHubTokens(),
        api.listDeployHistory(),
      ]);
      setAllowed(allowedData ?? []);
      setTokens(tokensData ?? []);
      setHistory(historyData ?? []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { loadData(); }, []);

  const handleAddRepo = async () => {
    setSaving(true);
    try {
      await api.addAllowed(repoForm);
      setShowRepoForm(false);
      setRepoForm({ repository: '', environment: 'production' });
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add');
    } finally {
      setSaving(false);
    }
  };

  const handleAddToken = async () => {
    setSaving(true);
    try {
      await api.addGitHubToken(tokenForm);
      setShowTokenForm(false);
      setTokenForm({ label: '', token: '' });
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add token');
    } finally {
      setSaving(false);
    }
  };

  const handleRemoveRepo = async (id: string) => {
    setDeletingId(id);
    try {
      await api.removeAllowed(id);
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove');
    } finally {
      setDeletingId(null);
    }
  };

  const handleRemoveToken = async (id: string) => {
    setDeletingId(id);
    try {
      await api.removeGitHubToken(id);
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove token');
    } finally {
      setDeletingId(null);
    }
  };

  const repoFormValid = repoForm.repository.includes('/') && repoForm.environment;
  const tokenFormValid = tokenForm.label && tokenForm.token;

  if (loading) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted text-sm">Loading...</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">

      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Github className="h-5 w-5 text-text-primary" />
          <h1 className="text-xl font-semibold text-text-primary">GitHub Actions</h1>
        </div>
        <button
          onClick={loadData}
          className="p-1.5 rounded text-text-muted hover:text-text-primary hover:bg-surface-hover transition-colors"
          title="Refresh"
        >
          <RefreshCw className="h-4 w-4" />
        </button>
      </div>

      {error && (
        <div className="bg-status-red/10 border border-status-red/30 rounded-lg px-4 py-3 text-sm text-status-red flex items-center justify-between">
          <span>{error}</span>
          <button onClick={() => setError(null)} className="ml-3 hover:opacity-70 transition-opacity">
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      )}

      {/* Authorized Repositories */}
      <div className="bg-surface border border-border rounded-lg">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div>
            <span className="text-sm font-medium text-text-primary">Authorized Repositories</span>
            <p className="text-xs text-text-muted mt-0.5">
              Only listed repositories can deploy. Projects are created automatically on first deploy.
            </p>
          </div>
          <button
            onClick={() => setShowRepoForm(f => !f)}
            className="flex items-center gap-1.5 text-xs text-accent hover:text-accent-hover transition-colors"
          >
            {showRepoForm ? <><X className="h-3 w-3" /> Cancel</> : <><Plus className="h-3 w-3" /> Add</>}
          </button>
        </div>

        {showRepoForm && (
          <div className="p-5 border-b border-border space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs text-text-muted mb-1">Repository</label>
                <input
                  type="text"
                  value={repoForm.repository}
                  onChange={e => setRepoForm(f => ({ ...f, repository: e.target.value }))}
                  placeholder="owner/repo"
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent font-mono"
                />
              </div>
              <div>
                <label className="block text-xs text-text-muted mb-1">Environment</label>
                <input
                  type="text"
                  value={repoForm.environment}
                  onChange={e => setRepoForm(f => ({ ...f, environment: e.target.value }))}
                  placeholder="production"
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent font-mono"
                />
              </div>
            </div>
            <div className="flex justify-end gap-3">
              <button
                onClick={() => setShowRepoForm(false)}
                className="text-sm text-text-muted hover:text-text-primary transition-colors px-3 py-1.5"
              >
                Cancel
              </button>
              <button
                onClick={handleAddRepo}
                disabled={saving || !repoFormValid}
                className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {saving ? 'Saving...' : 'Authorize'}
              </button>
            </div>
          </div>
        )}

        {allowed.length === 0 ? (
          <div className="px-5 py-10 text-center">
            <p className="text-sm text-text-muted">No repositories authorized</p>
            <p className="text-xs text-text-secondary mt-1">
              Add a repository to allow it to deploy via OIDC.
            </p>
          </div>
        ) : (
          <div className="divide-y divide-border">
            {allowed.map(r => (
              <div key={r.id} className="flex items-center justify-between px-5 py-3.5 group">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="text-sm text-text-primary font-mono">{r.repository}</span>
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-background border border-border text-text-muted font-mono">
                      {r.environment}
                    </span>
                    {!r.enabled && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-status-red/10 border border-status-red/20 text-status-red">
                        disabled
                      </span>
                    )}
                  </div>
                </div>
                <button
                  onClick={() => handleRemoveRepo(r.id)}
                  disabled={deletingId === r.id}
                  className="opacity-0 group-hover:opacity-100 text-text-muted hover:text-status-red transition-all p-1.5 rounded hover:bg-status-red/10 disabled:opacity-50"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* GitHub Tokens */}
      <div className="bg-surface border border-border rounded-lg">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div>
            <span className="text-sm font-medium text-text-primary">GitHub Tokens</span>
            <p className="text-xs text-text-muted mt-0.5">
              PATs used to pull private images from GHCR on your nodes. Needs <span className="font-mono">read:packages</span> scope.
            </p>
          </div>
          <button
            onClick={() => setShowTokenForm(f => !f)}
            className="flex items-center gap-1.5 text-xs text-accent hover:text-accent-hover transition-colors"
          >
            {showTokenForm ? <><X className="h-3 w-3" /> Cancel</> : <><Plus className="h-3 w-3" /> Add</>}
          </button>
        </div>

        {showTokenForm && (
          <div className="p-5 border-b border-border space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs text-text-muted mb-1">Label</label>
                <input
                  type="text"
                  value={tokenForm.label}
                  onChange={e => setTokenForm(f => ({ ...f, label: e.target.value }))}
                  placeholder="e.g. personal, org-bot"
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent"
                />
              </div>
              <div>
                <label className="block text-xs text-text-muted mb-1">Token</label>
                <input
                  type="password"
                  value={tokenForm.token}
                  onChange={e => setTokenForm(f => ({ ...f, token: e.target.value }))}
                  placeholder="ghp_..."
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent font-mono"
                />
              </div>
            </div>
            <div className="flex justify-end gap-3">
              <button
                onClick={() => setShowTokenForm(false)}
                className="text-sm text-text-muted hover:text-text-primary transition-colors px-3 py-1.5"
              >
                Cancel
              </button>
              <button
                onClick={handleAddToken}
                disabled={saving || !tokenFormValid}
                className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {saving ? 'Saving...' : 'Add Token'}
              </button>
            </div>
          </div>
        )}

        {tokens.length === 0 ? (
          <div className="px-5 py-10 text-center">
            <p className="text-sm text-text-muted">No tokens added</p>
            <p className="text-xs text-text-secondary mt-1">
              Add a PAT to pull private images from GHCR during deployment.
            </p>
          </div>
        ) : (
          <div className="divide-y divide-border">
            {tokens.map(t => (
              <div key={t.id} className="flex items-center justify-between px-5 py-3.5 group">
                <div className="flex items-center gap-3">
                  <Key className="h-3.5 w-3.5 text-text-muted flex-shrink-0" />
                  <div>
                    <span className="text-sm text-text-primary">{t.label}</span>
                    <div className="text-xs text-text-muted font-mono mt-0.5">{t.token}</div>
                  </div>
                </div>
                <button
                  onClick={() => handleRemoveToken(t.id)}
                  disabled={deletingId === t.id}
                  className="opacity-0 group-hover:opacity-100 text-text-muted hover:text-status-red transition-all p-1.5 rounded hover:bg-status-red/10 disabled:opacity-50"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Deployment History */}
      <div className="bg-surface border border-border rounded-lg">
        <div className="px-5 py-4 border-b border-border">
          <span className="text-sm font-medium text-text-primary">Deployment History</span>
          <p className="text-xs text-text-muted mt-0.5">Audit log of all OIDC-authenticated deployments.</p>
        </div>

        {history.length === 0 ? (
          <div className="px-5 py-10 text-center">
            <p className="text-sm text-text-muted">No deployments yet</p>
            <p className="text-xs text-text-secondary mt-1">
              Deployments triggered from GitHub Actions will appear here.
            </p>
          </div>
        ) : (
          <div className="divide-y divide-border">
            {history.map(d => (
              <div key={d.id} className="flex items-center justify-between px-5 py-3.5">
                <div className="flex items-center gap-3 min-w-0">
                  <div className={`h-2 w-2 rounded-full flex-shrink-0 ${statusDot(d.status)}`} />
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-text-primary font-mono truncate">{d.repository}</span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-background border border-border text-text-muted font-mono flex-shrink-0">
                        {d.environment}
                      </span>
                    </div>
                    <div className="flex items-center gap-1.5 mt-0.5 text-xs text-text-muted">
                      <span className={statusText(d.status)}>{d.status}</span>
                      <span className="text-border">·</span>
                      <span className="font-mono">{shortSHA(d.sha)}</span>
                      <span className="text-border">·</span>
                      <span className="font-mono truncate max-w-[200px]">{d.image}</span>
                    </div>
                    <div className="flex items-center gap-1.5 mt-0.5 text-xs text-text-muted">
                      <span className="font-mono">project: {d.project_id.slice(0, 8)}</span>
                      <span className="text-border">·</span>
                      <span className="font-mono">node: {d.node_id.slice(0, 8)}</span>
                    </div>
                    {d.error && (
                      <div className="mt-1 text-xs text-status-red font-mono truncate max-w-sm">
                        {d.error}
                      </div>
                    )}
                  </div>
                </div>
                <div className="flex-shrink-0 text-xs text-text-muted ml-4">
                  {timeAgo(d.created_at)}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Workflow snippet */}
      <div className="bg-surface border border-border rounded-lg">
        <div className="px-5 py-4 border-b border-border">
          <span className="text-sm font-medium text-text-primary">Workflow Setup</span>
          <p className="text-xs text-text-muted mt-0.5">
            Add this to your GitHub Actions workflow. No secrets required beyond <span className="font-mono">GITHUB_TOKEN</span>.
          </p>
        </div>
        <div className="p-5">
          <pre className="bg-background border border-border rounded p-4 text-xs text-text-secondary font-mono overflow-x-auto leading-relaxed">{`permissions:
  contents: read
  id-token: write
  packages: write

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: \${{ github.actor }}
          password: \${{ secrets.GITHUB_TOKEN }}

      - name: Build and push
        id: build
        run: |
          IMAGE=ghcr.io/\$(echo "\${{ github.repository }}" | tr '[:upper:]' '[:lower:]')
          docker build -t $IMAGE:\${{ github.sha }} .
          docker push $IMAGE:\${{ github.sha }}
          echo "image=$IMAGE:\${{ github.sha }}" >> $GITHUB_OUTPUT

      - name: Get OIDC token
        id: oidc
        run: |
          TOKEN=$(curl -sSL \\
            -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \\
            -H "Accept: application/json; api-version=2.0" \\
            "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=${hubUrl}" \\
            | jq -r '.value')
          echo "token=$TOKEN" >> $GITHUB_OUTPUT

      - name: Deploy
        run: |
          curl -sSf -X POST ${hubUrl}/api/v1/deploy \\
            -H "Content-Type: application/json" \\
            -d '{
              "oidc_token": "\${{ steps.oidc.outputs.token }}",
              "image":      "\${{ steps.build.outputs.image }}"
            }'`}
          </pre>
        </div>
      </div>

    </div>
  );
}