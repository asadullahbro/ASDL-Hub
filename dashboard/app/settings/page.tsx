'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

// /settings opens the first section.
export default function SettingsIndex() {
  const router = useRouter();
  useEffect(() => {
    router.replace('/settings/security/');
  }, [router]);
  return null;
}
