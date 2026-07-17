<script lang="ts">
  import { api } from './api';
  import type { InvokeRequest, InvokeResponse } from './types';

  interface Props {
    appId: string;
    fnId: string;
    /** Function's mount route (e.g. "/uuid/..."). Used as the initial path
     *  so the first click on "Send" hits the function, not a bare "/" that
     *  isn't mounted anywhere. */
    route?: string;
  }

  let { appId, fnId, route }: Props = $props();

  const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];
  let method = $state('GET');
  // Strip Spin's "/..." suffix — that's a route matcher, not a real path.
  let path = $state(route ? route.replace(/\/\.\.\.$/, '') || '/' : '/');
  let query = $state<[string, string][]>([['', '']]);
  let headers = $state<[string, string][]>([['', '']]);
  let body = $state('');

  let response = $state<InvokeResponse | null>(null);
  let error = $state<string | null>(null);
  let sending = $state(false);

  function toKV(rows: [string, string][]): Record<string, string[]> {
    const out: Record<string, string[]> = {};
    for (const [k, v] of rows) {
      if (!k) continue;
      (out[k] ??= []).push(v);
    }
    return out;
  }

  async function send(e: SubmitEvent) {
    e.preventDefault();
    if (sending) return;
    sending = true;
    error = null;
    const bodyAllowed = !['GET', 'HEAD'].includes(method);
    const input: InvokeRequest = {
      method,
      path: path || '/',
      query: toKV(query),
      headers: toKV(headers),
      body: bodyAllowed ? body : undefined
    };
    try {
      response = await api.invokeFunction(appId, fnId, input);
    } catch (err) {
      error = (err as Error).message;
      response = null;
    } finally {
      sending = false;
    }
  }

  function addRow(rows: [string, string][]): [string, string][] {
    return [...rows, ['', '']];
  }
  function removeRow(rows: [string, string][], i: number): [string, string][] {
    const next = rows.filter((_, idx) => idx !== i);
    return next.length ? next : [['', '']];
  }

  function statusClass(s: number): string {
    if (s >= 200 && s < 300) return 'ok';
    if (s >= 400) return 'fail';
    return 'wait';
  }

  function contentType(headers: Record<string, string[]> | undefined): string {
    if (!headers) return '';
    const key = Object.keys(headers).find((k) => k.toLowerCase() === 'content-type');
    return key ? headers[key][0] : '';
  }

  function isJson(ct: string): boolean {
    return ct.toLowerCase().includes('json');
  }

  const prettyBody = $derived.by(() => {
    if (!response || response.bodyIsBase64) return response?.body ?? '';
    if (isJson(contentType(response.headers))) {
      try {
        return JSON.stringify(JSON.parse(response.body), null, 2);
      } catch {
        return response.body;
      }
    }
    return response.body;
  });

  const inputCls =
    'font-mono text-sm px-2 py-1.5 border border-border-strong rounded focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent';
  const ghostBtn =
    'inline-flex items-center gap-1 bg-transparent border border-dashed border-border-strong text-fg-muted px-2 py-1 text-sm rounded-md hover:bg-bg-elev cursor-pointer';
</script>

<form onsubmit={send} class="flex flex-col gap-2.5">
  <div class="flex gap-2 items-stretch">
    <select
      bind:value={method}
      class="font-mono px-2.5 border border-border-strong rounded-md bg-white focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
    >
      {#each methods as m}<option>{m}</option>{/each}
    </select>
    <input
      class="flex-1 font-mono text-sm px-2.5 py-2 border border-border-strong rounded-md focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
      bind:value={path}
      placeholder="/"
      spellcheck="false"
    />
    <button class="button primary" type="submit" disabled={sending}>
      {sending ? 'Sending…' : 'Send'}
    </button>
  </div>

  <details>
    <summary class="cursor-pointer text-sm text-fg-muted select-none py-1 [details[open]_&]:text-fg">Query params</summary>
    <div class="grid grid-cols-[1fr_1fr_auto] gap-1.5 mt-1">
      {#each query as _, i (i)}
        <input bind:value={query[i][0]} placeholder="name" class={inputCls} />
        <input bind:value={query[i][1]} placeholder="value" class={inputCls} />
        <button type="button" class={ghostBtn} onclick={() => (query = removeRow(query, i))}>×</button>
      {/each}
      <button type="button" class="{ghostBtn} col-span-full justify-self-start" onclick={() => (query = addRow(query))}>+ add</button>
    </div>
  </details>

  <details>
    <summary class="cursor-pointer text-sm text-fg-muted select-none py-1 [details[open]_&]:text-fg">Headers</summary>
    <div class="grid grid-cols-[1fr_1fr_auto] gap-1.5 mt-1">
      {#each headers as _, i (i)}
        <input bind:value={headers[i][0]} placeholder="X-Header" class={inputCls} />
        <input bind:value={headers[i][1]} placeholder="value" class={inputCls} />
        <button type="button" class={ghostBtn} onclick={() => (headers = removeRow(headers, i))}>×</button>
      {/each}
      <button type="button" class="{ghostBtn} col-span-full justify-self-start" onclick={() => (headers = addRow(headers))}>+ add</button>
    </div>
  </details>

  {#if !['GET', 'HEAD'].includes(method)}
    <details open>
      <summary class="cursor-pointer text-sm text-fg-muted select-none py-1 [details[open]_&]:text-fg">Body</summary>
      <textarea
        class="w-full p-2 border border-border-strong rounded-md font-mono text-sm bg-bg-elev resize-y focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
        bind:value={body}
        rows="6"
        spellcheck="false"
        placeholder={'{"hello": "world"}'}
      ></textarea>
    </details>
  {/if}
</form>

{#if error}
  <p class="error">{error}</p>
{/if}

{#if response}
  <div class="mt-4 pt-4 border-t border-border">
    <div class="flex items-center gap-2 mb-3">
      <span class="pill {statusClass(response.status)}">HTTP {response.status}</span>
      <span class="text-sm text-fg-muted">{response.durationMs}ms</span>
      {#if response.truncated}<span class="pill fail">truncated at 1 MiB</span>{/if}
    </div>
    <details>
      <summary class="cursor-pointer text-sm text-fg-muted select-none py-1 [details[open]_&]:text-fg">Response headers</summary>
      <table class="w-full mt-1.5 border-collapse text-xs">
        <tbody>
          {#each Object.entries(response.headers) as [k, vs]}
            {#each vs as v}
              <tr>
                <th class="text-left py-0.5 pr-2 text-fg-muted font-medium font-mono">{k}</th>
                <td class="py-0.5 font-mono break-all">{v}</td>
              </tr>
            {/each}
          {/each}
        </tbody>
      </table>
    </details>
    <div class="text-xs text-fg-muted mt-3 mb-1 uppercase tracking-wider">
      Body {#if response.bodyIsBase64}<span class="text-xs text-fg-muted">(binary, base64)</span>{/if}
    </div>
    <pre class="bg-[#0b1120] text-[#d1d5db] p-3 m-0 max-h-80 overflow-auto font-mono text-xs rounded-md whitespace-pre-wrap break-all">{prettyBody || '(empty)'}</pre>
  </div>
{/if}
