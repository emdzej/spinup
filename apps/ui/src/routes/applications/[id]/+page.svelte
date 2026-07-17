<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { onMount, onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import { initialFiles } from '$lib/templates';
  import MetricChart from '$lib/MetricChart.svelte';
  import { formatBytes } from '$lib/format';
  import type { ApplicationDetail, Build, MetricsResponse } from '$lib/types';

  const id = $derived($page.params.id as string);

  let app = $state<ApplicationDetail | null>(null);
  let loadErr = $state<string | null>(null);
  let deleting = $state(false);

  let builds = $state<Build[]>([]);
  let buildErr = $state<string | null>(null);
  let building = $state(false);
  let selectedBuildId = $state<string | null>(null);
  let logText = $state('');
  let logLoading = $state(false);
  let logAbort: AbortController | undefined = undefined;

  let addOpen = $state(false);
  let newFnName = $state('');
  let newFnRoute = $state('');
  let addErr = $state<string | null>(null);
  let addBusy = $state(false);

  let metricsRange = $state('15m');
  let metrics = $state<MetricsResponse | null>(null);

  async function loadApp() {
    try {
      app = await api.getApplication(id);
      loadErr = null;
    } catch (e) {
      loadErr = (e as Error).message;
    }
  }
  async function loadBuilds() {
    try {
      builds = await api.listBuilds(id);
    } catch {
      /* ignore poll */
    }
  }
  async function loadMetrics() {
    try {
      metrics = await api.applicationMetrics(id, metricsRange, stepFor(metricsRange));
    } catch {
      metrics = null;
    }
  }
  function stepFor(r: string): string {
    return { '5m': '5s', '15m': '15s', '1h': '30s', '6h': '2m' }[r] ?? '15s';
  }

  let poll: ReturnType<typeof setInterval> | undefined;
  onMount(() => {
    void loadApp();
    void loadBuilds();
    void loadMetrics();
    poll = setInterval(() => {
      void loadApp();
      void loadBuilds();
      void loadMetrics();
    }, 5000);
  });
  onDestroy(() => {
    if (poll) clearInterval(poll);
    logAbort?.abort();
  });

  $effect(() => {
    void metricsRange;
    void loadMetrics();
  });

  async function startBuild() {
    if (building) return;
    building = true;
    buildErr = null;
    try {
      const b = await api.startBuild(id);
      selectedBuildId = b.id;
      await loadBuilds();
    } catch (e) {
      buildErr = (e as Error).message;
    } finally {
      building = false;
    }
  }

  async function viewLogs(buildId: string) {
    logAbort?.abort();
    logAbort = new AbortController();
    const build = builds.find((b) => b.id === buildId);
    const isRunning = build && (build.status === 'pending' || build.status === 'running');
    selectedBuildId = buildId;
    logLoading = true;
    logText = '';
    try {
      const url = `${api.buildLogsUrl(id, buildId)}${isRunning ? '?follow=true' : ''}`;
      const res = await fetch(url, { signal: logAbort.signal });
      if (!res.ok || !res.body) {
        logText = await res.text();
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        logText += decoder.decode(value, { stream: true });
      }
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        logText += `\n[${(e as Error).message}]`;
      }
    } finally {
      logLoading = false;
    }
  }

  $effect(() => {
    if (!builds.length) return;
    const latest = builds[0];
    if (
      (latest.status === 'pending' || latest.status === 'running') &&
      selectedBuildId !== latest.id
    ) {
      void viewLogs(latest.id);
    }
  });

  async function addFunction(e: SubmitEvent) {
    e.preventDefault();
    if (addBusy || !app) return;
    addBusy = true;
    addErr = null;
    try {
      const fn = await api.createFunction(id, {
        name: newFnName,
        route: newFnRoute || undefined
      });
      await api.putSource(id, fn.id, initialFiles(app.language));
      newFnName = '';
      newFnRoute = '';
      addOpen = false;
      await loadApp();
    } catch (e) {
      addErr = (e as Error).message;
    } finally {
      addBusy = false;
    }
  }

  async function removeFunction(fnId: string, name: string) {
    if (!confirm(`Delete function "${name}"?`)) return;
    try {
      await api.deleteFunction(id, fnId);
      await loadApp();
    } catch (e) {
      alert((e as Error).message);
    }
  }

  async function onDelete() {
    if (!app) return;
    if (!confirm(`Delete application "${app.name}"? This removes the SpinApp too.`)) return;
    deleting = true;
    try {
      await api.deleteApplication(id);
      await goto('/');
    } catch (e) {
      loadErr = (e as Error).message;
      deleting = false;
    }
  }

  const status = $derived.by(() => {
    if (!app?.deployment) return { label: 'not deployed', cls: '' };
    // "deploying" beats "ready" — an old pod may be Ready while a new build
    // is still rolling; showing "ready" then would mask stale responses.
    if (app.deployment.progressing) return { label: 'deploying', cls: 'wait' };
    if (app.deployment.ready) return { label: 'ready', cls: 'ok' };
    if (app.deployment.observedReplicas > 0) return { label: 'partial', cls: 'wait' };
    return { label: 'pending', cls: 'wait' };
  });

  function buildPillClass(s: string): string {
    if (s === 'succeeded') return 'ok';
    if (s === 'failed') return 'fail';
    return 'wait';
  }
  const shortId = (s: string) => s.slice(0, 8);
  function timeAgo(iso: string): string {
    const t = new Date(iso).getTime();
    const s = Math.floor((Date.now() - t) / 1000);
    if (s < 60) return `${s}s ago`;
    if (s < 3600) return `${Math.floor(s / 60)}m ago`;
    return `${Math.floor(s / 3600)}h ago`;
  }
