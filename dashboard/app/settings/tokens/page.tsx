'use client';

import { useEffect, useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { AdminOnly, Input, List, Row, SudoModal, failed, useSettingsToast } from '@/components/settings/common';
import { api } from '@/lib/api';
import { PermanentToken, TokenScope } from '@/types';

const SCOPES: { value: TokenScope; label: string; hint: string }[] = [
  { value: 'read', label: 'Read only', hint: 'See apps, nodes and status. Cannot change anything.' },
  { value: 'operate', label: 'Operate', hint: 'Read, plus deploy, restart, move and roll back apps.' },
  { value: 'admin', label: 'Full access', hint: 'Everything you can do, including settings and tokens.' },
];

// Tokens made before scopes existed have none and work as full access.
const scopeLabel = (s?: TokenScope) => SCOPES.find(x => x.value === s)?.label ?? 'Full access';

function TokensCard() {
  const { toast } = useSettingsToast();
  const [tokens, setTokens] = useState<PermanentToken[]>([]);
  const [tokenName, setTokenName] = useState('');
  const [scope, setScope] = useState<TokenScope>('read');   // least access unless chosen otherwise
  const [newToken, setNewToken] = useState('');
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    api.listTokens().then(setTokens).catch(() => {});
  }, []);

  const generate = async (password: string) => {
    const res = await api.generateToken(tokenName, scope, password);
    setNewToken(res.token);
    setTokens(prev => [res.meta, ...prev]);
    setTokenName('');
    setScope('read');
    setConfirming(false);
  };

  const revoke = async (t: PermanentToken) => {
    try {
      await api.revokeToken(t.id);
      setTokens(prev => prev.filter(x => x.id !== t.id));
      toast('ok', 'Token revoked');
    } catch (err) {
      toast('err', failed(err));
    }
  };

  return (
    <>
      <Card
        title="Permanent tokens"
        description="Long-lived API tokens for agents and integrations. Each is shown once, when it is made."
      >
        <div className="space-y-3">
          {tokens.length === 0 ? (
            <p className="text-sm text-text-secondary">No tokens yet.</p>
          ) : (
            <List>
              {tokens.map(t => (
                <Row key={t.id}>
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium text-text-primary">{t.name}</div>
                    <div className="text-xs text-text-secondary mt-0.5">
                      {scopeLabel(t.scope)} · {t.token_hint} · created {new Date(t.created_at).toLocaleDateString()}
                    </div>
                  </div>
                  <Button variant="danger" onClick={() => revoke(t)}>Revoke</Button>
                </Row>
              ))}
            </List>
          )}

          {newToken && (
            <div className="bg-status-green/5 border border-status-green/20 rounded-md p-3">
              <p className="text-xs text-status-green mb-2">Token created. Copy it now: it won&apos;t be shown again.</p>
              <code className="text-xs text-text-primary font-mono break-all block mb-3">{newToken}</code>
              <Button onClick={() => { navigator.clipboard.writeText(newToken); toast('ok', 'Copied'); }}>Copy</Button>
            </div>
          )}

          <form
            className="space-y-3 pt-1"
            onSubmit={e => { e.preventDefault(); if (tokenName) setConfirming(true); }}
          >
            <Input placeholder="Token name e.g. ci-deploy" value={tokenName} onChange={e => setTokenName(e.target.value)} />
            <fieldset className="space-y-2">
              <legend className="text-xs font-medium text-text-primary mb-1">What can it do?</legend>
              {SCOPES.map(o => (
                <label
                  key={o.value}
                  className={`flex items-start gap-2 rounded-md border p-2.5 cursor-pointer ${scope === o.value ? 'border-accent bg-accent/5' : 'border-border'}`}
                >
                  <input type="radio" name="scope" className="mt-0.5" checked={scope === o.value} onChange={() => setScope(o.value)} />
                  <span>
                    <span className="block text-xs font-medium text-text-primary">{o.label}</span>
                    <span className="block text-xs text-text-secondary">{o.hint}</span>
                  </span>
                </label>
              ))}
            </fieldset>
            <Button variant="primary" type="submit" disabled={!tokenName}>Generate</Button>
          </form>
        </div>
      </Card>

      {confirming && <SudoModal title={`Generate ${scopeLabel(scope).toLowerCase()} token`} onConfirm={generate} onCancel={() => setConfirming(false)} />}
    </>
  );
}

export default function TokensSettings() {
  return (
    <AdminOnly>
      <TokensCard />
    </AdminOnly>
  );
}
