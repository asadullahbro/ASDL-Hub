'use client';

import { useEffect, useState } from 'react';
import { CheckCircle2, Terminal, XCircle } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { useAuth } from '@/components/providers/AuthProvider';
import { api } from '@/lib/api';

type Request = { machine: string; ip: string; created_at: string; expires_at: string; status: string };
type Outcome = 'approved' | 'denied';

// Where `asdl-hub login` sends the user to approve a command-line login.
export default function AuthorizePage() {
  const { user } = useAuth();
  const [code, setCode] = useState('');
  const [req, setReq] = useState<Request | null>(null);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const c = new URLSearchParams(window.location.search).get('code') || '';
    setCode(c);
    if (!c) {
      setError('This page needs a code. Run `asdl-hub login` and open the address it prints.');
      return;
    }
    api.getCLIRequest(c).then(setReq).catch(err => setError(err instanceof Error ? err.message : 'Could not load the request'));
  }, []);

  const answer = async (approve: boolean) => {
    setBusy(true);
    setError('');
    try {
      await (approve ? api.approveCLI(code) : api.denyCLI(code));
      setOutcome(approve ? 'approved' : 'denied');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong');
    } finally {
      setBusy(false);
    }
  };

  const permanent = user?.role === 'admin';

  return (
    <div className="space-y-6 max-w-2xl">
      <PageHeader icon={Terminal} title="Authorise the command line" description="Sign asdl-hub in on another machine." />

      <Card title="Login request" description="Someone ran asdl-hub login and is waiting for this.">
        <div className="space-y-4 text-sm">
          {outcome === 'approved' && (
            <p className="flex items-center gap-2 text-status-green">
              <CheckCircle2 className="h-4 w-4" /> Approved. Go back to your terminal; you can close this page.
            </p>
          )}
          {outcome === 'denied' && (
            <p className="flex items-center gap-2 text-text-secondary">
              <XCircle className="h-4 w-4" /> Turned down. Nothing was signed in.
            </p>
          )}

          {!outcome && req && (
            <>
              <div className="flex items-start gap-3 rounded-md border border-border bg-surface px-3 py-3">
                <Terminal className="h-4 w-4 mt-0.5 text-text-secondary flex-shrink-0" />
                <div className="space-y-1 min-w-0">
                  <div>
                    <span className="text-text-secondary">Machine: </span>
                    <span className="font-mono text-text-primary break-all">{req.machine}</span>
                  </div>
                  <div>
                    <span className="text-text-secondary">Asked from: </span>
                    <span className="font-mono text-text-primary">{req.ip}</span>
                  </div>
                  <div>
                    <span className="text-text-secondary">Code: </span>
                    <span className="font-mono text-text-primary tracking-widest">{code.toUpperCase()}</span>
                  </div>
                </div>
              </div>
              <p className="text-xs text-text-secondary">
                Check that the code matches the one in your terminal, and that you started this login.
                Approving signs that machine in as <b className="text-text-primary">{user?.username}</b>
                {permanent
                  ? ' with a token that lasts until you revoke it in Settings → Tokens.'
                  : ' for 24 hours.'}{' '}
                If you didn&apos;t start it, turn it down.
              </p>
              <div className="flex gap-2">
                <Button variant="primary" loading={busy} onClick={() => answer(true)}>Approve</Button>
                <Button variant="ghost" disabled={busy} onClick={() => answer(false)}>Turn down</Button>
              </div>
            </>
          )}

          {!outcome && !req && !error && <p className="text-xs text-text-secondary">Loading…</p>}
          {error && <p className="text-xs text-status-red">{error}</p>}
        </div>
      </Card>
    </div>
  );
}
