<script lang="ts">
  import { untrack } from 'svelte';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { directoryBrowseURL, type DirectoryRoute } from '../../state/routes';
  import { decimal } from '../../format/decimal';
  let {
    id,
    query,
    retire,
    resume,
  }: { id: string; query: DirectoryRoute; retire: () => void; resume: () => void } = $props();
  const initial = untrack(() => query);
  let recursive = $state(initial.recursive ?? false);
  let metric = $state(initial.replica_metric ?? 'disks');
  let exact = $state(initial.other_replicas ?? '');
  let min = $state(initial.min_other_replicas ?? '');
  let max = $state(initial.max_other_replicas ?? '');
  let limit = $state(String(initial.limit));
  let error = $state('');
  function apply(event: globalThis.SubmitEvent) {
    event.preventDefault();
    try {
      if (exact !== '' && (min !== '' || max !== ''))
        throw new Error('Exact bounds cannot combine with range bounds.');
      for (const value of [exact, min, max]) if (value !== '') decimal(value);
      if (min !== '' && max !== '' && decimal(min) > decimal(max))
        throw new Error('Minimum exceeds maximum.');
      if (!/^[1-9][0-9]*$/.test(limit)) throw new Error('Page limit must be a positive integer.');
      const url = directoryBrowseURL(id, {
        path: query.path,
        recursive,
        replica_metric: metric,
        other_replicas: exact === '' ? undefined : exact,
        min_other_replicas: min === '' ? undefined : min,
        max_other_replicas: max === '' ? undefined : max,
        limit: Number(limit),
      });
      error = '';
      if (url === directoryBrowseURL(id, query)) resume();
      else void goto(resolve(`/snapshots/[id]/directory?${url.split('?')[1]}`, { id }));
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Invalid filters';
    }
  }
</script>

<form onsubmit={apply} oninput={retire} onchange={retire}>
  <fieldset>
    <legend>Entry filters</legend>
    <label><input type="checkbox" bind:checked={recursive} /> Recursive files</label>
    <label
      >Current replica metric <select bind:value={metric}
        ><option value="disks">Other disks</option><option value="locations">Other locations</option
        ></select
      ></label
    >
    <label>Exact other replicas <input inputmode="numeric" bind:value={exact} /></label>
    <label>Minimum other replicas <input inputmode="numeric" bind:value={min} /></label>
    <label>Maximum other replicas <input inputmode="numeric" bind:value={max} /></label>
    <label>Page limit <input inputmode="numeric" bind:value={limit} /></label>
    <button>Apply filters</button>
  </fieldset>
  <p>
    Bounds use the current catalog, even for a historical source. Immediate child directories stay
    navigable under file filters.
  </p>
  {#if error}<p role="alert">{error}</p>{/if}
</form>

<style>
  label {
    display: block;
    margin-block: 0.5rem;
  }
</style>
