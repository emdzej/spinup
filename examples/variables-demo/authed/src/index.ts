import { AutoRouter } from 'itty-router';
import { Variables } from '@fermyon/spin-sdk';

const router = AutoRouter();

// Compare a request header against an app-level variable. Rotate the key
// in the UI's Configuration panel without a rebuild — the shim reads
// variables at request time.
router.get('*', ({ headers }) => {
  const expected = Variables.get('api_key');
  if (!expected) {
    return new Response('server not configured: set api_key variable\n', {
      status: 503,
      headers: { 'content-type': 'text/plain; charset=utf-8' },
    });
  }
  const provided = headers.get('x-api-key');
  if (provided !== expected) {
    return new Response('unauthorized\n', {
      status: 401,
      headers: {
        'content-type': 'text/plain; charset=utf-8',
        'www-authenticate': 'ApiKey realm="spinup"',
      },
    });
  }
  return new Response('ok — authenticated\n', {
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
});

//@ts-ignore
addEventListener('fetch', (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
