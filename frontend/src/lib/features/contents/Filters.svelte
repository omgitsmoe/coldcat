<script lang="ts">
  import { untrack } from 'svelte';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { contentsURL, type ContentsRoute } from '../../state/routes';
  import ReplicaBounds from '../../components/ReplicaBounds.svelte';
  import ResourcePicker from '../../components/ResourcePicker.svelte';
  import { boundsMode, boundsQuery } from '../search/filters';
  import { contentFilterCount } from './filters';
  let {
    query,
    retire,
    resume,
    bookmarkedDraft = false,
  }: {
    query: ContentsRoute;
    retire: () => void;
    resume: () => void;
    bookmarkedDraft?: boolean;
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
  let mode = $state(
    boundsMode(
      initial.other_replicas ?? '',
      initial.min_other_replicas ?? '',
      initial.max_other_replicas ?? '',
    ),
  );
  let advanced = $state(contentFilterCount(initial) > 0);
  let limit = $state(String(initial.limit));
  const validation = $derived.by(() => {
    try {
      return { url: draftURL(), error: '' };
    } catch (cause) {
      return { url: undefined, error: cause instanceof Error ? cause.message : 'Invalid filters' };
    }
  });
  function draftURL() {
    if (!/^[1-9][0-9]*$/.test(limit)) throw new Error('Page limit must be a positive integer.');
    return contentsURL({
      scope,
      disk_id: disk || undefined,
      directory: useDirectory ? directory : undefined,
      replica_metric: metric,
      ...boundsQuery({ mode, exact, minimum: min, maximum: max }),
      limit: Number(limit),
    });
  }
  function preset(value: 'zero' | 'some' | 'clear') {
    retire();
    exact = value === 'zero' ? '0' : '';
    min = value === 'some' ? '1' : '';
    max = '';
    mode = value === 'zero' ? 'exact' : value === 'some' ? 'range' : 'any';
    if (value !== 'clear') {
      scope = 'current';
      metric = 'disks';
    }
  }
  function apply(event: globalThis.SubmitEvent) {
    event.preventDefault();
    const url = validation.url;
    if (url) {
      if (!bookmarkedDraft && url === contentsURL(query)) resume();
      else void goto(resolve(`/contents?${url.split('?')[1]}`));
    }
  }
</script>

<form onsubmit={apply} oninput={retire} onchange={retire}>
  <div class="control-group">
    <label class="field"
      >Scope <select bind:value={scope}
        ><option value="current">Current</option><option value="history">History</option></select
      ></label
    >
    <label class="field">Page limit <input inputmode="numeric" bind:value={limit} /></label>
    <button class="button-primary">Apply filters</button>
  </div>
  <details bind:open={advanced}>
    <summary
      >Advanced filters ({[
        disk !== '',
        useDirectory,
        metric === 'locations',
        mode !== 'any',
      ].filter(Boolean).length})</summary
    >
    <div class="advanced">
      <fieldset>
        <legend>Membership</legend>
        <ResourcePicker bind:value={disk} edited={retire} />
        <label class="check-field"
          ><input type="checkbox" bind:checked={useDirectory} /> Filter directory membership</label
        >
        <label class="field"
          >Directory <input
            class="literal"
            bind:value={directory}
            disabled={!useDirectory}
          /></label
        >
        <p>
          Directory requires a disk ID. Empty directory means root; paths are literal and
          case-sensitive.
        </p>
      </fieldset>
      <fieldset>
        <legend>Current redundancy</legend>
        <label class="field"
          >Current replica metric <select bind:value={metric}
            ><option value="disks">Other disks</option><option value="locations"
              >Other locations</option
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
        <p>
          Exact and range bounds are mutually exclusive. Bounds require current scope; switching to
          history does not clear them.
        </p>
        <button type="button" onclick={() => preset('zero')}>Zero other disks</button>
        <button type="button" onclick={() => preset('some')}>At least one other disk</button>
        <button type="button" onclick={() => preset('clear')}>Clear bounds</button>
        <p>Presets update the visible draft values. Apply to request results.</p>
      </fieldset>
    </div>
  </details>
  {#if validation.error}<p role="alert">{validation.error}</p>{/if}
</form>

<style>
  form {
    display: grid;
    gap: var(--space-3);
  }
  .advanced {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
  .control-group input {
    width: 6rem;
  }
  @media (max-width: 900px) {
    .advanced {
      grid-template-columns: 1fr;
    }
  }
</style>
