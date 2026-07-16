import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router.post('/', async (req) => {
  const raw = await req.text();
  try {
    // Also accept the `+`-as-space form used by application/x-www-form-urlencoded.
    const normalized = raw.replace(/\+/g, ' ');
    return new Response(decodeURIComponent(normalized), {
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  } catch (e) {
    return new Response(`url decode failed: ${(e as Error).message}`, {
      status: 400,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
