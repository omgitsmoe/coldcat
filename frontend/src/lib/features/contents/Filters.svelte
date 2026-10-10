<script lang="ts">
  import { untrack } from 'svelte';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { contentsURL, type ContentsRoute } from '../../state/routes';
  let {
    query,
    retire,
    resume,
  }: {
    query: ContentsRoute;
    retire: () => void;
    resume: () => void;
  } = $props();
  const initial = untrack(() => query);
  let scope = $state(initial.scope ?? 'current');
  let disk = $state(initial.disk_id ?? '');
  let useDirectory = $state(initial.directory !== undefined);
  let directory = $state(initial.directory ?? '');
  let metric = $state(initial.replica_metric ?? 'disks');
  let exact = $state(initial.other_replicas ?? '');
  let min = $state(initial.min_other_replicas ?? '');
  let max = $state(initial.max_other_replicas ?? '');
  let limit = $state(String(initial.limit));
  let error = $state('');
  function preset(value: 'zero' | 'some' | 'clear') {
    retire();
    exact = value === 'zero' ? '0' : '';
    min = value === 'some' ? '1' : '';
    max = '';
    if (value !== 'clear') {
      scope = 'current';
      metric = 'disks';
    }
  }
  function apply(event: globalThis.SubmitEvent) {
    event.preventDefault();
    try {
      if (!/^[1-9][0-9]*$/.test(limit)) throw new Error('Page limit must be a positive integer.');
      const url = contentsURL({
        scope,
        disk_id: disk || undefined,
        directory: useDirectory ? directory : undefined,
        replica_metric: metric,
        other_replicas: exact === '' ? undefined : exact,
        min_other_replicas: min === '' ? undefined : min,
        max_other_replicas: max === '' ? undefined : max,
        limit: Number(limit),
      });
      error = '';
      if (url === contentsURL(query)) resume();
      else void goto(resolve(`/contents?${url.split('?')[1]}`));
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Invalid filters';
    }
  }
</script>

<form onsubmit={apply} oninput={retire} onchange={retire}>
  <fieldset>
    <legend>Content filters</legend>
    <label
      >Scope <select bind:value={scope}
        ><option value="current">Current</option><option value="history">History</option></select
      ></label
    >
    <label>Disk ID <input inputmode="numeric" bind:value={disk} /></label>
    <label><input type="checkbox" bind:checked={useDirectory} /> Filter directory membership</label>
    <label>Directory <input bind:value={directory} disabled={!useDirectory} /></label>
    <p>
      Directory requires a disk ID. Empty directory means root; paths are literal and
      case-sensitive.
    </p>
    <label
      >Current replica metric <select bind:value={metric}
        ><option value="disks">Other disks</option><option value="locations">Other locations</option
        ></select
      ></label
    >
    <label>Exact other replicas <input inputmode="numeric" bind:value={exact} /></label>
    <label>Minimum other replicas <input inputmode="numeric" bind:value={min} /></label>
    <label>Maximum other replicas <input inputmode="numeric" bind:value={max} /></label>
    <p>
      Exact and range bounds are mutually exclusive. Bounds require current scope; switching to
      history does not clear them.
    </p>
    <button type="button" onclick={() => preset('zero')}>Zero other disks</button>
    <button type="button" onclick={() => preset('some')}>At least one other disk</button>
    <button type="button" onclick={() => preset('clear')}>Clear bounds</button>
    <p>Presets update the visible draft values. Apply to request results.</p>
    <label>Page limit <input inputmode="numeric" bind:value={limit} /></label>
    <button>Apply filters</button>
  </fieldset>
  {#if error}<p role="alert">{error}</p>{/if}
</form>

<style>
  label {
    display: block;
    margin-block: 0.5rem;
  }
</style>
