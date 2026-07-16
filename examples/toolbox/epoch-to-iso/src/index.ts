import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router.post('/', async (req) => {
  const raw = (await req.text()).trim();
  const n = Number(raw);
  if (!Number.isFinite(n)) {
    return new Response('input must be a numeric unix timestamp (seconds or milliseconds)', {
      status: 400,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
  // Anything below ~10^11 is seconds — 10^11 seconds is year 5138. Everything
  // above that we treat as milliseconds. Covers both conventions without a flag.
  const ms = Math.abs(n) < 1e11 ? n * 1000 : n;
  const d = new Date(ms);
  if (isNaN(d.getTime())) {
    return new Response('timestamp out of range', {
      status: 400,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
  return new Response(d.toISOString(), {
    headers: { 'content-type': 'text/plain' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
