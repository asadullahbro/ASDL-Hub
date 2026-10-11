'use client';

import { useEffect, useState } from 'react';
import { formatDistanceToNow } from 'date-fns';
import { KeyRound, Plus } from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { api } from '@/lib/api';
import { createPasskey, passkeyError, passkeysSupported } from '@/lib/webauthn';
import { Passkey } from '@/types';
import { Input, List, Row, failed, useSettingsToast } from './common';

// A name for a new passkey: the kind of device it is probably on.
function guessName(): string {
  if (typeof navigator === 'undefined') return 'Passkey';
  const ua = navigator.userAgent;
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua)) return 'iPad';
  if (/Android/.test(ua)) return 'Android phone';
  if (/Macintosh/.test(ua)) return 'Mac';
  if (/Windows/.test(ua)) return 'Windows PC';
  if (/CrOS/.test(ua)) return 'Chromebook';
  if (/Linux/.test(ua)) return 'Linux PC';
  return 'Passkey';
}

// The signed-in user's passkeys: add, list, remove.
export function Passkeys() {
  const { toast } = useSettingsToast();
  const [keys, setKeys] = useState<Passkey[] | null>(null);
  const [totp, setTotp] = useState(false);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [supported, setSupported] = useState(true);

  useEffect(() => {
    setSupported(passkeysSupported());
    api.listPasskeys().then(setKeys).catch(() => setKeys([]));
    api.getMe().then(u => setTotp(!!u.totp_enabled)).catch(() => {});
  }, []);

  const startAdding = () => {
    setName(guessName());
    setPassword('');
    setCode('');
    setError('');
    setAdding(true);
  };

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      const { ceremony, options } = await api.beginPasskeyRegistration(password, code.trim());
      const credential = await createPasskey(options);
      const pk = await api.finishPasskeyRegistration(ceremony, name, credential);
      setKeys(prev => [...(prev ?? []), pk]);
      setAdding(false);
      toast('ok', `Passkey "${pk.name}" added`);
    } catch (err) {
      setError(err instanceof DOMException ? passkeyError(err) : failed(err));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (pk: Passkey) => {
    if (!window.confirm(`Remove the passkey "${pk.name}"? You can't sign in with it any more.`)) return;
    try {
      await api.deletePasskey(pk.id);
      setKeys(prev => (prev ?? []).filter(k => k.id !== pk.id));
      toast('ok', 'Passkey removed');
    } catch (err) {
      toast('err', failed(err));
    }
  };

  return (
    <Card
      title="Passkeys"
      icon={KeyRound}
      description="Sign in with your fingerprint, face or device PIN instead of a password and code. Phishing sites can't use them."
      actions={
        !adding && supported && <Button variant="primary" icon={Plus} onClick={startAdding}>Add passkey</Button>
      }
    >
      <div className="space-y-3 text-sm">
        {!supported && (
          <p className="text-xs text-status-yellow">
            Passkeys only work when the Hub is opened over https at its domain name.
          </p>
        )}

        {keys && keys.length > 0 ? (
          <List>
            {keys.map(k => (
              <Row key={k.id}>
                <div className="flex-1 min-w-0">
                  <div className="text-xs font-medium text-text-primary">{k.name}</div>
                  <div className="text-xs text-text-secondary mt-0.5">
                    added {new Date(k.created_at).toLocaleDateString()}
                    {' · '}
                    {k.last_used_at ? `last used ${formatDistanceToNow(new Date(k.last_used_at), { addSuffix: true })}` : 'never used'}
                    {' · '}
                    <span className="font-mono">{k.rp_id}</span>
                  </div>
                </div>
                <Button variant="danger" onClick={() => remove(k)}>Remove</Button>
              </Row>
            ))}
          </List>
        ) : (
          keys && !adding && <p className="text-xs text-text-secondary">No passkeys yet.</p>
        )}

        {adding && (
          <form onSubmit={add} className="space-y-3 rounded-md border border-border p-3">
            <p className="text-xs text-text-secondary">
              Confirm it&apos;s you{totp ? ' with your password and an authenticator code' : ' with your password'}, then
              follow your browser&apos;s prompt.
            </p>
            <div className="grid gap-2 sm:grid-cols-3">
              <Input value={name} onChange={e => setName(e.target.value)} placeholder="Name, e.g. MacBook" maxLength={100} required />
              <Input type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder="Password" autoFocus required />
              {totp && (
                <Input
                  value={code}
                  onChange={e => setCode(e.target.value)}
                  autoComplete="one-time-code"
                  inputMode="numeric"
                  placeholder="Code"
                  className="font-mono"
                  required
                />
              )}
            </div>
            <div className="flex gap-2">
              <Button type="submit" variant="primary" loading={busy}>Create passkey</Button>
              <Button variant="ghost" onClick={() => setAdding(false)}>Cancel</Button>
            </div>
          </form>
        )}

        {error && <p className="text-xs text-status-red">{error}</p>}
      </div>
    </Card>
  );
}
