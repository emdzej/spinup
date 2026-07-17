import { AutoRouter } from 'itty-router';
import { ResponseBuilder } from '@fermyon/spin-sdk';

const router = AutoRouter();

router.post('*', async (req) => {
  const raw = (await req.text()).trim();
  if (!/^[0-9a-fA-F\s]*$/.test(raw)) {
    return new Response('input must be hex (0-9, a-f, A-F, whitespace allowed)', {
      status: 400,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
  const clean = raw.replace(/\s+/g, '');
  if (clean.length % 2 !== 0) {
    return new Response('hex must have an even number of characters', {
      status: 400,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
  const bytes = new Uint8Array(clean.length / 2);
  for (let i = 0; i < clean.length; i += 2) {
    bytes[i / 2] = parseInt(clean.substring(i, i + 2), 16);
  }
  return new Response(new TextDecoder().decode(bytes), {
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
});

//@ts-ignore
addEventListener('fetch', async (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
