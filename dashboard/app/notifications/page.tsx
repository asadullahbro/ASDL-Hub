'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

// Notifications live on the Plugins page; old links land there.
export default function NotificationsRedirect() {
  const router = useRouter();
  useEffect(() => {
    router.replace('/plugins?tab=notifications');
  }, [router]);
  return null;
}
