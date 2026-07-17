import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router.post('*', async (req) => {
  const bytes = new Uint8Array(await req.arrayBuffer());
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  const hex = Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
  return new Response(hex, {
    headers: { 'content-type': 'text/plain' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