</script>

<a class="text-fg-muted no-underline text-sm hover:text-fg" href="/">← All applications</a>

{#if loadErr && !app}
  <p class="error">Failed to load: {loadErr}</p>
{:else if !app}
  <p class="muted">Loading…</p>
{:else}
  <div class="flex justify-between items-start gap-4 mt-4 mb-6">
    <div>
      <h2 class="mt-0 mb-1.5">{app.name}</h2>
      <div class="flex gap-1.5 mb-1">
        <span class="pill">{app.language}</span>
        <span class="pill" title="Runtime">{app.runtime}</span>
        <span class="pill {status.cls}">{status.label}</span>
      </div>
      {#if app.description}<p class="muted">{app.description}</p>{/if}
    </div>
    <button class="button danger" onclick={onDelete} disabled={deleting}>
      {deleting ? 'Deleting…' : 'Delete'}
    </button>
  </div>

  <section class="card">
    <h3>Deployment</h3>
    {#if app.deployment}
      <dl class="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-1.5 m-0">
        <dt class="text-fg-muted text-sm">Image</dt><dd class="m-0"><code class="text-sm bg-bg-elev px-1.5 py-0.5 rounded">{app.deployment.image}</code></dd>
        {#if app.deployment.imageSizeBytes != null}
          <dt class="text-fg-muted text-sm">Image size</dt><dd class="m-0">{formatBytes(app.deployment.imageSizeBytes)}</dd>
        {/if}
        <dt class="text-fg-muted text-sm">Replicas</dt><dd class="m-0">{app.deployment.observedReplicas} / {app.deployment.replicas} ready</dd>
        {#if app.deployment.message}<dt class="text-fg-muted text-sm">Status</dt><dd class="m-0 text-fg-muted">{app.deployment.message}</dd>{/if}
      </dl>
    {:else}
      <p class="muted">No SpinApp yet. Build the app to deploy.</p>
    {/if}
  </section>

  <section class="card">
    <div class="flex justify-between items-baseline">
      <h3>Functions</h3>
      <button class="button" onclick={() => (addOpen = !addOpen)}>
        {addOpen ? 'Cancel' : '+ Add function'}
      </button>
    </div>

    {#if addOpen}
      <form onsubmit={addFunction} class="flex gap-2 mt-2 items-center flex-wrap">
        <input
          bind:value={newFnName}
          placeholder="function name (DNS-1123)"
          required
          class="px-2 py-1.5 border border-border-strong rounded text-sm focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
        />
        <input
          bind:value={newFnRoute}
          placeholder="/route/... (default /{newFnName || 'name'}/...)"
          class="px-2 py-1.5 border border-border-strong rounded text-sm focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
        />
        {#if addErr}<span class="error">{addErr}</span>{/if}
        <button type="submit" class="button primary" disabled={addBusy}>
          {addBusy ? 'Adding…' : 'Add'}
        </button>
      </form>
    {/if}

    <ul class="list-none p-0 mt-2 border border-border rounded-md">
      {#each app.functions as f, i (f.id)}
        <li class="flex items-center gap-3 px-3 py-2 {i > 0 ? 'border-t border-border' : ''}">
          <a href="/applications/{id}/functions/{f.id}" class="text-fg no-underline flex-1 flex items-center gap-2 hover:underline">
            <strong>{f.name}</strong>
            <code class="text-fg-muted font-mono text-sm">{f.route}</code>
          </a>
          {#if app.functions.length > 1}
            <button
              class="bg-transparent border-none text-danger text-xs cursor-pointer p-0 hover:underline"
              onclick={() => removeFunction(f.id, f.name)}
            >remove</button>
          {/if}
        </li>
      {/each}
    </ul>
  </section>

  <section class="card">
    <div class="flex justify-between items-baseline">
      <h3>Builds</h3>
      <button class="button primary" onclick={startBuild} disabled={building}>
        {building ? 'Starting…' : 'Build & Deploy'}
      </button>
    </div>
    {#if buildErr}<p class="error">{buildErr}</p>{/if}

    {#if builds.length > 0}
      <ul class="list-none p-0 mt-2 border border-border rounded-md overflow-hidden">
        {#each builds as b, i (b.id)}
          <li class="{selectedBuildId === b.id ? 'bg-bg-elev' : ''} {i > 0 ? 'border-t border-border' : ''}">
            <button
              class="flex items-center gap-2 px-3 py-2 bg-transparent border-none w-full text-left cursor-pointer font-[inherit] hover:bg-bg-elev"
              onclick={() => viewLogs(b.id)}
            >
              <span class="pill {buildPillClass(b.status)}">{b.status}</span>
              <code class="font-mono">{shortId(b.id)}</code>
              <span class="text-fg-muted text-sm">{timeAgo(b.createdAt)}</span>
              {#if b.imageSizeBytes != null}
                <span class="text-fg-muted text-sm ml-auto tabular-nums" title="Compressed image size">{formatBytes(b.imageSizeBytes)}</span>
              {/if}
              {#if b.error}<span class="text-fg-muted text-sm truncate">— {b.error}</span>{/if}
            </button>
          </li>
        {/each}
      </ul>
      {#if selectedBuildId}
        <div class="mt-4 border border-border rounded-md bg-[#0b1120]">
          <div class="flex items-center justify-between px-3 py-2 text-[#cbd5e1] bg-[#1e293b] border-b border-[#334155] rounded-t-md">
            <strong>Logs</strong>
            <button
              class="bg-[#334155] border-[#475569] text-[#f1f5f9] text-xs px-3 py-1.5 rounded-md border cursor-pointer disabled:opacity-50"
              onclick={() => viewLogs(selectedBuildId!)}
              disabled={logLoading}
            >
              {logLoading ? 'Streaming…' : 'Refresh'}
            </button>
          </div>
          <pre class="text-[#d1d5db] p-3 m-0 max-h-80 overflow-auto font-mono text-xs whitespace-pre-wrap">{logText || '(no output yet)'}</pre>
        </div>
      {/if}
    {:else}
      <p class="muted">No builds yet. Upload source for each function, then hit Build & Deploy.</p>
    {/if}
  </section>

  {#if app.deployment}
    <section class="card">
      <div class="flex justify-between items-baseline">
        <h3>Resource usage</h3>
        <label class="inline-flex items-center gap-1.5 text-sm text-fg-muted">
          <span>Range</span>
          <select
            bind:value={metricsRange}
            class="px-1.5 py-0.5 border border-border-strong rounded text-sm bg-white focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent"
          >
            <option value="5m">5 min</option>
            <option value="15m">15 min</option>
            <option value="1h">1 hour</option>
            <option value="6h">6 hours</option>
          </select>
        </label>
      </div>
      <div class="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-4 mt-2">
        <MetricChart title="CPU" points={metrics?.series.cpu?.points} unit="cores" color="#3b82f6" />
        <MetricChart title="Memory" points={metrics?.series.memory?.points} unit="bytes" color="#059669" />
      </div>
    </section>

    {@const d = app.deployment}
    {@const isWorkerpool = app.runtime === 'workerpool'}
    {@const pfCmd = `kubectl -n ${d.namespace} port-forward svc/${d.serviceName} 8080:80`}
    <section class="card">
      <h3>Invoke</h3>
      {#if d.publicUrl}
        <div class="flex items-center gap-2 py-1.5">
          <span class="text-fg-muted text-xs uppercase tracking-wider min-w-20">Public</span>
          <code class="flex-1 font-mono text-sm px-2 py-1.5 bg-bg-elev rounded overflow-x-auto whitespace-nowrap">{d.publicUrl}</code>
          <button class="button" onclick={() => navigator.clipboard.writeText(d.publicUrl!)}>Copy</button>
        </div>
      {/if}
      <div class="flex items-center gap-2 py-1.5 border-t border-dashed border-border">
        <span class="text-fg-muted text-xs uppercase tracking-wider min-w-20">In-cluster</span>
        <code class="flex-1 font-mono text-sm px-2 py-1.5 bg-bg-elev rounded overflow-x-auto whitespace-nowrap">{d.internalUrl}</code>
        <button class="button" onclick={() => navigator.clipboard.writeText(d.internalUrl)}>Copy</button>
      </div>
      {#if !isWorkerpool}
        <div class="flex items-center gap-2 py-1.5 border-t border-dashed border-border">
          <span class="text-fg-muted text-xs uppercase tracking-wider min-w-20">Local dev</span>
          <code class="flex-1 font-mono text-sm px-2 py-1.5 bg-bg-elev rounded overflow-x-auto whitespace-nowrap">{pfCmd}</code>
          <button class="button" onclick={() => navigator.clipboard.writeText(pfCmd)}>Copy</button>
        </div>
      {/if}
      <p class="muted text-sm mt-2">
        {#if isWorkerpool}
          Workerpool apps run inside the shared spinup-worker (no per-app Service).
          Use the "Try it out" panel on a function's page — it routes through the
          control plane to the worker.
        {:else}
          Use the "Try it out" panel on a function's page to send requests through
          the control plane without a port-forward.
        {/if}
      </p>
    </section>
  {/if}
{/if}
