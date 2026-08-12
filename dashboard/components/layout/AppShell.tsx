'use client';

import { usePathname } from 'next/navigation';
import { Sidebar } from './Sidebar';

const PUBLIC_ROUTES = ['/login'];

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const isPublic = PUBLIC_ROUTES.includes(pathname.replace(/\/$/, ''));

  //console.log('AppShell pathname:', pathname, 'isPublic:', isPublic); // add this

  if (isPublic) {
    return <>{children}</>;
  }

  return (
    <div className="flex h-full flex-col md:flex-row">
      <Sidebar />
      <main className="flex-1 overflow-y-auto p-4 md:p-6">{children}</main>
    </div>
  );
}