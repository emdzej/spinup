import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router.post('/', async (req) => {
  const text = await req.text();
  const bytes = new TextEncoder().encode(text);
  const hex = Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
  return new Response(hex, {
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
