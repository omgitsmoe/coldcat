<script lang="ts">
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { useConnection } from '../../state/context';
  import { directoryDetail, directoryPages } from '../../state/feature-requests';
  import {
    parseDirectory,
    directoryBrowseURL,
    routes,
    type DirectoryRoute,
  } from '../../state/routes';
  import type { RequestState } from '../../state/request';
  import type { TraversalState } from '../../state/traversal';
  import { breadcrumbs } from '../../format/path';
  import { formatCount } from '../../format/decimal';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import PageControls from '../../components/PageControls.svelte';
  import DateValue from '../../components/DateValue.svelte';
  import Sizes from './Sizes.svelte';
  import Entries from './Entries.svelte';
  import Filters from './Filters.svelte';
  import Replicas from './Replicas.svelte';
  import Coverage from './Coverage.svelte';
  import { directorySummary } from './filters';
  let { id, search }: { id: string; search: string } = $props();
  const connection = useConnection();
  type Detail = Result<'GET /api/v1/snapshots/{id}/directory'>;
  type Page = Result<'GET /api/v1/snapshots/{id}/directory/entries'>;
  let query = $state<DirectoryRoute>();
  let parseError = $state('');
  let detailOwner = $state<ReturnType<typeof directoryDetail>>();
  let pagesOwner = $state<ReturnType<typeof directoryPages>>();
  let detail = $state<RequestState<Detail>>({ status: 'idle' });
  let view = $state<TraversalState<Page>>({ status: 'idle', pages: [], active: 0, paged: false });
  let items = $state<Page['items']>([]);
  let draft = $state(false);
  let tab = $state<'entries' | 'replicas' | 'coverage'>('entries');
  let paging = $state({ busy: false, canMore: false, canNext: false, canPrevious: false });
  const source = $derived(detail.status === 'success' ? detail.value : view.pages[0]?.value);
  $effect(() => {
    let input: DirectoryRoute;
    try {
      input = parseDirectory(new globalThis.URLSearchParams(search));
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid directory route';
      return;
    }
    query = input;
    const summary = directoryDetail(connection, id, input.path);
    const pages = directoryPages(connection, id, input);
    detailOwner = summary;
    pagesOwner = pages;
    const unDetail = summary.request.subscribe((state) => {
      detail = state;
    });
    const unPages = pages.traversal.subscribe((state) => {
      view = state;
      items = pages.traversal.items;
      paging = {
        busy: pages.traversal.busy,
        canMore: pages.traversal.canMore,
        canNext: pages.traversal.canNext,
        canPrevious: pages.traversal.canPrevious,
      };
    });
    void summary.reload();
    void pages.traversal.restart();
    return () => {
      unDetail();
      unPages();
      summary.dispose();
      pages.dispose();
    };
  });
  function retire() {
    draft = true;
    pagesOwner?.traversal.retire();
  }
  function resume() {
    draft = false;
    void pagesOwner?.traversal.restart();
  }
  function retry() {
    const traversal = pagesOwner?.traversal;
    if (!traversal) return;
    const disconnected =
      view.error instanceof ApiError &&
      (view.error.kind === 'transport' || view.error.status === 503);
    if (!view.pages.length || view.status === 'stale' || disconnected) void traversal.restart();
    else if (view.paged || view.pages.length >= 10) void traversal.next();
    else void traversal.more();
  }
</script>

