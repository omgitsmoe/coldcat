<script lang="ts">
  import { untrack } from 'svelte';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { directoryBrowseURL, type DirectoryRoute } from '../../state/routes';
  import ReplicaBounds from '../../components/ReplicaBounds.svelte';
  import { boundsMode, boundsQuery } from '../search/filters';
  import { decimal } from '../../format/decimal';
  let {
    id,
    query,
    retire,
    resume,
    returnTo,
  }: {
    id: string;
    query: DirectoryRoute;
    retire: () => void;
    resume: () => void;
    returnTo?: string;
  } = $props();
  const initial = untrack(() => query);
  let recursive = $state(initial.recursive ?? false);
  let metric = $state(initial.replica_metric ?? 'disks');
  let exact = $state(initial.other_replicas ?? '');
  let min = $state(initial.min_other_replicas ?? '');
  let max = $state(initial.max_other_replicas ?? '');
  let limit = $state(String(initial.limit));
  let mode = $state(
    boundsMode(
      initial.other_replicas ?? '',
      initial.min_other_replicas ?? '',
      initial.max_other_replicas ?? '',
    ),
  );
  const validation = $derived.by(() => {
    try {
      if (!/^[1-9][0-9]*$/.test(limit)) throw new Error('Page limit must be a positive integer.');
      const bounds = boundsQuery({ mode, exact, minimum: min, maximum: max });
      for (const value of Object.values(bounds)) if (value !== undefined) decimal(value);
      if (
        bounds.min_other_replicas !== undefined &&
        bounds.max_other_replicas !== undefined &&
        decimal(bounds.min_other_replicas) > decimal(bounds.max_other_replicas)
      ) {
        throw new Error('Minimum exceeds maximum.');
      }
      return {
        url: directoryBrowseURL(
          id,
          {
            path: query.path,
            recursive,
            replica_metric: metric,
            ...bounds,
            limit: Number(limit),
          },
          { returnTo },
        ),
        error: '',
      };
    } catch (cause) {
      return { url: undefined, error: cause instanceof Error ? cause.message : 'Invalid filters' };
    }
  });
  function apply(event: globalThis.SubmitEvent) {
    event.preventDefault();
    const url = validation.url;
    if (url) {
      if (url === directoryBrowseURL(id, query, { returnTo })) resume();
      else void goto(resolve(`/snapshots/[id]/directory?${url.split('?')[1]}`, { id }));
    }
  }
</script>

<form onsubmit={apply} oninput={retire} onchange={retire}>
  <div class="control-group">
    <label class="check-field"
      ><input type="checkbox" bind:checked={recursive} /> Recursive files</label
    >
    <label class="field"
      >Current replica metric <select bind:value={metric}
        ><option value="disks">Other disks</option><option value="locations">Other locations</option
        ></select
      ></label
    >
    <ReplicaBounds
      bind:mode
      bind:exact
      bind:minimum={min}
      bind:maximum={max}
      invalid={!!validation.error}
    />
    <label class="field">Page limit <input inputmode="numeric" bind:value={limit} /></label>
    <button class="button-primary">Apply filters</button>
  </div>
  {#if validation.error}<p role="alert">{validation.error}</p>{/if}
</form>

<style>
  .control-group {
    align-items: end;
  }
  input:not([type='checkbox']) {
    width: 6rem;
  }
</style>
