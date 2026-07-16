import { AutoRouter } from 'itty-router';

const router = AutoRouter();

router.post('/', async (req) => {
  const text = await req.text();
  // encodeURIComponent covers everything a "form value" should escape:
  // it does NOT preserve /, ?, #, &, =, +, etc. — which is what you want
  // when you're building a value, not a URL. For URL-safe encoding of
  // path components, this is the correct primitive.
  return new Response(encodeURIComponent(text), {
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
