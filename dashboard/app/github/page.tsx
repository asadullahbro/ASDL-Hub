'use client';

import { useEffect, useState } from 'react';
import { api } from '../../lib/api';
import { OIDCDeployment } from '../../types';
import { Github, RefreshCw, X } from 'lucide-react';

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
  const [history, setHistory] = useState<OIDCDeployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function loadData() {
    try {
      const historyData = await api.listDeployHistory();
      setHistory(historyData ?? []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { loadData(); }, []);

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

      {/* Deployment History */}
      <div className="bg-surface border border-border rounded-lg">
        <div className="px-5 py-4 border-b border-border">
          <span className="text-sm font-medium text-text-primary">Deployment History</span>
          <p className="text-xs text-text-muted mt-0.5">
            Deployments triggered from GitHub Actions via OIDC — no secrets needed.
          </p>
        </div>

        {history.length === 0 ? (
          <div className="px-5 py-12 text-center">
            <p className="text-sm text-text-muted">No deployments yet</p>
            <p className="text-xs text-text-secondary mt-1">
              Push to a repo with the workflow configured and it will appear here automatically.
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
            Add this to your GitHub Actions workflow. No secrets required — the hub resolves the project automatically from the repository identity.
          </p>
        </div>
        <div className="p-5">
          <pre className="bg-background border border-border rounded p-4 text-xs text-text-secondary font-mono overflow-x-auto leading-relaxed">{`permissions:
  contents: read
  id-token: write

steps:
  - name: Get OIDC token
    id: oidc
    run: |
      TOKEN=$(curl -sSL \\
        -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \\
        -H "Accept: application/json; api-version=2.0" \\
        "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=\${HUB_URL}" \\
        | jq -r '.value')
      echo "token=$TOKEN" >> $GITHUB_OUTPUT

  - name: Deploy to ASDL Hub
    run: |
      curl -sSf -X POST \${HUB_URL}/api/v1/deploy \\
        -H "Content-Type: application/json" \\
        -d '{
          "oidc_token": "\${{ steps.oidc.outputs.token }}",
          "image":      "registry.example.com/myapp:\${{ github.sha }}"
        }'`}
          </pre>
        </div>
      </div>

    </div>
  );
}