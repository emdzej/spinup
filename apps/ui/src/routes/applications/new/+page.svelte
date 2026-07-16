<script lang="ts">
  import { goto } from '$app/navigation';
  import { api } from '$lib/api';
  import { initialFiles } from '$lib/templates';
  import type { Language, Runtime } from '$lib/types';

  let name = $state('');
  let language = $state<Language>('go');
  let runtime = $state<Runtime>('spinkube');
  let description = $state('');
  let submitting = $state(false);
  let error = $state<string | null>(null);

  const nameValid = $derived(/^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/.test(name));

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!nameValid || submitting) return;
    submitting = true;
    error = null;
    try {
      const app = await api.createApplication({
        name,
        language,
        runtime,
        description: description || undefined
      });
      const firstFn = app.functions[0];
      if (firstFn) {
        await api.putSource(app.id, firstFn.id, initialFiles(language));
      }
      await goto(`/applications/${app.id}`);
    } catch (err) {
      error = (err as Error).message;
      submitting = false;
    }
  }

  const inputCls =
    'px-2.5 py-2 border border-border-strong rounded-md text-sm focus:outline-2 focus:outline-accent focus:-outline-offset-1 focus:border-transparent';
</script>

<a class="text-fg-muted no-underline text-sm hover:text-fg" href="/">← All applications</a>
<h2>New application</h2>

<form onsubmit={submit} class="max-w-[480px] flex flex-col gap-5 mt-4">
  <label class="flex flex-col gap-1.5">
    <span class="font-medium text-sm">Name</span>
    <input bind:value={name} placeholder="hello" autocomplete="off" required class={inputCls} />
    {#if name && !nameValid}
      <span class="text-xs text-danger">DNS-1123: lowercase letters, digits, hyphens; 1–63 chars.</span>
    {:else}
      <span class="text-xs text-fg-muted">
        Used as the SpinApp name and the default first function's name.
      </span>
    {/if}
  </label>

  <label class="flex flex-col gap-1.5">
    <span class="font-medium text-sm">Language</span>
    <select bind:value={language} class={inputCls}>
      <option value="go">Go</option>
      <option value="js">JavaScript</option>
      <option value="ts">TypeScript</option>
      <option value="rust">Rust</option>
    </select>
    <span class="text-xs text-fg-muted">
      All functions in an application share this language. Add more functions
      with distinct routes after the app is created.
    </span>
  </label>

  <label class="flex flex-col gap-1.5">
    <span class="font-medium text-sm">Runtime</span>
    <select bind:value={runtime} class={inputCls}>
      <option value="spinkube">SpinKube — one pod per app</option>
      <option value="workerpool">Worker pool — shared wasmtime process</option>
    </select>
    {#if runtime === 'spinkube'}
      <span class="text-xs text-fg-muted">
        Default. Each app gets its own K8s Deployment / pod running the
        containerd-shim-spin. Always-on, ~30 MB idle RSS per app, sub-ms
        per-request instantiation once warm.
      </span>
    {:else}
      <span class="text-xs text-fg-muted">
        The shared <code>spinup-worker</code> process loads this app's WASM
        on demand. No per-app K8s objects. Cold start on first request
        (~50–500 ms), warm requests match SpinKube. Requires the worker
        component to be deployed alongside the control plane.
      </span>
    {/if}
  </label>

  <label class="flex flex-col gap-1.5">
    <span class="font-medium text-sm">Description <em>(optional)</em></span>
    <input bind:value={description} placeholder="what does this app do?" class={inputCls} />
  </label>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  <div class="flex justify-end gap-2">
    <a class="button" href="/">Cancel</a>
    <button type="submit" class="button primary" disabled={!nameValid || submitting}>
      {submitting ? 'Creating…' : 'Create'}
    </button>
  </div>
</form>
