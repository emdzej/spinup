import { AutoRouter } from 'itty-router';

const router = AutoRouter();

// btoa() operates on Latin-1; wrap it so multi-byte UTF-8 characters survive.
function encodeUtf8Base64(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
}

router.post('*', async (req) => {
  const text = await req.text();
  return new Response(encodeUtf8Base64(text), {
    headers: { 'content-type': 'text/plain' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
