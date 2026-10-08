'use client';

import { useEffect, useState } from 'react';
import { ShieldCheck, ShieldOff } from 'lucide-react';
import { QRCodeSVG } from 'qrcode.react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { inputClass } from '@/components/ui/Modal';
import { api } from '@/lib/api';

type Phase =
  | { kind: 'idle' }
  | { kind: 'setup'; secret: string; uri: string }
  | { kind: 'codes'; codes: string[] }
  | { kind: 'disable' };

// Sign-in with an authenticator app, for the signed-in user.
export function TwoFactor() {
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const [phase, setPhase] = useState<Phase>({ kind: 'idle' });
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    api.getMe().then(u => setEnabled(!!u.totp_enabled)).catch(() => setEnabled(null));
  }, []);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong');
    } finally {
      setBusy(false);
    }
  };

  const reset = () => {
    setPhase({ kind: 'idle' });
    setCode('');
    setPassword('');
    setError('');
  };

  const begin = () =>
    run(async () => {
      const { secret, uri } = await api.setupTwoFactor();
      setPhase({ kind: 'setup', secret, uri });
    });

  const confirm = (e: React.FormEvent) => {
    e.preventDefault();
    run(async () => {
      const { recovery_codes } = await api.enableTwoFactor(code.trim());
      setEnabled(true);
      setCode('');
      setPhase({ kind: 'codes', codes: recovery_codes });
    });
  };

  const turnOff = (e: React.FormEvent) => {
    e.preventDefault();
    run(async () => {
      await api.disableTwoFactor(password, code.trim());
      setEnabled(false);
      reset();
    });
  };

  if (enabled === null) return null;

  return (
    <Card
      title="Two-factor sign-in"
      description="Ask for a code from an authenticator app (Google Authenticator, Aegis, 1Password…) as well as your password."
      actions={
        phase.kind === 'idle' &&
        (enabled ? (
          <Button icon={ShieldOff} onClick={() => { setError(''); setPhase({ kind: 'disable' }); }}>Turn off</Button>
        ) : (
          <Button variant="primary" icon={ShieldCheck} loading={busy} onClick={begin}>Set up</Button>
        ))
      }
    >
      <div className="text-sm space-y-3">
        {phase.kind === 'idle' && (
          <p className="text-xs text-text-secondary">
            {enabled
              ? 'On. You are asked for a code each time you sign in.'
              : 'Off. Anyone with your password can sign in.'}
          </p>
        )}

        {phase.kind === 'setup' && (
          <form onSubmit={confirm} className="space-y-3">
            <p className="text-xs text-text-secondary">
              Scan this with your authenticator app, then enter the 6-digit code it shows.
            </p>
            <div className="flex flex-wrap items-center gap-4">
              <div className="rounded-md bg-white p-2 flex-shrink-0">
                <QRCodeSVG value={phase.uri} size={132} />
              </div>
              <div className="text-xs text-text-secondary min-w-0">
                Can&apos;t scan? Enter this key by hand:
                <div className="mt-1 font-mono text-text-primary break-all select-all">
                  {phase.secret.match(/.{1,4}/g)?.join(' ')}
                </div>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <input
                value={code}
                onChange={e => setCode(e.target.value)}
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder="123456"
                className={`${inputClass} font-mono tracking-widest max-w-[10rem]`}
                autoFocus
                required
              />
              <Button type="submit" variant="primary" loading={busy}>Turn on</Button>
              <Button variant="ghost" onClick={reset}>Cancel</Button>
            </div>
          </form>
        )}

        {phase.kind === 'codes' && (
          <div className="space-y-3">
            <p className="text-xs text-status-green">Two-factor is on.</p>
            <p className="text-xs text-text-secondary">
              Save these recovery codes somewhere safe. Each works once if you lose your phone, and they are
              not shown again.
            </p>
            <div className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-md border border-border bg-surface px-3 py-2 font-mono text-xs text-text-primary select-all w-fit">
              {phase.codes.map(c => <span key={c}>{c}</span>)}
            </div>
            <div className="flex gap-2">
              <Button onClick={() => navigator.clipboard?.writeText(phase.codes.join('\n')).catch(() => {})}>Copy</Button>
              <Button variant="primary" onClick={reset}>I&apos;ve saved them</Button>
            </div>
          </div>
        )}

        {phase.kind === 'disable' && (
          <form onSubmit={turnOff} className="space-y-3">
            <p className="text-xs text-text-secondary">
              Enter your password and a current code (or a recovery code) to turn it off.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <input
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder="Password"
                className={`${inputClass} max-w-[12rem]`}
                autoFocus
                required
              />
              <input
                value={code}
                onChange={e => setCode(e.target.value)}
                autoComplete="one-time-code"
                placeholder="Code"
                className={`${inputClass} font-mono max-w-[10rem]`}
                required
              />
              <Button type="submit" variant="danger" loading={busy}>Turn off</Button>
              <Button variant="ghost" onClick={reset}>Cancel</Button>
            </div>
          </form>
        )}

        {error && <p className="text-xs text-status-red">{error}</p>}
      </div>
    </Card>
  );
}
