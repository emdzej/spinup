import { AutoRouter } from 'itty-router';

const router = AutoRouter();

function decodeBase64UrlJson(segment: string): unknown {
  const normalized = segment.replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
  const bin = atob(padded);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  const json = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  return JSON.parse(json);
}

router.post('*', async (req) => {
  const token = (await req.text()).trim();
  const parts = token.split('.');
  if (parts.length !== 3) {
    return new Response(
      JSON.stringify({ error: 'not a JWT: expected 3 dot-separated segments' }, null, 2),
      { status: 400, headers: { 'content-type': 'application/json' } },
    );
  }
  try {
    const header = decodeBase64UrlJson(parts[0]);
    const payload = decodeBase64UrlJson(parts[1]);
    // Signature is opaque bytes — surface it as the raw base64url string.
    // Not verified here (this endpoint is a decoder, not a validator).
    return new Response(
      JSON.stringify({ header, payload, signature: parts[2] }, null, 2),
      { headers: { 'content-type': 'application/json' } },
    );
  } catch (e) {
    return new Response(
      JSON.stringify({ error: (e as Error).message }, null, 2),
      { status: 400, headers: { 'content-type': 'application/json' } },
    );
  }
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
