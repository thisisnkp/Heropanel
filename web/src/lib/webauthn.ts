// Browser-side WebAuthn glue: convert the base64url fields our JSON API uses
// to and from the ArrayBuffers navigator.credentials expects, and drive the
// two ceremonies. The server does all verification; this only marshals.
import { api } from "@/lib/api";
import type { Principal } from "@/lib/api";

export function supportsWebAuthn(): boolean {
  return typeof window !== "undefined" && !!window.PublicKeyCredential;
}

function b64urlToBuf(s: string): ArrayBuffer {
  const pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4));
  const b = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad);
  const out = new Uint8Array(b.length);
  for (let i = 0; i < b.length; i++) out[i] = b.charCodeAt(i);
  return out.buffer;
}

function bufToB64url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let s = "";
  for (const byte of bytes) s += String.fromCharCode(byte);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

type CreationOptions = {
  challenge: string;
  rp: { id: string; name: string };
  user: { id: string; name: string; displayName: string };
  pubKeyCredParams: { type: string; alg: number }[];
  timeout: number;
  authenticatorSelection: Record<string, unknown>;
  excludeCredentials?: { type: string; id: string }[];
  attestation: string;
};

type RequestOptions = {
  challenge: string;
  timeout: number;
  rpId: string;
  allowCredentials?: { type: string; id: string }[];
  userVerification: string;
};

// registerPasskey runs the create() ceremony for a signed-in user.
export async function registerPasskey(name: string): Promise<void> {
  const opts = await api.post<CreationOptions>("/account/passkeys/register/begin", {});
  const publicKey: PublicKeyCredentialCreationOptions = {
    challenge: b64urlToBuf(opts.challenge),
    rp: opts.rp,
    user: {
      id: b64urlToBuf(opts.user.id),
      name: opts.user.name,
      displayName: opts.user.displayName,
    },
    pubKeyCredParams: opts.pubKeyCredParams as PublicKeyCredentialParameters[],
    timeout: opts.timeout,
    authenticatorSelection: opts.authenticatorSelection as AuthenticatorSelectionCriteria,
    excludeCredentials: (opts.excludeCredentials ?? []).map((c) => ({
      type: "public-key",
      id: b64urlToBuf(c.id),
    })) as PublicKeyCredentialDescriptor[],
    attestation: opts.attestation as AttestationConveyancePreference,
  };
  const cred = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential;
  const resp = cred.response as AuthenticatorAttestationResponse;
  await api.post("/account/passkeys/register/finish", {
    name,
    id: bufToB64url(cred.rawId),
    client_data_json: bufToB64url(resp.clientDataJSON),
    attestation_object: bufToB64url(resp.attestationObject),
  });
}

// loginPasskey runs the get() ceremony and returns the principal on success.
export async function loginPasskey(email: string): Promise<Principal> {
  const begin = await api.post<{ options: RequestOptions; login_token: string }>(
    "/auth/webauthn/login/begin",
    { email },
  );
  const publicKey: PublicKeyCredentialRequestOptions = {
    challenge: b64urlToBuf(begin.options.challenge),
    timeout: begin.options.timeout,
    rpId: begin.options.rpId,
    allowCredentials: (begin.options.allowCredentials ?? []).map((c) => ({
      type: "public-key",
      id: b64urlToBuf(c.id),
    })) as PublicKeyCredentialDescriptor[],
    userVerification: begin.options.userVerification as UserVerificationRequirement,
  };
  const cred = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential;
  const resp = cred.response as AuthenticatorAssertionResponse;
  return api.post<Principal>("/auth/webauthn/login/finish", {
    login_token: begin.login_token,
    id: bufToB64url(cred.rawId),
    client_data_json: bufToB64url(resp.clientDataJSON),
    authenticator_data: bufToB64url(resp.authenticatorData),
    signature: bufToB64url(resp.signature),
  });
}
