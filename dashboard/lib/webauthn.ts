// Passkeys in the browser. The Hub sends WebAuthn options as JSON with
// base64url strings where the browser wants bytes, and wants the answer back
// the same way; these convert in both directions.

/* eslint-disable @typescript-eslint/no-explicit-any */

function toBytes(s: string): ArrayBuffer {
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/');
  const bin = atob(b64 + '='.repeat((4 - (b64.length % 4)) % 4));
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

function toBase64url(buf: ArrayBuffer | null): string | null {
  if (!buf) return null;
  let bin = '';
  for (const b of new Uint8Array(buf)) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

const descriptors = (list?: any[]) => (list ?? []).map(c => ({ ...c, id: toBytes(c.id) }));

// Whether this browser, on this page, can use passkeys at all. They need a
// secure page (https, or localhost).
export function passkeysSupported(): boolean {
  return typeof window !== 'undefined' && window.isSecureContext && !!window.PublicKeyCredential;
}

// What went wrong, in words, for errors the browser throws.
export function passkeyError(err: unknown): string {
  if (err instanceof DOMException) {
    if (err.name === 'NotAllowedError') return 'Cancelled, or it timed out.';
    if (err.name === 'InvalidStateError') return 'This device already has a passkey for your account.';
    if (err.name === 'SecurityError') return 'Passkeys need the Hub opened over https at its domain name.';
  }
  return err instanceof Error ? err.message : 'The passkey did not work.';
}

// createPasskey makes a passkey from the Hub's registration options.
export async function createPasskey(options: any): Promise<object> {
  const pk = options.publicKey;
  const cred = (await navigator.credentials.create({
    publicKey: {
      ...pk,
      challenge: toBytes(pk.challenge),
      user: { ...pk.user, id: toBytes(pk.user.id) },
      excludeCredentials: descriptors(pk.excludeCredentials),
    },
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error('No passkey was made.');
  const r = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: toBase64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: toBase64url(r.clientDataJSON),
      attestationObject: toBase64url(r.attestationObject),
      transports: r.getTransports?.() ?? [],
    },
  };
}

// getPasskey signs the Hub's challenge with a passkey the user picks.
export async function getPasskey(options: any): Promise<object> {
  const pk = options.publicKey;
  const cred = (await navigator.credentials.get({
    publicKey: {
      ...pk,
      challenge: toBytes(pk.challenge),
      allowCredentials: descriptors(pk.allowCredentials),
    },
  })) as PublicKeyCredential | null;
  if (!cred) throw new Error('No passkey was chosen.');
  const r = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: toBase64url(cred.rawId),
    type: cred.type,
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
    clientExtensionResults: cred.getClientExtensionResults(),
    response: {
      clientDataJSON: toBase64url(r.clientDataJSON),
      authenticatorData: toBase64url(r.authenticatorData),
      signature: toBase64url(r.signature),
      userHandle: toBase64url(r.userHandle),
    },
  };
}
