'use client';

import { useEffect, useState } from 'react';
import { formatDistanceToNow } from 'date-fns';
import { LogOut, MonitorSmartphone, Terminal } from 'lucide-react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/Badge';
import { api } from '@/lib/api';
import { SignInSession } from '@/types';
import { List, Row, failed, useSettingsToast } from './common';

const methodLabel: Record<string, string> = {
  password: 'password',
  passkey: 'passkey',
  cli: 'command line',
};

const ago = (iso: string) => formatDistanceToNow(new Date(iso), { addSuffix: true });

// Where the signed-in user is signed in, with a way to end any of it.
export function Sessions() {
  const { toast } = useSettingsToast();
  const [sessions, setSessions] = useState<SignInSession[] | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.listSessions().then(setSessions).catch(() => setSessions([]));
  }, []);

  const revoke = async (s: SignInSession) => {
    try {
      await api.revokeSession(s.id);
      setSessions(prev => (prev ?? []).filter(x => x.id !== s.id));
      toast('ok', `Signed out ${s.device}`);
    } catch (err) {
      toast('err', failed(err));
    }
  };

  const revokeOthers = async () => {
    setBusy(true);
    try {
      const { signed_out } = await api.revokeOtherSessions();
      setSessions(prev => (prev ?? []).filter(x => x.current));
      toast('ok', signed_out === 1 ? 'Signed out 1 other device' : `Signed out ${signed_out} other devices`);
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setBusy(false);
    }
  };

  const others = (sessions ?? []).filter(s => !s.current).length;

  return (
    <Card
      title="Where you're signed in"
      icon={MonitorSmartphone}
      description="Each sign-in only works in the browser it was made in. Sign out anything you don't recognise; changing your password signs out everywhere else too."
      actions={
        others > 0 && (
          <Button variant="danger" icon={LogOut} loading={busy} onClick={revokeOthers}>
            Sign out other devices
          </Button>
        )
      }
    >
      {sessions === null ? (
        <p className="text-xs text-text-secondary">Loading…</p>
      ) : sessions.length === 0 ? (
        <p className="text-xs text-text-secondary">
          No recorded sign-ins. Sign-ins from before this Hub version aren&apos;t listed; they end within a day.
        </p>
      ) : (
        <List>
          {sessions.map(s => {
            const Icon = s.method === 'cli' ? Terminal : MonitorSmartphone;
            return (
              <Row key={s.id}>
                <Icon className="h-4 w-4 text-text-secondary flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 text-xs font-medium text-text-primary">
                    {s.device || 'Unknown device'}
                    {s.current && <Badge tone="success">this device</Badge>}
                  </div>
                  <div className="text-xs text-text-secondary mt-0.5">
                    {s.current ? 'active now' : `last active ${ago(s.last_used_at)}`}
                    {' · '}
                    <span className="font-mono">{s.last_ip || s.ip}</span>
                    {s.last_ip && s.ip && s.last_ip !== s.ip && <> (signed in from <span className="font-mono">{s.ip}</span>)</>}
                  </div>
                  <div className="text-xs text-text-muted mt-0.5">
                    signed in {ago(s.created_at)} with {methodLabel[s.method] ?? 'password'}
                    {' · '}
                    {s.remember ? 'remembered, ' : ''}ends {ago(s.expires_at)}
                  </div>
                </div>
                {!s.current && (
                  <Button variant="ghost" onClick={() => revoke(s)}>Sign out</Button>
                )}
              </Row>
            );
          })}
        </List>
      )}
    </Card>
  );
}
