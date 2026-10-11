'use client';

import { Passkeys } from '@/components/settings/Passkeys';
import { Sessions } from '@/components/settings/Sessions';
import { TwoFactor } from '@/components/settings/TwoFactor';

export default function SecuritySettings() {
  return (
    <>
      <Passkeys />
      <TwoFactor />
      <Sessions />
    </>
  );
}
