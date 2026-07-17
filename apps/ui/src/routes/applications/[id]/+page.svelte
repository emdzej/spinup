<script lang="ts">
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { onMount, onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import { initialFiles } from '$lib/templates';
  import MetricChart from '$lib/MetricChart.svelte';
  import { formatBytes } from '$lib/format';
  import type { ApplicationDetail, Build, MetricsResponse, PlatformPolicy } from '$lib/types';

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
  // Paged builds view: start with 5, "Load more" bumps by 5. When the last
  // fetch returned fewer than `buildsLimit` rows there's nothing left to show.
  let buildsLimit = $state(5);
  const canLoadMore = $derived(builds.length >= buildsLimit);

  let addOpen = $state(false);
  let newFnName = $state('');
  let newFnRoute = $state('');
  let addErr = $state<string | null>(null);
  let addBusy = $state(false);
  // Ref to the "Add function" name input so we can focus it when the form
  // opens — one-shot, so the user can start typing without a click.
  let addNameEl = $state<HTMLInputElement | null>(null);
  $effect(() => {
    if (addOpen) addNameEl?.focus();
  });

  // Functions list filter (same UX as the applications list).
  let fnFilter = $state('');
  const filteredFunctions = $derived.by(() => {
    if (!app) return [];
    const q = fnFilter.trim().toLowerCase();
    if (!q) return app.functions;
    return app.functions.filter((f) =>
      [f.name, f.route].some((s) => s.toLowerCase().includes(q)),
    );
  });

  let metricsRange = $state('15m');
  let metrics = $state<MetricsResponse | null>(null);

  type Tab = 'general' | 'functions' | 'configuration' | 'builds';
  let tab = $state<Tab>('general');
  const tabs: { id: Tab; label: string }[] = [
    { id: 'general',       label: 'General' },
    { id: 'functions',     label: 'Functions' },
    { id: 'configuration', label: 'Configuration' },
    { id: 'builds',        label: 'Builds' },
  ];

  // Editable app-level config (replicas / variables / resources). Copied out
  // of `app` on load, PATCHed on save, then merged back.
  let cfg = $state<{
    replicas: number;
    variables: { name: string; value: string }[];
    resources: {
      cpuRequest: string; cpuLimit: string;
      memoryRequest: string; memoryLimit: string;
    };
  } | null>(null);
  let cfgSaving = $state(false);
  let cfgErr = $state<string | null>(null);
  let cfgOk = $state(false);
  let policy = $state<PlatformPolicy | null>(null);
  // Disable resource inputs entirely when the platform enforces forced mode —
  // stored values will be overridden by the platform config on save anyway.
  const resourcesLocked = $derived(policy?.resources?.mode === 'forced');
  function syncCfgFromApp() {
    if (!app) { cfg = null; return; }
    cfg = {
      replicas: app.replicas ?? 1,
      variables: app.variables.map((v) => ({ name: v.name, value: v.value })),
      resources: {
        cpuRequest:    app.resources.cpuRequest    ?? '',
        cpuLimit:      app.resources.cpuLimit      ?? '',
        memoryRequest: app.resources.memoryRequest ?? '',
        memoryLimit:   app.resources.memoryLimit   ?? '',
      },
    };
  }
  async function saveCfg() {
    if (!cfg) return;
    cfgSaving = true; cfgErr = null; cfgOk = false;
    try {
      const cleanVars = cfg.variables
        .map((v) => ({ name: v.name.trim(), value: v.value }))
        .filter((v) => v.name.length > 0);
      const res = cfg.resources;
      const anyRes = res.cpuRequest || res.cpuLimit || res.memoryRequest || res.memoryLimit;
      app = await api.updateApplication(id, {
        replicas: cfg.replicas,
        variables: cleanVars,
        resources: anyRes ? {
          cpuRequest:    res.cpuRequest    || undefined,
          cpuLimit:      res.cpuLimit      || undefined,
          memoryRequest: res.memoryRequest || undefined,
          memoryLimit:   res.memoryLimit   || undefined,
        } : {},
      });
      syncCfgFromApp();
      cfgOk = true;
    } catch (e) {
      cfgErr = (e as Error).message;
    } finally {
      cfgSaving = false;
    }
  }

  async function loadApp() {
    try {
      app = await api.getApplication(id);
      loadErr = null;
      syncCfgFromApp();
    } catch (e) {
      loadErr = (e as Error).message;
    }
  }
  async function loadBuilds() {
    try {
      builds = await api.listBuilds(id, buildsLimit);
    } catch {
      /* ignore poll */
    }
  }
  async function loadMoreBuilds() {
    buildsLimit += 5;
    await loadBuilds();
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

  async function loadPolicy() {
    try {
      policy = await api.policy();
    } catch {
      policy = { resources: { mode: 'open' } };
    }
  }

  let poll: ReturnType<typeof setInterval> | undefined;
  onMount(() => {
    void loadPolicy();
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
    <div class="flex items-center gap-2">
      <button class="button primary" onclick={startBuild} disabled={building}>
        {building ? 'Starting…' : 'Build & Deploy'}
      </button>
      <button class="button danger" onclick={onDelete} disabled={deleting}>
        {deleting ? 'Deleting…' : 'Delete'}
      </button>
    </div>
  </div>

  {#if buildErr}<p class="error mb-3">Build error: {buildErr}</p>{/if}

  <!-- Tab bar. Underlined text-buttons; active one gets the accent border. -->
  <nav class="border-b border-border mb-4 flex gap-6">
    {#each tabs as t (t.id)}
      <button
        type="button"
        onclick={() => (tab = t.id)}
        class="py-2 px-0.5 text-sm bg-transparent border-0 border-b-2 cursor-pointer transition-colors
               {tab === t.id ? 'border-accent text-fg font-medium' : 'border-transparent text-fg-muted hover:text-fg'}"
      >{t.label}</button>
    {/each}
  </nav>

  {#if tab === 'general'}
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
  {/if}

  {#if tab === 'functions'}
  <section class="card">
    <div class="flex justify-end items-baseline">
      <button class="button" onclick={() => (addOpen = !addOpen)}>
        {addOpen ? 'Cancel' : '+ Add function'}
      </button>
    </div>

    {#if addOpen}
      <form
        onsubmit={addFunction}
        onkeydown={(e) => { if (e.key === 'Escape') { addOpen = false; addErr = null; } }}
        class="flex gap-2 mt-2 items-center flex-wrap"
      >
        <input
          bind:value={newFnName}
          bind:this={addNameEl}
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

    {#if app.functions.length > 0}
      <div class="mt-3 flex items-center gap-3">
        <input
          type="search"
          placeholder="Filter by name or route…"
          class="flex-1 border border-border-strong rounded-md px-3 py-1.5 text-sm"
          bind:value={fnFilter}
        />
        <span class="text-fg-muted text-xs">
          {filteredFunctions.length}{#if filteredFunctions.length !== app.functions.length} / {app.functions.length}{/if}
        </span>
      </div>
    {/if}

    {#if filteredFunctions.length === 0 && fnFilter}
      <p class="muted text-sm mt-3">No functions match &ldquo;{fnFilter}&rdquo;.</p>
    {:else}
    <ul class="list-none p-0 mt-2 border border-border rounded-md">
      {#each filteredFunctions as f, i (f.id)}
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
    {/if}
  </section>
  {/if}

  {#if tab === 'configuration' && cfg && app.runtime !== 'workerpool'}
    <section class="card">
      <div class="flex justify-end items-center gap-2">
        {#if cfgErr}<span class="text-danger text-sm">{cfgErr}</span>{/if}
        {#if cfgOk}<span class="muted text-sm">Saved. Redeploys on next apply.</span>{/if}
        <button class="button primary" onclick={saveCfg} disabled={cfgSaving}>
          {cfgSaving ? 'Saving…' : 'Save'}
        </button>
      </div>

      <div class="grid grid-cols-2 gap-4 mt-3">
        <label class="flex flex-col gap-1">
          <span class="text-fg-muted text-xs uppercase tracking-wider">Replicas</span>
          <input
            type="number" min="0" max="20" step="1"
            class="border border-border-strong rounded-md px-2 py-1"
            bind:value={cfg.replicas}
          />
        </label>
      </div>

      <div class="mt-4">
        <div class="flex items-baseline justify-between mb-1">
          <span class="text-fg-muted text-xs uppercase tracking-wider">Variables</span>
          <button
            type="button"
            class="text-xs text-fg-muted hover:text-fg underline underline-offset-2"
            onclick={() => cfg && cfg.variables.push({ name: '', value: '' })}
          >
            + add
          </button>
        </div>
        <p class="muted text-xs mb-2">
          Accessible from every function via <code>@fermyon/spin-sdk</code>'s Variables API.
        </p>
        {#each cfg.variables as v, i}
          <div class="flex gap-2 mb-1">
            <input
              placeholder="NAME"
              class="flex-1 border border-border-strong rounded-md px-2 py-1 font-mono text-sm"
              bind:value={v.name}
            />
            <input
              placeholder="value"
              class="flex-[2] border border-border-strong rounded-md px-2 py-1 font-mono text-sm"
              bind:value={v.value}
            />
            <button
              type="button"
              class="button danger"
              onclick={() => cfg && cfg.variables.splice(i, 1)}
            >×</button>
          </div>
        {/each}
      </div>

      <div class="mt-4">
        <div class="flex items-baseline gap-2">
          <span class="text-fg-muted text-xs uppercase tracking-wider">Resources</span>
          {#if policy?.resources.mode === 'max'}
            <span class="pill wait">policy: max</span>
          {:else if policy?.resources.mode === 'forced'}
            <span class="pill fail">policy: forced by platform</span>
          {/if}
        </div>
        <p class="muted text-xs mb-2">
          {#if resourcesLocked}
            Values are set by the platform administrator and can't be changed here.
          {:else if policy?.resources.mode === 'max'}
            K8s quantity strings; values above the platform cap are rejected on save.
          {:else}
            K8s quantity strings (e.g. <code>100m</code>, <code>128Mi</code>). Blank = unset.
          {/if}
        </p>
        <div class="grid grid-cols-4 gap-2">
          <label class="flex flex-col gap-1">
            <span class="text-fg-muted text-xs">CPU request{#if policy?.resources.cpuRequest} (≤ {policy.resources.cpuRequest}){/if}</span>
            <input class="border border-border-strong rounded-md px-2 py-1 font-mono text-sm disabled:opacity-50" placeholder={policy?.resources.cpuRequest || '100m'} disabled={resourcesLocked} bind:value={cfg.resources.cpuRequest} />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-fg-muted text-xs">CPU limit{#if policy?.resources.cpuLimit} (≤ {policy.resources.cpuLimit}){/if}</span>
            <input class="border border-border-strong rounded-md px-2 py-1 font-mono text-sm disabled:opacity-50" placeholder={policy?.resources.cpuLimit || '500m'} disabled={resourcesLocked} bind:value={cfg.resources.cpuLimit} />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-fg-muted text-xs">Mem request{#if policy?.resources.memoryRequest} (≤ {policy.resources.memoryRequest}){/if}</span>
            <input class="border border-border-strong rounded-md px-2 py-1 font-mono text-sm disabled:opacity-50" placeholder={policy?.resources.memoryRequest || '128Mi'} disabled={resourcesLocked} bind:value={cfg.resources.memoryRequest} />
          </label>
          <label class="flex flex-col gap-1">
            <span class="text-fg-muted text-xs">Mem limit{#if policy?.resources.memoryLimit} (≤ {policy.resources.memoryLimit}){/if}</span>
            <input class="border border-border-strong rounded-md px-2 py-1 font-mono text-sm disabled:opacity-50" placeholder={policy?.resources.memoryLimit || '512Mi'} disabled={resourcesLocked} bind:value={cfg.resources.memoryLimit} />
          </label>
        </div>
      </div>
    </section>
  {/if}

  {#if tab === 'builds'}
  <section class="card">
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
      {#if canLoadMore}
        <div class="mt-2 text-center">
          <button class="button" onclick={loadMoreBuilds}>Load more</button>
        </div>
      {/if}
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
  {/if}

  {#if tab === 'general' && app.deployment}
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
