<script lang="ts">
  import { api } from '$lib/api';
  import type { ApplicationSummary } from '$lib/types';

  let apps = $state<ApplicationSummary[]>([]);
  let error = $state<string | null>(null);
  let loading = $state(true);
  let filter = $state('');

  // Case-insensitive substring match across name, language, runtime, and
  // description — one input, no filter-selector UI. Empty query shows all.
  const filtered = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return apps;
    return apps.filter((a) =>
      [a.name, a.language, a.runtime, a.description ?? '']
        .some((s) => s.toLowerCase().includes(q)),
    );
  });

  async function load() {
    try {
      loading = true;
      error = null;
      apps = await api.listApplications();
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void load();
  });
</script>

<div class="flex items-center justify-between gap-4 mb-4">
  <h2 class="m-0">Applications</h2>
  <a class="button primary" href="/applications/new">New application</a>
</div>

{#if loading}
  <p class="muted">Loading…</p>
{:else if error}
  <p class="error">Failed to load: {error}</p>
{:else if apps.length === 0}
  <div class="text-center px-4 py-12 border border-dashed border-border-strong rounded-lg text-fg-muted">
    <p>No applications yet.</p>
    <a class="button primary mt-4" href="/applications/new">Create your first application</a>
  </div>
{:else}
  <div class="mb-3 flex items-center gap-3">
    <input
      type="search"
      placeholder="Filter by name, language, or runtime…"
      class="flex-1 border border-border-strong rounded-md px-3 py-1.5 text-sm"
      bind:value={filter}
    />
    <span class="text-fg-muted text-xs">
      {filtered.length}{#if filtered.length !== apps.length} / {apps.length}{/if}
    </span>
  </div>

  {#if filtered.length === 0}
    <p class="muted text-sm">No applications match &ldquo;{filter}&rdquo;.</p>
  {:else}
    <ul class="list-none p-0">
      {#each filtered as a (a.id)}
        <li class="border-b border-border">
          <a href="/applications/{a.id}" class="block py-3.5 px-1 text-fg no-underline hover:bg-bg-elev">
            <strong>{a.name}</strong>
            <span class="text-fg-muted text-sm ml-2">{a.language}</span>
            <span class="text-fg-muted text-[0.7rem] ml-2 px-2 py-px border border-border rounded-full uppercase tracking-wider">{a.runtime}</span>
            {#if a.description}<div class="text-fg-muted mt-1 text-sm">{a.description}</div>{/if}
          </a>
        </li>
      {/each}
    </ul>
  {/if}
{/if}
