'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { KeyRound, Server, Settings as SettingsIcon, ShieldCheck, SlidersHorizontal, Users } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { PageHeader } from '@/components/ui/PageHeader';
import { Badge } from '@/components/ui/Badge';
import { useAuth } from '@/components/providers/AuthProvider';
import { SettingsToastProvider, useSettingsToast } from '@/components/settings/common';

type Item = { href: string; name: string; icon: LucideIcon; blurb: string };

// Settings, one page per kind of thing. Everyone has Security; the rest
// change the Hub for everyone, so only admins see them.
const account: Item[] = [
  { href: '/settings/security', name: 'Security', icon: ShieldCheck, blurb: 'Passkeys, two-factor sign-in and where you are signed in.' },
];
const hub: Item[] = [
  { href: '/settings/users', name: 'Users', icon: Users, blurb: 'Who can sign in, and what they can do.' },
  { href: '/settings/tokens', name: 'API tokens', icon: KeyRound, blurb: 'Long-lived tokens for scripts, CI and integrations.' },
  { href: '/settings/nodes', name: 'Nodes', icon: Server, blurb: 'Adding nodes, the master node and agent updates.' },
  { href: '/settings/system', name: 'System', icon: SlidersHorizontal, blurb: 'Hub updates and the Nginx routes.' },
];

function NavGroup({ label, items, pathname }: { label: string; items: Item[]; pathname: string }) {
  return (
    <div className="contents md:block md:space-y-0.5">
      <p className="hidden md:block px-3 pb-1.5 pt-1 text-[11px] uppercase tracking-wider text-text-muted">{label}</p>
      {items.map(item => {
        const active = pathname.startsWith(item.href);
        const Icon = item.icon;
        return (
          <Link
            key={item.href}
            href={item.href}
            aria-current={active ? 'page' : undefined}
            className={`flex items-center gap-2.5 px-3 py-1.5 rounded-md text-xs whitespace-nowrap border transition-colors ${
              active
                ? 'bg-surface-hover text-text-primary border-border-strong'
                : 'text-text-secondary hover:bg-surface-hover hover:text-text-primary border-transparent'
            }`}
          >
            <Icon className="h-3.5 w-3.5 flex-shrink-0" />
            {item.name}
          </Link>
        );
      })}
    </div>
  );
}

function SettingsFrame({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { user } = useAuth();
  const { feedback } = useSettingsToast();
  const isAdmin = user?.role === 'admin';
  const current = [...account, ...hub].find(i => pathname.startsWith(i.href));

  return (
    <div className="space-y-6">
      <PageHeader
        icon={SettingsIcon}
        title={current ? <>Settings <span className="text-text-muted font-normal">/ {current.name}</span></> : 'Settings'}
        description={current?.blurb}
        actions={
          feedback && (
            <Badge tone={feedback.type === 'ok' ? 'success' : 'danger'} className="text-xs py-1">
              {feedback.msg}
            </Badge>
          )
        }
      />

      <div className="flex flex-col gap-6 md:flex-row md:items-start">
        {/* Section nav: a column on wide screens, a scrolling row on phones. */}
        <nav
          aria-label="Settings"
          className="flex gap-1 overflow-x-auto -mx-4 px-4 pb-1 md:mx-0 md:px-0 md:pb-0 md:w-44 md:flex-shrink-0 md:flex-col md:gap-4 md:sticky md:top-0"
        >
          <NavGroup label="Account" items={account} pathname={pathname} />
          {isAdmin && <NavGroup label="Hub" items={hub} pathname={pathname} />}
        </nav>
        <div className="flex-1 min-w-0 max-w-4xl space-y-6">{children}</div>
      </div>
    </div>
  );
}

export default function SettingsLayout({ children }: { children: React.ReactNode }) {
  return (
    <SettingsToastProvider>
      <SettingsFrame>{children}</SettingsFrame>
    </SettingsToastProvider>
  );
}
