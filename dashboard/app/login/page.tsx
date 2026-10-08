'use client';

import { useState, useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/components/providers/AuthProvider';
import { api } from '@/lib/api';

interface NodeStatus {
  hostname: string;
  online: boolean;
  ping_latency: number;
}

interface ClusterStatus {
  nodes: NodeStatus[];
  online: number;
  total: number;
}

export default function LoginPage() {
  const router = useRouter();
  const { login } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  // Set once the password was right and an authenticator code is still needed.
  const [mfaToken, setMfaToken] = useState('');
  const [code, setCode] = useState('');
  const [status, setStatus] = useState<ClusterStatus | null>(null);

  useEffect(() => {
    api.getStatus().then(setStatus).catch(() => null);
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      if (mfaToken) {
        const response = await api.loginTwoFactor(mfaToken, code);
        login(response.token, response.user);
        return;
      }
      const response = await api.login(username, password);
      if (response.mfa_required) {
        setMfaToken(response.mfa_token);
        return;
      }
      login(response.token, response.user);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Login failed';
      // The code step's token lasts five minutes; after that, start over.
      if (mfaToken && /sign in again/i.test(msg)) {
        setMfaToken('');
        setCode('');
      }
      setError(msg);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex bg-background">
      {/* Left panel */}
      <div className="hidden md:flex w-56 flex-col border-r border-border bg-surface p-6">
        <div className="flex items-center gap-2 mb-8">
          <img src="/icon.svg" alt="" className="w-6 h-6 flex-shrink-0" />
          <span className="text-sm font-semibold text-text-primary tracking-wide">ASDL Hub</span>
        </div>

        <p className="text-[10px] uppercase tracking-widest text-text-muted mb-3">
          Connected nodes
        </p>

        <div className="flex flex-col flex-1">
          {status ? (
            status.nodes.map((node) => (
              <div
                key={node.hostname}
                className="flex items-center gap-2.5 py-2 border-b border-border last:border-none"
              >
                <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
                  node.online ? 'bg-status-green' : 'bg-text-muted'
                }`} />
                <span className="text-xs text-text-primary font-mono flex-1 truncate">
                  {node.hostname}
                </span>
                <span className="text-xs text-text-secondary">
                  {node.online && node.ping_latency > 0
                    ? `${Math.round(node.ping_latency)}ms`
                    : '—'}
                </span>
              </div>
            ))
          ) : (
            // Skeleton
            Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center gap-2.5 py-2 border-b border-border last:border-none">
                <span className="w-1.5 h-1.5 rounded-full bg-border flex-shrink-0" />
                <span className="h-2.5 w-24 bg-surface-hover rounded flex-1 animate-pulse" />
                <span className="h-2 w-6 bg-surface-hover rounded animate-pulse" />
              </div>
            ))
          )}
        </div>

        <div className="mt-auto pt-4 border-t border-border space-y-1">
          {status ? (
            <>
              <p className="text-xs text-text-secondary">
                {status.online} of {status.total} nodes online
              </p>
            </>
          ) : (
            <p className="text-xs text-text-secondary animate-pulse">Connecting…</p>
          )}
        </div>
      </div>

      {/* Right panel — form */}
      <div className="flex-1 flex items-center justify-center px-6">
        <div className="w-full max-w-sm">
          {/* Mobile logo */}
          <div className="flex items-center gap-2 mb-8 md:hidden">
            <img src="/icon.svg" alt="" className="w-6 h-6" />
            <span className="text-sm font-semibold text-text-primary tracking-wide">ASDL Hub</span>
          </div>

          <h1 className="text-lg font-semibold text-text-primary mb-1">
            {mfaToken ? 'Two-factor code' : 'Sign in'}
          </h1>
          <p className="text-sm text-text-secondary mb-7">
            {mfaToken ? 'Enter the 6-digit code from your authenticator app, or a recovery code' : 'Access your control plane'}
          </p>

          <form onSubmit={handleSubmit} className="space-y-4">
            {mfaToken ? (
              <div>
                <label htmlFor="code" className="block text-xs text-text-secondary mb-1.5">
                  Code
                </label>
                <input
                  id="code"
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  className="w-full h-9 px-3 bg-surface border border-border rounded-md text-sm font-mono tracking-widest text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent transition-colors"
                  placeholder="123456"
                  autoFocus
                  required
                />
              </div>
            ) : (
            <>
            <div>
              <label
                htmlFor="username"
                className="block text-xs text-text-secondary mb-1.5"
              >
                Username
              </label>
              <input
                id="username"
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                className="w-full h-9 px-3 bg-surface border border-border rounded-md text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent transition-colors"
                placeholder="asadullah"
                required
              />
            </div>

            <div>
              <label
                htmlFor="password"
                className="block text-xs text-text-secondary mb-1.5"
              >
                Password
              </label>
              <input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full h-9 px-3 bg-surface border border-border rounded-md text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent transition-colors"
                placeholder="••••••••"
                required
              />
            </div>
            </>
            )}

            {error && (
              <div className="text-status-red text-xs text-center bg-status-red/10 border border-status-red/20 px-3 py-2 rounded-md">
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={loading}
              className="w-full h-9 bg-accent text-black text-sm font-semibold rounded-md hover:bg-accent-hover transition-colors disabled:opacity-50 mt-2"
            >
              {loading ? 'Signing in…' : mfaToken ? 'Verify' : 'Sign in'}
            </button>
            {mfaToken && (
              <button
                type="button"
                onClick={() => { setMfaToken(''); setCode(''); setError(''); }}
                className="w-full text-xs text-text-secondary hover:text-text-primary"
              >
                Back
              </button>
            )}
          </form>

          <div className="mt-8 pt-6 border-t border-border space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-xs text-text-secondary">cluster status</span>
              <div className="flex items-center gap-1.5">
                <span className={`w-1.5 h-1.5 rounded-full ${
                  status
                    ? status.online > 0 ? 'bg-status-green' : 'bg-status-red'
                    : 'bg-text-muted animate-pulse'
                }`} />
                <span className="text-xs text-text-secondary">
                  {status
                    ? status.online > 0 ? 'operational' : 'no nodes online'
                    : 'checking…'}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}