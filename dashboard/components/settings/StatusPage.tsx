'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/Badge';
import { api } from '@/lib/api';
import { failed, useSettingsToast } from '@/components/settings/common';

// Whether the status page at /status is open to everyone. Off, only signed-in
// users see it. On, visitors see just the apps that have a domain: name, state
// and uptime, never nodes or addresses.
export function StatusPageSetting() {
  const { toast } = useSettingsToast();
  const [isPublic, setPublic] = useState<boolean | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api.getStatusPageSetting().then(r => setPublic(r.public)).catch(err => toast('err', failed(err)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const change = async (next: boolean) => {
    setSaving(true);
    try {
      const res = await api.setStatusPageSetting(next);
      setPublic(res.public);
      toast('ok', res.public ? 'The status page is now public' : 'The status page is now private');
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card
      title="Status page"
      description="A page of your apps' state and uptime, for people who aren't signed in."
      actions={
        isPublic === null ? undefined : (
          <Badge tone={isPublic ? 'success' : 'neutral'}>{isPublic ? 'Public' : 'Private'}</Badge>
        )
      }
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-text-secondary max-w-xl">
          {isPublic
            ? 'Anyone can open it. Visitors see only apps that have a domain: their name, state and uptime. Never nodes or addresses.'
            : 'Only signed-in users can open it, and they see every app. Make it public to share it.'}{' '}
          <Link href="/status" className="text-accent hover:underline">Open the page</Link>
        </p>
        {isPublic !== null && (
          <Button variant={isPublic ? 'secondary' : 'primary'} loading={saving} onClick={() => change(!isPublic)}>
            {isPublic ? 'Make private' : 'Make public'}
          </Button>
        )}
      </div>
    </Card>
  );
}
