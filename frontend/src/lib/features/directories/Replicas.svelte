<script lang="ts">
  import { onDestroy } from 'svelte';
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { useConnection } from '../../state/context';
  import { replicaPages } from '../../state/feature-requests';
  import type { TraversalState } from '../../state/traversal';
  import { directoryBrowseURL, routes } from '../../state/routes';
  import { formatCount } from '../../format/decimal';
  import PageControls from '../../components/PageControls.svelte';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import DateValue from '../../components/DateValue.svelte';
  import RuleEditor from './RuleEditor.svelte';
  import type { Rules } from './rules';
  let { id, path }: { id: string; path: string } = $props();
  const connection = useConnection();
  type Page = Result<'GET /api/v1/snapshots/{id}/directory/replicas'>;
  let draft = $state<Rules>({ allow: [], block: [] });
  let applied = $state<Rules>();
  let dirty = $state(false);
  let limit = $state('50');
  let error = $state('');
  let owner: ReturnType<typeof replicaPages> | undefined;
  let unsubscribe: (() => void) | undefined;
  let view = $state<TraversalState<Page>>({ status: 'idle', pages: [], active: 0, paged: false });
  let items = $state<Page['items']>([]);
  let paging = $state({ busy: false, canMore: false, canNext: false, canPrevious: false });
  const first = $derived(view.pages[0]?.value);
  function dispose() {
    unsubscribe?.();
    owner?.dispose();
    owner = undefined;
  }
  onDestroy(dispose);
  function changed(rules = draft) {
    draft = rules;
    dirty = true;
    error = '';
    owner?.traversal.retire();
  }
  function apply(rules: Rules) {
    dispose();
    try {
      if (!/^[1-9]\d*$/.test(limit) || BigInt(limit) > 200n) {
        throw new Error('Page size must be 1–200');
      }
      owner = replicaPages(connection, id, { path, ...rules, limit: Number(limit) });
      applied = rules;
      dirty = false;
      error = '';
      const traversal = owner.traversal;
      unsubscribe = traversal.subscribe((state) => {
        view = state;
        items = traversal.items;
        paging = {
          busy: traversal.busy,
          canMore: traversal.canMore,
          canNext: traversal.canNext,
          canPrevious: traversal.canPrevious,
        };
      });
      void traversal.restart();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Invalid comparison';
    }
  }
  function retry() {
    const traversal = owner?.traversal;
    if (!traversal) return;
    const disconnected =
      view.error instanceof ApiError &&
      (view.error.kind === 'transport' || view.error.status === 503);
    if (!view.pages.length || view.status === 'stale' || disconnected) void traversal.restart();
    else if (view.paged || view.pages.length >= 10) void traversal.next();
    else void traversal.more();
  }
</script>

<section aria-label="Exact tree copies">
  <h2>Exact tree copies</h2>
  <p>
    Compare relative file paths and content identities, ignoring root names and mtimes. Destinations
    are current inventories only, even for a historical source.
  </p>
  <RuleEditor {draft} {changed} {apply} />
  <label
    >Comparison page size
    <input
      value={limit}
      inputmode="numeric"
      oninput={(event) => {
        limit = event.currentTarget.value;
        changed();
      }}
    />
  </label>
  {#if error}<p role="alert">{error}</p>{/if}
  {#if dirty}<p role="status">Rules changed; apply to compare.</p>
  {:else if applied}
    <h3>Applied rules</h3>
    {#each ['allow', 'block'] as kind (kind)}
      {@const key = kind as keyof Rules}
      <p>{key === 'allow' ? 'Allow' : 'Block'}:</p>
      {#if applied[key].length}<ul>
          {#each applied[key] as row, index (index)}<li class="literal">{row}</li>{/each}
        </ul>
      {:else}<p>None</p>{/if}
    {/each}
    <RequestFeedback
      status={view.status}
      error={view.error}
      resource="Exact tree copies"
      {retry}
      loadingLabel="Comparing trees…"
    />
    {#if paging.busy}<button onclick={() => changed()}>Cancel comparison</button>{/if}
    {#if view.status === 'idle'}<p>Comparison invalidated; apply to compare again.</p>{/if}
    {#if first}
      {#if view.error}<p>Previously loaded copies; not freshly verified.</p>{/if}
      <p>
        {first.is_current ? 'Current' : 'Historical'} source inventory compared against current destination
        inventories.
      </p>
      <p>
        Selected source files: {formatCount(first.selection.retained_file_count)}; excluded source
        files: {formatCount(first.selection.excluded_file_count)}.
      </p>
      {#if first.selection.empty_comparison}<p>No files selected</p>
      {:else}
        {#if !items.length}<p>No exact tree copies match this selection.</p>{/if}
        <ul>
          {#each items as item (`${item.disk.id}:${item.snapshot.id}:${item.path}`)}
            <li>
              <a href="{resolve('/')}{routes.disk(item.disk.id).slice(1)}"
                >{item.disk.label} — Disk {item.disk.id}</a
              >
              {#if item.same_disk}<strong>Same disk</strong>{/if}
              <a
                class="literal"
                href="{resolve('/')}{directoryBrowseURL(item.snapshot.id, {
                  path: item.path,
                }).slice(1)}">{item.path || 'Root'}</a
              >
              <a href="{resolve('/')}{routes.snapshot(item.snapshot.id).slice(1)}"
                >Inventory {item.snapshot.id}</a
              >
              <DateValue value={item.snapshot.captured_at} meaning="captured_at" />
              <p>{item.whole_tree_equal ? 'Whole tree equal' : 'Equal under these filters'}</p>
              <p>
                Retained destination files: {formatCount(item.retained_file_count)}; excluded
                destination files: {formatCount(item.excluded_file_count)}.
              </p>
            </li>
          {/each}
        </ul>
        <PageControls
          {...paging}
          paged={view.paged}
          page={view.active}
          firstRetained={view.pages[0]!.number}
          loaded={items.length}
          complete={view.pages.at(-1)!.value.next_cursor === null}
          more={() => void owner?.traversal.more()}
          next={() => void owner?.traversal.next()}
          previous={() => owner?.traversal.previous()}
        />
      {/if}
    {/if}
  {:else}<p>Apply rules to run a comparison. No comparison has been requested.</p>{/if}
</section>

<style>
  .literal {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  li {
    margin-block: 0.5rem;
  }
</style>
