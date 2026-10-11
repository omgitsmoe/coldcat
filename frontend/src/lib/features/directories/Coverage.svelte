<script lang="ts">
  import { onDestroy } from 'svelte';
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { useConnection } from '../../state/context';
  import { coveragePages } from '../../state/feature-requests';
  import type { TraversalState } from '../../state/traversal';
  import { routes } from '../../state/routes';
  import { formatCount, percentage } from '../../format/decimal';
  import SizeValue from '../../components/SizeValue.svelte';
  import PageControls from '../../components/PageControls.svelte';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import DateValue from '../../components/DateValue.svelte';
  import RuleEditor from './RuleEditor.svelte';
  import AppliedRules from './AppliedRules.svelte';
  import './comparison.css';
  import type { Rules } from './rules';

  let { id, path }: { id: string; path: string } = $props();
  const connection = useConnection();
  type Page = Result<'GET /api/v1/snapshots/{id}/directory/coverage'>;
  let draft = $state<Rules>({ allow: [], block: [] });
  let applied = $state<Rules>();
  let appliedLimit = $state('');
  let dirty = $state(false);
  let limit = $state('50');
  let error = $state('');
  let owner: ReturnType<typeof coveragePages> | undefined;
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
      owner = coveragePages(connection, id, { path, ...rules, limit: Number(limit) });
      applied = rules;
      appliedLimit = limit;
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

<section class="comparison" aria-label="Content on other disks">
  <h2>Content on other disks</h2>
  <p>
    Selected source content anywhere on each other disk, regardless of destination names or paths.
    The source disk is excluded. Destinations are current inventories only, even for a historical
    source.
  </p>
  <p>
    Distributed coverage is not one complete directory backup. Do not add overlapping per-disk
    counts. Even complete content coverage does not mean an exact tree copy. Coverage is not a
    backup guarantee.
  </p>
  <RuleEditor {draft} {changed} {apply} scope="source" />
  <label class="field comparison-limit"
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
  <AppliedRules {applied} {dirty} limit={appliedLimit} />
  {#if error}<p role="alert">{error}</p>{/if}
  {#if !dirty && applied}
    <RequestFeedback
      status={view.status}
      error={view.error}
      resource="Content coverage"
      {retry}
      loadingLabel="Comparing content coverage…"
    />
    {#if paging.busy}<button onclick={() => changed()}>Cancel comparison</button>{/if}
    {#if view.status === 'success'}<p role="status">
        Comparison complete for loaded destinations.
      </p>{/if}
    {#if view.status === 'idle'}<p>Comparison invalidated; apply to compare again.</p>{/if}
    {#if first}
      {#if view.error}<p>Previously loaded coverage; not freshly verified.</p>{/if}
      <p>
        {first.is_current ? 'Current' : 'Historical'} source inventory compared against current destination
        inventories.
      </p>
      <p>
        Selected source files: {formatCount(first.selection.retained_file_count)}; excluded source
        files: {formatCount(first.selection.excluded_file_count)}.
      </p>
      <p>Selected distinct contents: {formatCount(first.selection.content_count)}.</p>
      <p>
        Source byte total: <SizeValue
          value={first.selection.size_complete ? first.selection.known_bytes : null}
        />; known source byte subtotal: <SizeValue value={first.selection.known_bytes} />;
        unknown-size source files: {formatCount(first.selection.unknown_size_file_count)}. Byte
        sizes {first.selection.size_complete ? 'complete' : 'incomplete'}.
      </p>
      {#if first.selection.empty_comparison}<p>No files selected</p>
      {:else}
        {#if !items.length}<p>No other disks with current inventories.</p>{/if}
        <table class="data-table comparison-results coverage-results">
          <caption
            >Current destination content coverage — counts overlap across disks; do not add them</caption
          >
          <thead
            ><tr
              ><th>Destination disk</th><th>Coverage</th><th class="numeric">Covered</th><th
                class="numeric">Missing</th
              ></tr
            ></thead
          >
          <tbody>
            {#each items as item (item.disk.id)}
              <tr>
                <td data-label="Destination disk">
                  <a href="{resolve('/')}{routes.disk(item.disk.id).slice(1)}"
                    >{item.disk.label} — Disk {item.disk.id}</a
                  >
                  <a href="{resolve('/')}{routes.snapshot(item.snapshot.id).slice(1)}"
                    >Current destination inventory {item.snapshot.id}</a
                  >
                  <DateValue value={item.snapshot.captured_at} meaning="captured_at" />
                </td>
                <td data-label="Coverage">
                  <span
                    class="badge"
                    class:status-success={item.complete}
                    class:status-warning={!item.complete}
                    >{item.complete
                      ? 'Complete content coverage'
                      : 'Partial content coverage'}</span
                  >
                  <p>
                    {percentage(item.covered_content_count, first.selection.content_count)} of selected
                    distinct contents (rounded).
                  </p>
                </td>
                <td data-label="Covered" class="numeric">
                  <p>Files: {formatCount(item.covered_file_count)}</p>
                  <p>Distinct contents: {formatCount(item.covered_content_count)}</p>
                  <p>Known covered source bytes: <SizeValue value={item.covered_known_bytes} /></p>
                  <p>Unknown-size files: {formatCount(item.covered_unknown_size_file_count)}</p>
                </td>
                <td data-label="Missing" class="numeric">
                  <p>Files: {formatCount(item.missing_file_count)}</p>
                  <p>Distinct contents: {formatCount(item.missing_content_count)}</p>
                  <p>Known missing source bytes: <SizeValue value={item.missing_known_bytes} /></p>
                  <p>Unknown-size files: {formatCount(item.missing_unknown_size_file_count)}</p>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
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
  {:else if !dirty}<p>Apply rules to run a comparison. No comparison has been requested.</p>{/if}
</section>
