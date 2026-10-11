'use client';

import { useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { HubVersion } from '@/components/settings/HubVersion';
import { AdminOnly, failed, useSettingsToast } from '@/components/settings/common';
import { api } from '@/lib/api';

export default function SystemSettings() {
  const { toast } = useSettingsToast();
  const [loading, setLoading] = useState(false);

  const handleNginx = async () => {
    setLoading(true);
    try {
      await api.updateNginx();
      toast('ok', 'Nginx config updated');
    } catch (err) {
      toast('err', failed(err));
    } finally {
      setLoading(false);
    }
  };

  return (
    <AdminOnly>
      <HubVersion />
      <Card
        title="Nginx"
        description="Regenerate and reload the Nginx reverse proxy config from current running projects."
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-xs text-text-secondary">Triggers a config update across all nodes with active projects.</p>
          <Button variant="primary" loading={loading} onClick={handleNginx}>Update config</Button>
        </div>
      </Card>
    </AdminOnly>
  );
}
