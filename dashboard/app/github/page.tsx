'use client';

import { useEffect, useState } from 'react';
import { api } from '../../lib/api';
import { AllowedRepo, GitHubToken, OIDCDeployment } from '../../types';
import { Github, RefreshCw, X, Plus, Trash2, Key } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Button } from '@/components/ui/Button';
import { Badge, StatusBadge } from '@/components/ui/Badge';
import { Card, EmptyState } from '@/components/ui/Card';
import { Field, inputClass } from '@/components/ui/Modal';

const hubUrl = typeof window !== 'undefined' && !window.location.hostname.includes('localhost')
  ? window.location.origin
  : process.env.NEXT_PUBLIC_HUB_URL || 'https://your-hub-url';

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
  const [names, setNames]         = useState<Record<string, string>>({});
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
      const [nodes, projects] = await Promise.all([
        api.getNodes().catch(() => []),
        api.getProjects(1, 200).then(r => r.data).catch(() => []),
      ]);
      setNames(Object.fromEntries([...nodes.map(n => [n.id, n.hostname]), ...projects.map(p => [p.id, p.name])]));
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
    return <div className="flex items-center justify-center h-96 text-sm text-text-secondary">Loading…</div>;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        icon={Github}
        title="GitHub"
        description="Deploy from GitHub Actions: a push builds your image and the Hub deploys it. No deploy keys; the Hub checks the workflow's identity with GitHub."
        actions={<Button variant="ghost" icon={RefreshCw} onClick={loadData}>Refresh</Button>}
      />

      {error && (
        <div className="bg-status-red/10 border border-status-red/30 rounded-lg px-4 py-3 text-sm text-status-red flex items-center justify-between">
          <span>{error}</span>
          <button onClick={() => setError(null)} aria-label="Dismiss" className="ml-3 hover:opacity-70"><X className="h-3.5 w-3.5" /></button>
        </div>
      )}

      <Card
        title="Authorized repositories"
        description="Only these repositories can deploy. A project is created on a repository's first deploy."
        flush
        actions={
          <Button icon={showRepoForm ? X : Plus} onClick={() => setShowRepoForm(f => !f)}>
            {showRepoForm ? 'Cancel' : 'Add repository'}
          </Button>
        }
      >
        {showRepoForm && (
          <div className="p-5 border-b border-border space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Field label="Repository">
                <input value={repoForm.repository} onChange={e => setRepoForm(f => ({ ...f, repository: e.target.value }))} placeholder="owner/repo" className={`${inputClass} font-mono`} />
              </Field>
              <Field label="Environment">
                <input value={repoForm.environment} onChange={e => setRepoForm(f => ({ ...f, environment: e.target.value }))} placeholder="production" className={`${inputClass} font-mono`} />
              </Field>
            </div>
            <div className="flex justify-end">
              <Button variant="primary" loading={saving} disabled={!repoFormValid} onClick={handleAddRepo}>Authorize</Button>
            </div>
          </div>
        )}
        {allowed.length === 0 ? (
          <EmptyState>No repositories yet. Add one to let it deploy.</EmptyState>
        ) : (
          <div className="divide-y divide-border">
            {allowed.map(r => (
              <div key={r.id} className="flex items-center justify-between px-5 py-3">
                <div className="flex items-center gap-2 min-w-0">
                  <span className="text-sm text-text-primary font-mono truncate">{r.repository}</span>
                  <Badge>{r.environment}</Badge>
                  {!r.enabled && <Badge tone="danger">disabled</Badge>}
                </div>
                <Button variant="ghost" icon={Trash2} aria-label="Remove" className="hover:text-status-red" loading={deletingId === r.id} onClick={() => handleRemoveRepo(r.id)} />
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card
        title="Registry tokens"
        description={<>GitHub tokens nodes use to pull private images from GHCR. They need the <span className="font-mono">read:packages</span> scope.</>}
        flush
        actions={
          <Button icon={showTokenForm ? X : Plus} onClick={() => setShowTokenForm(f => !f)}>
            {showTokenForm ? 'Cancel' : 'Add token'}
          </Button>
        }
      >
        {showTokenForm && (
          <div className="p-5 border-b border-border space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <Field label="Label">
                <input value={tokenForm.label} onChange={e => setTokenForm(f => ({ ...f, label: e.target.value }))} placeholder="e.g. personal" className={inputClass} />
              </Field>
              <Field label="Token">
                <input type="password" value={tokenForm.token} onChange={e => setTokenForm(f => ({ ...f, token: e.target.value }))} placeholder="ghp_…" className={`${inputClass} font-mono`} />
              </Field>
            </div>
            <div className="flex justify-end">
              <Button variant="primary" loading={saving} disabled={!tokenFormValid} onClick={handleAddToken}>Add token</Button>
            </div>
          </div>
        )}
        {tokens.length === 0 ? (
          <EmptyState>No tokens. Public images don&apos;t need one.</EmptyState>
        ) : (
          <div className="divide-y divide-border">
            {tokens.map(t => (
              <div key={t.id} className="flex items-center justify-between px-5 py-3">
                <div className="flex items-center gap-3">
                  <Key className="h-4 w-4 text-text-muted flex-shrink-0" />
                  <div>
                    <div className="text-sm text-text-primary">{t.label}</div>
                    <div className="text-xs text-text-secondary font-mono mt-0.5">{t.token}</div>
                  </div>
                </div>
                <Button variant="ghost" icon={Trash2} aria-label="Remove" className="hover:text-status-red" loading={deletingId === t.id} onClick={() => handleRemoveToken(t.id)} />
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card title="Deploy history" description="Every deploy GitHub Actions asked for." flush>
        {history.length === 0 ? (
          <EmptyState>No deploys yet. They appear here after your workflow runs.</EmptyState>
        ) : (
          <div className="divide-y divide-border">
            {history.map(d => (
              <div key={d.id} className="flex items-start justify-between gap-4 px-5 py-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-sm text-text-primary font-mono truncate">{d.repository}</span>
                    <StatusBadge status={d.status} />
                  </div>
                  <div className="text-xs text-text-secondary mt-1 flex flex-wrap gap-x-2">
                    <span className="font-mono">{shortSHA(d.sha)}</span>
                    <span>·</span>
                    <span>{names[d.project_id] ?? 'deleted project'}</span>
                    <span>·</span>
                    <span>{names[d.node_id] ?? 'removed node'}</span>
                  </div>
                  {d.error && <div className="mt-1 text-xs text-status-red font-mono truncate max-w-xl">{d.error}</div>}
                </div>
                <div className="flex-shrink-0 text-xs text-text-secondary">{timeAgo(d.created_at)}</div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card
        title="Workflow"
        description={
          <>
            Add this to <span className="font-mono">.github/workflows/deploy.yml</span> in an authorized repository. No secrets needed beyond{' '}
            <span className="font-mono">GITHUB_TOKEN</span>.
          </>
        }
      >
        <pre className="bg-background border border-border rounded-md p-4 text-xs text-text-secondary font-mono overflow-x-auto leading-relaxed">{`permissions:
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
      </Card>
    </div>
  );
}
