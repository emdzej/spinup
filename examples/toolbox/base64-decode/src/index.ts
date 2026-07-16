import { AutoRouter } from 'itty-router';

const router = AutoRouter();

// atob() is Latin-1; convert bytes back to UTF-8 explicitly. Accepts both
// standard and URL-safe alphabets.
function decodeBase64Utf8(input: string): string {
  const normalized = input.replace(/-/g, '+').replace(/_/g, '/');
  const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
  const bin = atob(padded);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
}

router.post('/', async (req) => {
  const input = (await req.text()).trim();
  try {
    return new Response(decodeBase64Utf8(input), {
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  } catch (e) {
    return new Response(`base64 decode failed: ${(e as Error).message}`, {
      status: 400,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
