'use client';

import { useEffect, useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { AdminOnly, Input, List, Row, SudoModal, failed, useSettingsToast } from '@/components/settings/common';
import { api } from '@/lib/api';
import { PermanentToken } from '@/types';

function TokensCard() {
  const { toast } = useSettingsToast();
  const [tokens, setTokens] = useState<PermanentToken[]>([]);
  const [tokenName, setTokenName] = useState('');
  const [newToken, setNewToken] = useState('');
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    api.listTokens().then(setTokens).catch(() => {});
  }, []);

  const generate = async (password: string) => {
    const res = await api.generateToken(tokenName, password);
    setNewToken(res.token);
    setTokens(prev => [res.meta, ...prev]);
    setTokenName('');
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
                      {t.token_hint} · created {new Date(t.created_at).toLocaleDateString()}
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
            className="flex gap-2 pt-1"
            onSubmit={e => { e.preventDefault(); if (tokenName) setConfirming(true); }}
          >
            <Input placeholder="Token name e.g. ci-deploy" value={tokenName} onChange={e => setTokenName(e.target.value)} />
            <Button variant="primary" type="submit" disabled={!tokenName}>Generate</Button>
          </form>
        </div>
      </Card>

      {confirming && <SudoModal title="Generate permanent token" onConfirm={generate} onCancel={() => setConfirming(false)} />}
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
