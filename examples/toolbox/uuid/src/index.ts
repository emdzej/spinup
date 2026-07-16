import { AutoRouter } from 'itty-router';

const router = AutoRouter();

// GET /?n=<count> — one UUID per line. Default n=1, cap n=1000 to keep
// responses bounded.
router.get('/', ({ query }) => {
  const raw = Array.isArray(query.n) ? query.n[0] : query.n;
  const n = Math.min(Math.max(parseInt(String(raw ?? '1'), 10) || 1, 1), 1000);
  const lines: string[] = [];
  for (let i = 0; i < n; i++) lines.push(crypto.randomUUID());
  return new Response(lines.join('\n') + '\n', {
    headers: { 'content-type': 'text/plain' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
