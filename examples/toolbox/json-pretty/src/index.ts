import { AutoRouter } from 'itty-router';

const router = AutoRouter();

// POST body: any JSON value. Query params:
//   ?indent=<n>   spaces (default 2, max 8)
//   ?minify=1     override — output on one line, no whitespace
router.post('*', async (req) => {
  const raw = await req.text();
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (e) {
    return new Response(`invalid JSON: ${(e as Error).message}`, {
      status: 400,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
  const url = new URL(req.url);
  const minify = url.searchParams.get('minify') === '1';
  const indent = Math.min(Math.max(parseInt(url.searchParams.get('indent') ?? '2', 10) || 2, 0), 8);
  const out = minify ? JSON.stringify(value) : JSON.stringify(value, null, indent);
  return new Response(out, {
    headers: { 'content-type': 'application/json; charset=utf-8' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
