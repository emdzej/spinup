import { AutoRouter } from 'itty-router';
import { Variables } from '@fermyon/spin-sdk';

const router = AutoRouter();

// Reads the app-level `greeting` variable at request time. Set it in the
// UI's Configuration → Variables panel; kubectl-side it becomes
// spec.variables[name=greeting] on the SpinApp CR.
router.get('*', () => {
  const message = Variables.get('greeting') ?? 'Hello from Spinup';
  return new Response(`${message}\n`, {
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
});

//@ts-ignore
addEventListener('fetch', (event: FetchEvent) => {
  event.respondWith(router.fetch(event.request));
});