<h1>Directory browser</h1>
{#if parseError}<p role="alert">{parseError}</p>
  <pre>{search}</pre>
{:else if query}
  <div class="source-context">
    <a href="{resolve('/')}{routes.snapshot(id).slice(1)}">Inventory {id}</a>
    {#if source}
      <span class="badge"
        >{source.is_current ? 'Current source inventory' : 'Historical source inventory'}</span
      >
      <a href="{resolve('/')}{routes.disk(source.snapshot.disk_id).slice(1)}"
        >Disk {source.snapshot.disk_id}</a
      >
      <DateValue value={source.snapshot.captured_at} meaning="captured_at" />
    {/if}
  </div>
  <nav class="breadcrumbs" aria-label="Directory breadcrumbs">
    <a href="{resolve('/')}{directoryBrowseURL(id, { ...query, path: '' }).slice(1)}"
      >Root{source ? ` — Disk ${source.snapshot.disk_id}` : ` — Inventory ${id}`}</a
    >
    {#each breadcrumbs(query.path!) as crumb (crumb.path)}
      <span> / </span>
      {#if crumb.path === query.path}<span class="literal" aria-current="page">{crumb.name}</span>
      {:else}<a
          class="literal"
          href="{resolve('/')}{directoryBrowseURL(id, { ...query, path: crumb.path }).slice(1)}"
          >{crumb.name}</a
        >{/if}
    {/each}
  </nav>
  <section class="summary panel" aria-label="Directory summary">
    <RequestFeedback
      status={detail.status}
      error={detail.status === 'failure' ? detail.error : undefined}
      resource="Directory summary"
      retry={() => void detailOwner?.reload()}
    />
    {#if detail.status !== 'failure' && detail.status !== 'success'}<button
        class="button-quiet button-compact summary-reload"
        disabled={detail.status === 'loading'}
        onclick={() => void detailOwner?.reload()}>Reload summary</button
      >{/if}
    {#if detail.status === 'idle'}<p>
        Summary invalidated; reload to verify current redundancy.
      </p>{/if}
    {#if detail.status === 'success'}
      <Sizes item={detail.value.directory} />
      <div class="summary-footer">
        <details class="statistics">
          <summary>Detailed statistics</summary>
          <p class="muted">
            Cataloged logical bytes, not filesystem allocation or free space. Sizes include all
            descendant files, independent of entry filters.
          </p>
          <DateValue value={detail.value.directory.max_known_mtime} meaning="mtime" />
          <section aria-label="Current other-disk redundancy">
            <h3>Current other-disk redundancy</h3>
            <p>
              All descendant file occurrences, against current destination inventories only; the
              source disk is excluded. Historical snapshots do not add replicas.
            </p>
            <ul>
              {#each detail.value.redundancy_histogram as bucket (bucket.other_disk_count)}<li>
                  {formatCount(bucket.other_disk_count)} other disks: {formatCount(
                    bucket.file_count,
                  )} files
                </li>{/each}
            </ul>
            {#if !detail.value.redundancy_histogram.length}<p>No descendant files.</p>{/if}
          </section>
        </details>
        <button class="button-quiet button-compact" onclick={() => void detailOwner?.reload()}
          >Reload summary</button
        >
      </div>
    {/if}
  </section>
  <nav class="directory-views control-group" aria-label="Directory views">
    <button aria-pressed={tab === 'entries'} onclick={() => (tab = 'entries')}>Entries</button>
    <button aria-pressed={tab === 'replicas'} onclick={() => (tab = 'replicas')}
      >Exact tree copies</button
    >
    <button aria-pressed={tab === 'coverage'} onclick={() => (tab = 'coverage')}
      >Content on other disks</button
    >
  </nav>
  {#if tab === 'replicas'}
    {#key `${id}:${query.path}`}<Replicas {id} path={query.path!} />{/key}
  {:else if tab === 'coverage'}
    {#key `${id}:${query.path}`}<Coverage {id} path={query.path!} />{/key}
  {:else}
    <section aria-label="Directory entry listing">
      <h2>Entries</h2>
      <Filters {id} {query} {retire} {resume} />
      <p class="muted">{draft ? 'Last applied' : 'Applied'}: {directorySummary(query)}</p>
      <p class="muted">
        Replicas use current destination inventories only, even for a historical source; the source
        disk is excluded. Immediate child directories stay navigable under file replica filters.
      </p>
      {#if draft}<p class="status-warning" role="status">
          Unapplied changes — filters changed; apply to load entries.
        </p>
      {:else}
        <RequestFeedback
          status={view.status}
          error={view.error}
          resource="Directory entries"
          {retry}
          retained={view.pages.length > 0}
          retainedLabel="Previously loaded entries; not freshly verified."
          invalidatedLabel="Entries invalidated; reload results."
          emptyLabel={!items.length
            ? 'No entries match this listing. This does not imply an empty recursive summary.'
            : undefined}
        />
        {#if view.status === 'idle' || view.status === 'loading'}<button
            disabled={paging.busy}
            onclick={() => void pagesOwner?.traversal.restart()}>Reload entries</button
          >{/if}
        {#if view.pages.length}
          {#if items.length}<Entries
              {items}
              {id}
              {query}
              returnTo={directoryBrowseURL(id, query)}
            />
          {/if}
          <PageControls
            reload={() => void pagesOwner?.traversal.restart()}
            reloadLabel={view.status === 'failure' ? 'Reload from first page' : 'Reload entries'}
            {...paging}
            paged={view.paged}
            page={view.active}
            firstRetained={view.pages[0]!.number}
            loaded={items.length}
            complete={view.pages.at(-1)!.value.next_cursor === null}
            more={() => void pagesOwner?.traversal.more()}
            next={() => void pagesOwner?.traversal.next()}
            previous={() => pagesOwner?.traversal.previous()}
          />
        {/if}
      {/if}
    </section>
  {/if}
{/if}

<style>
  .literal {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .source-context {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-2);
  }
  .breadcrumbs {
    margin-block: var(--space-3);
    overflow-wrap: anywhere;
  }
  .summary-footer {
    display: flex;
    align-items: start;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }
  .summary-footer details {
    flex: 1;
    min-width: 0;
  }
  .statistics {
    margin-top: var(--space-2);
  }
  .directory-views {
    margin-block: var(--space-4);
  }
  .directory-views button {
    border-color: transparent;
  }
  .directory-views button[aria-pressed='true'] {
    border-color: var(--color-accent);
    text-decoration: underline;
  }
</style>
