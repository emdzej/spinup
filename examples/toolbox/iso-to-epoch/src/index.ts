import { AutoRouter } from 'itty-router';

const router = AutoRouter();

// Returns unix seconds by default. Add ?unit=ms for milliseconds.
router.post('*', async (req) => {
  const raw = (await req.text()).trim();
  const t = Date.parse(raw);
  if (!Number.isFinite(t)) {
    return new Response('input must be an ISO-8601 datetime (e.g. 2026-07-16T15:36:50Z)', {
      status: 400,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
  const url = new URL(req.url);
  const ms = url.searchParams.get('unit') === 'ms';
  const value = ms ? t : Math.floor(t / 1000);
  return new Response(String(value), {
    headers: { 'content-type': 'text/plain' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
