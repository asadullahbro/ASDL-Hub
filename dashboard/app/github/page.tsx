'use client';

import { useEffect, useState } from 'react';
import { api } from '../../lib/api';
import { GitHubInstallation, GitHubRepo, GitHubRepository, Project } from '../../types';
import { Github, Link, RefreshCw, Settings, ChevronDown, ChevronRight } from 'lucide-react';

export default function GitHubPage() {
  const [appConfig, setAppConfig] = useState<{ configured: boolean; app_id: string } | null>(null);
  const [installations, setInstallations] = useState<GitHubInstallation[]>([]);
  const [linkedRepos, setLinkedRepos] = useState<GitHubRepository[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // App config form
  const [showAppForm, setShowAppForm] = useState(false);
  const [appForm, setAppForm] = useState({ app_id: '', private_key: '', webhook_secret: '' });
  const [savingApp, setSavingApp] = useState(false);

  // Installation form
  const [showInstallForm, setShowInstallForm] = useState(false);
  const [installForm, setInstallForm] = useState({ installation_id: '', account_login: '', account_type: 'User' });
  const [savingInstall, setSavingInstall] = useState(false);

  // Repo browser
  const [selectedInstallation, setSelectedInstallation] = useState<GitHubInstallation | null>(null);
  const [availableRepos, setAvailableRepos] = useState<GitHubRepo[]>([]);
  const [loadingRepos, setLoadingRepos] = useState(false);

  // Link repo form
  const [linkingRepo, setLinkingRepo] = useState<GitHubRepo | null>(null);
  const [linkForm, setLinkForm] = useState({ project_id: '', default_branch: '' });
  const [savingLink, setSavingLink] = useState(false);

  async function loadData() {
    try {
      const [config, installs, linked, projectsData] = await Promise.all([
        api.getGitHubAppConfig(),
        api.listGitHubInstallations(),
        api.getLinkedRepos(),
        api.getProjects(),
        ]);
        setAppConfig(config);
        setInstallations(installs ?? []);
        setLinkedRepos(linked ?? []);
        setProjects(projectsData?.data ?? []);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load GitHub data');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadData();
  }, []);

  const handleSaveApp = async () => {
    setSavingApp(true);
    try {
      await api.configureGitHubApp(appForm);
      setShowAppForm(false);
      setAppForm({ app_id: '', private_key: '', webhook_secret: '' });
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to configure app');
    } finally {
      setSavingApp(false);
    }
  };

  const handleRegisterInstallation = async () => {
    setSavingInstall(true);
    try {
      await api.registerGitHubInstallation({
        installation_id: parseInt(installForm.installation_id),
        account_login: installForm.account_login,
        account_type: installForm.account_type,
      });
      setShowInstallForm(false);
      setInstallForm({ installation_id: '', account_login: '', account_type: 'User' });
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to register installation');
    } finally {
      setSavingInstall(false);
    }
  };

  const handleBrowseRepos = async (installation: GitHubInstallation) => {
    if (selectedInstallation?.id === installation.id) {
      setSelectedInstallation(null);
      setAvailableRepos([]);
      return;
    }
    setSelectedInstallation(installation);
    setLoadingRepos(true);
    try {
      const result = await api.listGitHubRepos(installation.installation_id);
      setAvailableRepos(result.repositories);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load repositories');
    } finally {
      setLoadingRepos(false);
    }
  };

  const handleLinkRepo = async () => {
    if (!linkingRepo || !selectedInstallation) return;
    setSavingLink(true);
    try {
      await api.linkGitHubRepo({
        installation_id: selectedInstallation.installation_id,
        repo_id: linkingRepo.id,
        owner: linkingRepo.owner,
        name: linkingRepo.name,
        default_branch: linkForm.default_branch || linkingRepo.default_branch,
        project_id: linkForm.project_id || undefined,
      });
      setLinkingRepo(null);
      setLinkForm({ project_id: '', default_branch: '' });
      await loadData();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to link repository');
    } finally {
      setSavingLink(false);
    }
  };

  const isRepoLinked = (repoId: number) =>
    linkedRepos.some(r => r.repo_id === repoId);

  const getProjectName = (projectId: string) =>
    projects.find(p => p.id === projectId)?.name ?? 'Unknown';

  if (loading) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-text-muted">Loading GitHub integration...</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Github className="h-5 w-5 text-text-primary" />
          <h1 className="text-xl font-semibold text-text-primary">GitHub</h1>
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
        <div className="bg-status-red/10 border border-status-red/30 rounded-lg px-4 py-3 text-sm text-status-red">
          {error}
        </div>
      )}

      {/* App Configuration */}
      <div className="bg-surface border border-border rounded-lg">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div className="flex items-center gap-2">
            <Settings className="h-4 w-4 text-text-muted" />
            <span className="text-sm font-medium text-text-primary">GitHub App</span>
          </div>
          <div className="flex items-center gap-3">
            <div className={`h-2 w-2 rounded-full ${appConfig?.configured ? 'bg-status-green' : 'bg-text-muted'}`} />
            <span className="text-xs text-text-muted">
              {appConfig?.configured ? `App ID: ${appConfig.app_id}` : 'Not configured'}
            </span>
            <button
              onClick={() => setShowAppForm(!showAppForm)}
              className="text-xs text-accent hover:text-accent-hover transition-colors"
            >
              {showAppForm ? 'Cancel' : appConfig?.configured ? 'Reconfigure' : 'Configure'}
            </button>
          </div>
        </div>

        {showAppForm && (
          <div className="p-5 space-y-4">
            <p className="text-xs text-text-muted">
              Create a GitHub App at{' '}
              <a
                href="https://github.com/settings/apps/new"
                target="_blank"
                rel="noreferrer"
                className="text-accent hover:underline"
              >
                github.com/settings/apps/new
              </a>{' '}
              then paste the credentials below.
            </p>
            <div>
              <label className="block text-xs text-text-muted mb-1">App ID</label>
              <input
                type="text"
                value={appForm.app_id}
                onChange={e => setAppForm(f => ({ ...f, app_id: e.target.value }))}
                placeholder="123456"
                className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent font-mono"
              />
            </div>
            <div>
              <label className="block text-xs text-text-muted mb-1">Private Key (PEM)</label>
              <textarea
                value={appForm.private_key}
                onChange={e => setAppForm(f => ({ ...f, private_key: e.target.value }))}
                placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;...&#10;-----END RSA PRIVATE KEY-----"
                rows={6}
                className="w-full bg-background border border-border rounded px-3 py-2 text-xs text-text-primary focus:outline-none focus:border-accent font-mono resize-none"
              />
            </div>
            <div>
              <label className="block text-xs text-text-muted mb-1">Webhook Secret</label>
              <input
                type="password"
                value={appForm.webhook_secret}
                onChange={e => setAppForm(f => ({ ...f, webhook_secret: e.target.value }))}
                placeholder="your-webhook-secret"
                className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent font-mono"
              />
            </div>
            <div className="flex justify-end gap-3">
              <button
                onClick={() => setShowAppForm(false)}
                className="text-sm text-text-muted hover:text-text-primary transition-colors px-3 py-1.5"
              >
                Cancel
              </button>
              <button
                onClick={handleSaveApp}
                disabled={savingApp || !appForm.app_id || !appForm.private_key || !appForm.webhook_secret}
                className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {savingApp ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Installations */}
      <div className="bg-surface border border-border rounded-lg">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <span className="text-sm font-medium text-text-primary">Installations</span>
          {appConfig?.configured && (
            <button
              onClick={() => setShowInstallForm(!showInstallForm)}
              className="text-xs text-accent hover:text-accent-hover transition-colors"
            >
              {showInstallForm ? 'Cancel' : '+ Add Installation'}
            </button>
          )}
        </div>

        {showInstallForm && (
          <div className="p-5 border-b border-border space-y-4">
            <p className="text-xs text-text-muted">
              Install the GitHub App on your account or org, then paste the installation ID here.
              Find it in the App's installation URL:{' '}
              <span className="font-mono text-text-secondary">github.com/settings/installations/{'<id>'}</span>
            </p>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs text-text-muted mb-1">Installation ID</label>
                <input
                  type="text"
                  value={installForm.installation_id}
                  onChange={e => setInstallForm(f => ({ ...f, installation_id: e.target.value }))}
                  placeholder="12345678"
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent font-mono"
                />
              </div>
              <div>
                <label className="block text-xs text-text-muted mb-1">Account Login</label>
                <input
                  type="text"
                  value={installForm.account_login}
                  onChange={e => setInstallForm(f => ({ ...f, account_login: e.target.value }))}
                  placeholder="asadullahbro"
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent"
                />
              </div>
            </div>
            <div>
              <label className="block text-xs text-text-muted mb-1">Account Type</label>
              <select
                value={installForm.account_type}
                onChange={e => setInstallForm(f => ({ ...f, account_type: e.target.value }))}
                className="bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent"
              >
                <option value="User">User</option>
                <option value="Organization">Organization</option>
              </select>
            </div>
            <div className="flex justify-end gap-3">
              <button
                onClick={() => setShowInstallForm(false)}
                className="text-sm text-text-muted hover:text-text-primary transition-colors px-3 py-1.5"
              >
                Cancel
              </button>
              <button
                onClick={handleRegisterInstallation}
                disabled={savingInstall || !installForm.installation_id || !installForm.account_login}
                className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {savingInstall ? 'Registering...' : 'Register'}
              </button>
            </div>
          </div>
        )}

        {installations.length === 0 ? (
          <div className="px-5 py-10 text-center">
            <p className="text-sm text-text-muted">No installations registered</p>
            <p className="text-xs text-text-secondary mt-1">
              {appConfig?.configured
                ? 'Install the GitHub App and add the installation above'
                : 'Configure the GitHub App first'}
            </p>
          </div>
        ) : (
          <div className="divide-y divide-border">
            {installations.map(installation => (
              <div key={installation.id}>
                <div
                  className="flex items-center justify-between px-5 py-3.5 hover:bg-surface-hover transition-colors cursor-pointer"
                  onClick={() => handleBrowseRepos(installation)}
                >
                  <div className="flex items-center gap-3">
                    {selectedInstallation?.id === installation.id
                      ? <ChevronDown className="h-3.5 w-3.5 text-text-muted" />
                      : <ChevronRight className="h-3.5 w-3.5 text-text-muted" />
                    }
                    <div>
                      <span className="text-sm text-text-primary font-medium">{installation.account_login}</span>
                      <span className="ml-2 text-xs text-text-muted">{installation.account_type}</span>
                    </div>
                  </div>
                  <span className="text-xs text-text-muted font-mono">#{installation.installation_id}</span>
                </div>

                {/* Repo browser */}
                {selectedInstallation?.id === installation.id && (
                  <div className="border-t border-border bg-background">
                    {loadingRepos ? (
                      <div className="px-8 py-6 text-xs text-text-muted">Loading repositories...</div>
                    ) : availableRepos.length === 0 ? (
                      <div className="px-8 py-6 text-xs text-text-muted">No repositories accessible</div>
                    ) : (
                      <div className="divide-y divide-border">
                        {availableRepos.map(repo => {
                          const linked = isRepoLinked(repo.id);
                          const linkedEntry = linkedRepos.find(r => r.repo_id === repo.id);
                          return (
                            <div key={repo.id} className="flex items-center justify-between px-8 py-3">
                              <div className="min-w-0">
                                <div className="flex items-center gap-2">
                                  <span className="text-sm text-text-primary">{repo.name}</span>
                                  {repo.private && (
                                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface border border-border text-text-muted">
                                      private
                                    </span>
                                  )}
                                  {linked && (
                                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-accent/10 border border-accent/20 text-accent">
                                      linked
                                    </span>
                                  )}
                                </div>
                                <div className="flex items-center gap-3 mt-0.5">
                                  <span className="text-xs text-text-muted font-mono">{repo.default_branch}</span>
                                  {linked && linkedEntry?.project_id && (
                                    <span className="text-xs text-text-muted">
                                      → {getProjectName(linkedEntry.project_id)}
                                    </span>
                                  )}
                                </div>
                              </div>
                              <button
                                onClick={() => {
                                  setLinkingRepo(repo);
                                  setLinkForm({ project_id: linkedEntry?.project_id ?? '', default_branch: repo.default_branch });
                                }}
                                className="flex items-center gap-1.5 text-xs text-text-muted hover:text-accent transition-colors flex-shrink-0 ml-4"
                              >
                                <Link className="h-3 w-3" />
                                {linked ? 'Relink' : 'Link'}
                              </button>
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Linked Repos */}
      {linkedRepos.length > 0 && (
        <div className="bg-surface border border-border rounded-lg">
          <div className="px-5 py-4 border-b border-border">
            <span className="text-sm font-medium text-text-primary">Linked Repositories</span>
          </div>
          <div className="divide-y divide-border">
            {linkedRepos.map(repo => (
              <div key={repo.id} className="flex items-center justify-between px-5 py-3.5">
                <div>
                  <div className="text-sm text-text-primary">{repo.full_name}</div>
                  <div className="flex items-center gap-3 mt-0.5">
                    <span className="text-xs text-text-muted font-mono">{repo.default_branch}</span>
                    {repo.project_id && (
                      <span className="text-xs text-text-muted">
                        → {getProjectName(repo.project_id)}
                      </span>
                    )}
                  </div>
                </div>
                <span className="text-xs text-text-muted font-mono">#{repo.repo_id}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Link Repo Modal */}
      {linkingRepo && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div className="bg-surface border border-border rounded-lg w-full max-w-md">
            <div className="flex items-center justify-between px-5 py-3.5 border-b border-border">
              <h3 className="font-medium text-text-primary">Link Repository</h3>
              <button
                onClick={() => setLinkingRepo(null)}
                className="text-text-muted hover:text-text-primary transition-colors p-1"
              >
                ✕
              </button>
            </div>
            <div className="p-5 space-y-4">
              <div className="bg-background border border-border rounded px-3 py-2">
                <div className="text-sm text-text-primary">{linkingRepo.full_name}</div>
                <div className="text-xs text-text-muted font-mono mt-0.5">{linkingRepo.clone_url}</div>
              </div>
              <div>
                <label className="block text-xs text-text-muted mb-1">Branch</label>
                <input
                  type="text"
                  value={linkForm.default_branch}
                  onChange={e => setLinkForm(f => ({ ...f, default_branch: e.target.value }))}
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent font-mono"
                />
              </div>
              <div>
                <label className="block text-xs text-text-muted mb-1">Link to Project (optional)</label>
                <select
                  value={linkForm.project_id}
                  onChange={e => setLinkForm(f => ({ ...f, project_id: e.target.value }))}
                  className="w-full bg-background border border-border rounded px-3 py-2 text-sm text-text-primary focus:outline-none focus:border-accent"
                >
                  <option value="">— No project —</option>
                  {projects.map(p => (
                    <option key={p.id} value={p.id}>{p.name}</option>
                  ))}
                </select>
              </div>
            </div>
            <div className="flex justify-end gap-3 px-5 py-3.5 border-t border-border">
              <button
                onClick={() => setLinkingRepo(null)}
                className="text-sm text-text-muted hover:text-text-primary transition-colors px-3 py-1.5"
              >
                Cancel
              </button>
              <button
                onClick={handleLinkRepo}
                disabled={savingLink}
                className="text-sm bg-accent text-background px-4 py-1.5 rounded hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {savingLink ? 'Linking...' : 'Link Repository'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}