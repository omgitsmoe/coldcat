<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { page } from '$app/state';
  import { afterNavigate, beforeNavigate, goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { SvelteURLSearchParams } from 'svelte/reactivity';
  import { useConnection } from '../../state/context';
  import { parseSearch, searchURL, type SearchRoute } from '../../state/routes';
  import { searchKeyAction } from '../../state/keyboard';
  import { acquireSearch, retainSearch, SearchWorkspace, type SearchPage } from './workspace';
  import type { TraversalState } from '../../state/traversal';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import PageControls from '../../components/PageControls.svelte';
  import Results from './Results.svelte';

  const connection = useConnection();
  let workspace: SearchWorkspace | undefined;
  let unsubscribe: (() => void) | undefined;
  let input: globalThis.HTMLInputElement;
  let mounted = $state(false);
  let q = $state('');
  let field = $state<'name' | 'path'>('name');
  let match = $state<'substring' | 'exact'>('substring');
  let scope = $state<'current' | 'history'>('current');
  let metric = $state<'disks' | 'locations'>('disks');
  let disk = $state('');
  let snapshot = $state('');
  let directoryEnabled = $state(false);
  let directory = $state('');
  let exact = $state('');
  let minimum = $state('');
  let maximum = $state('');
  let dirty = $state(false);
  let composing = $state(false);
  let typeToSearch = $state(true);
  let desktop = $state(false);
  let parseError = $state('');
  let issue = $state('');
  let debouncing = $state(false);
  let view = $state<TraversalState<SearchPage>>({
    status: 'idle',
    pages: [],
    active: 0,
    paged: false,
  });
  let items = $state<SearchPage['items']>([]);
  let selected = $state<string>();
  let lastURL = '';
  let restored = false;
  let restoreScroll: number | undefined;
  let returnTo = $state('/');
  let traversal = $state<SearchWorkspace['owner']>();

  function sync() {
    if (!workspace) return;
    view = workspace.state;
    items = workspace.items;
    selected = workspace.selected;
    issue = workspace.issue?.message ?? '';
    debouncing = workspace.debouncing;
    traversal = workspace.owner;
  }
  function drafts(route: SearchRoute) {
    q = route.q;
    field = route.field ?? 'name';
    match = route.match ?? 'substring';
    scope = route.scope ?? 'current';
    metric = route.replica_metric ?? 'disks';
    disk = route.disk_id ?? '';
    snapshot = route.snapshot_id ?? '';
    directoryEnabled = route.directory !== undefined;
    directory = route.directory ?? '';
    exact = route.other_replicas ?? '';
    minimum = route.min_other_replicas ?? '';
    maximum = route.max_other_replicas ?? '';
    dirty = false;
  }
  function routeFromDraft(): SearchRoute {
    const params = new SvelteURLSearchParams({
      q,
      field,
      match,
      scope,
      replica_metric: metric,
      limit: '50',
    });
    for (const [key, value] of [
      ['disk_id', disk],
      ['snapshot_id', snapshot],
      ['other_replicas', exact],
      ['min_other_replicas', minimum],
      ['max_other_replicas', maximum],
    ]) {
      if (value !== '') params.set(key!, value!);
    }
    if (directoryEnabled) params.set('directory', directory);
    // Invalid literal input remains visible and bookmarkable, but is never dispatched.
    const validation = new SvelteURLSearchParams(params);
    validation.set('q', 'route-validation');
    return { ...parseSearch(validation), q };
  }
  function writeURL(route: SearchRoute, discrete: boolean) {
    const params = new SvelteURLSearchParams(
      searchURL({ ...route, q: 'route-validation' }).split('?')[1],
    );
    params.set('q', q);
    lastURL = `/?${params}`;
    returnTo = lastURL;
    void goto(resolve(`/?${params}`), {
      replace: !discrete,
      reset: false,
      state: page.state,
    });
  }
  function changedQuery() {
    workspace?.retire();
    parseError = '';
    try {
      const route = routeFromDraft();
      writeURL(route, false);
      if (!dirty && !composing) workspace?.update(route);
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid inputs';
    }
  }
  function editedFilter() {
    dirty = true;
    workspace?.retire();
    issue = 'Filters changed. Apply filters to search.';
    parseError = '';
  }
  function apply() {
    try {
      const route = routeFromDraft();
      dirty = false;
      parseError = '';
      writeURL(route, true);
      workspace?.update(route, true);
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid inputs';
    }
  }
  function submit(event: globalThis.SubmitEvent) {
    event.preventDefault();
    if (composing) return;
    if (dirty) apply();
    else if (selected) openSelected();
    else workspace?.flush();
  }
  function openSelected() {
    globalThis.document
      .querySelector<globalThis.HTMLAnchorElement>('[data-selected-content="true"]')
      ?.click();
  }
  function inputKey(event: globalThis.KeyboardEvent) {
    if (
      event.isComposing ||
      composing ||
      event.keyCode === 229 ||
      event.defaultPrevented ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey
    )
      return;
    if (event.key === 'Escape') {
      event.preventDefault();
      workspace?.select();
    }
    if (event.key === 'Enter' && selected) {
      event.preventDefault();
      openSelected();
    }
    if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && items.length) {
      event.preventDefault();
      const current = items.findIndex((item) => item.observation.id === selected);
      const index =
        current < 0
          ? event.key === 'ArrowDown'
            ? 0
            : items.length - 1
          : Math.max(0, Math.min(items.length - 1, current + (event.key === 'ArrowDown' ? 1 : -1)));
      workspace?.select(items[index]!.observation.id);
      void tick().then(() =>
        globalThis.document
          .querySelector('[data-selected-content="true"]')
          ?.scrollIntoView({ block: 'nearest' }),
      );
    }
  }
  function globalKey(event: globalThis.KeyboardEvent) {
    const action = searchKeyAction(event, {
      workspace: true,
      desktop,
      typeToSearch,
      selection: !!globalThis.window.getSelection()?.toString(),
      dialog: !!globalThis.document.querySelector(
        'dialog[open], [aria-modal="true"], [role="dialog"]:not([hidden])',
      ),
    });
    if (!action) return;
    event.preventDefault();
    input.focus();
    if (action === 'type') {
      const start = input.selectionStart ?? q.length;
      const end = input.selectionEnd ?? start;
      q = q.slice(0, start) + event.key + q.slice(end);
      changedQuery();
      void tick().then(() =>
        input.setSelectionRange(start + event.key.length, start + event.key.length),
      );
    }
  }
  function readURL() {
    const url = page.url.pathname + page.url.search;
    if (url === lastURL) return;
    lastURL = url;
    returnTo = url;
    parseError = '';
    try {
      const route = parseSearch(new globalThis.URLSearchParams(page.url.search));
      drafts(route);
      if (!workspace) {
        const acquired = acquireSearch(connection, route);
        workspace = acquired.workspace;
        restored = acquired.restored;
        unsubscribe = workspace.subscribe(sync);
        if (acquired.restored) {
          restoreScroll = workspace.scroll;
        }
      } else workspace.update(route, true);
    } catch (error) {
      workspace?.retire();
      if (!workspace) {
        workspace = new SearchWorkspace(connection);
        unsubscribe = workspace.subscribe(sync);
      }
      q = page.url.searchParams.get('q') ?? '';
      disk = page.url.searchParams.get('disk_id') ?? '';
      snapshot = page.url.searchParams.get('snapshot_id') ?? '';
      directoryEnabled = page.url.searchParams.has('directory');
      directory = page.url.searchParams.get('directory') ?? '';
      exact = page.url.searchParams.get('other_replicas') ?? '';
      minimum = page.url.searchParams.get('min_other_replicas') ?? '';
      maximum = page.url.searchParams.get('max_other_replicas') ?? '';
      parseError = error instanceof Error ? error.message : 'Invalid search URL';
    }
  }
  $effect(() => {
    if (mounted) {
      void page.url;
      readURL();
    }
  });
  beforeNavigate(() => {
    if (workspace) workspace.scroll = globalThis.window.scrollY;
  });
  afterNavigate(() => {
    if (restoreScroll === undefined) return;
    globalThis.window.scrollTo(0, restoreScroll);
    restoreScroll = undefined;
    if (
      selected &&
      (globalThis.document.activeElement === globalThis.document.body ||
        globalThis.document.activeElement?.id === 'main')
    )
      input.focus({ preventScroll: true });
  });
  onMount(() => {
    const media = globalThis.window.matchMedia('(min-width: 900px) and (pointer: fine)');
    desktop = media.matches;
    const change = () => {
      desktop = media.matches;
    };
    media.addEventListener('change', change);
    typeToSearch = globalThis.localStorage.getItem('coldcat.typeToSearch') !== 'off';
    readURL();
    mounted = true;
    if (
      desktop &&
      !restored &&
      (globalThis.document.activeElement === globalThis.document.body ||
        globalThis.document.activeElement?.id === 'main')
    )
      input.focus({ preventScroll: true });
    return () => media.removeEventListener('change', change);
  });
  onDestroy(() => {
    unsubscribe?.();
    if (workspace) retainSearch(workspace);
  });
</script>

<svelte:window onkeydown={globalKey} />
<h1>Search</h1>
<form role="search" onsubmit={submit}>
  <label for="query">Filename or disk-relative path</label>
  <input
    id="query"
    type="search"
    bind:this={input}
    bind:value={q}
    oninput={changedQuery}
    onkeydown={inputKey}
    oncompositionstart={() => {
      composing = true;
      workspace?.retire();
    }}
    oncompositionend={() => {
      composing = false;
      changedQuery();
    }}
    autocomplete="off"
  />
  <button type="submit">Search</button>
  <button
    type="button"
    onclick={() => {
      q = '';
      changedQuery();
      input.focus();
    }}>Clear</button
  >
</form>
<form
  aria-label="Search filters"
  onsubmit={(event) => {
    event.preventDefault();
    apply();
  }}
  oninput={editedFilter}
  onchange={editedFilter}
>
  <div class="filters">
    <label
      >Field <select bind:value={field}
        ><option value="name">Name</option><option value="path">Path</option></select
      ></label
    >
    <label
      >Match <select bind:value={match}
        ><option value="substring">Substring</option><option value="exact">Exact</option></select
      ></label
    >
    <label
      >Scope <select bind:value={scope}
        ><option value="current">Current</option><option value="history">History</option></select
      ></label
    >
    <label
      >Other-copy metric <select bind:value={metric}
        ><option value="disks">Disks</option><option value="locations">Locations</option></select
      ></label
    >
    <label>Disk ID <input bind:value={disk} inputmode="numeric" /></label>
    <label>Snapshot ID <input bind:value={snapshot} inputmode="numeric" /></label>
    <label
      ><input type="checkbox" bind:checked={directoryEnabled} /> Filter directory membership</label
    >
    <label
      >Directory (empty is root) <input
        bind:value={directory}
        disabled={!directoryEnabled}
      /></label
    >
    <label>Exact other copies <input bind:value={exact} inputmode="numeric" /></label>
    <label>Minimum other copies <input bind:value={minimum} inputmode="numeric" /></label>
    <label>Maximum other copies <input bind:value={maximum} inputmode="numeric" /></label>
  </div>
  <p>
    Other-copy bounds require Current scope. Exact and range bounds are mutually exclusive. Snapshot
    must belong to Disk when both are supplied (validated by the server).
  </p>
  <button>Apply filters</button>
  <button
    type="button"
    onclick={() => {
      scope = 'history';
      editedFilter();
      apply();
    }}>Search history</button
  >
  {#if snapshot}<button
      type="button"
      onclick={() => {
        scope = 'history';
        editedFilter();
        apply();
      }}>Search this older snapshot in history</button
    >{/if}
</form>
<details>
  <summary>Keyboard help</summary>
  <p>
    / or Ctrl/Cmd+K focuses search outside editable controls. On desktop, typing outside controls
    inserts the character into search. Arrows select results; Enter opens selected content or
    searches immediately. Escape cancels selection without erasing the query. All actions also have
    visible controls.
  </p>
  <label
    ><input
      type="checkbox"
      bind:checked={typeToSearch}
      onchange={() =>
        globalThis.localStorage.setItem('coldcat.typeToSearch', typeToSearch ? 'on' : 'off')}
    /> Type to search on desktop</label
  >
</details>
{#if scope === 'history'}<p>
    <strong>History scope — repeated and older inventory observations are included.</strong>
  </p>{/if}
{#if parseError}<p role="alert">{parseError}</p>
  <details>
    <summary>Submitted URL</summary>
    <pre>{page.url.search}</pre>
  </details>
{:else if issue}<p aria-live="polite">{issue}</p>
{:else if debouncing}<p role="status">Waiting for typing…</p>{/if}
<RequestFeedback
  status={view.status}
  error={view.error}
  resource="Search"
  loadingLabel={view.status === 'loading-more' ? 'Loading another page…' : 'Searching…'}
  retry={() => {
    if (view.pages.length) {
      if (view.paged || view.pages.length >= 10) void traversal?.traversal.next();
      else void traversal?.traversal.more();
    } else workspace?.restart();
  }}
/>
{#if !dirty && !parseError && !issue && !debouncing && view.status === 'idle'}
  <p>
    Search data was invalidated. <button onclick={() => workspace?.restart()}>Reload results</button
    >
  </p>
{/if}
{#if !dirty && !parseError && view.status === 'success' && items.length === 0}
  <p role="status">
    No matching observations. Try another literal query or deliberately choose Search history.
  </p>
{/if}
{#if items.length}
  {#if view.paged}<p>{items.length} observations displayed on this page.</p>{/if}
  {#if selected}<p aria-live="polite">
      Selected observation {selected}.
      <button onclick={openSelected}>Open selected content</button><button
        onclick={() => workspace?.select()}>Cancel selection</button
      >
    </p>{/if}
  <Results {items} {selected} {returnTo} select={(id) => workspace?.select(id)} />
{/if}
{#if view.pages.length && traversal}
  <PageControls
    busy={traversal.traversal.busy}
    canMore={traversal.traversal.canMore}
    canNext={traversal.traversal.canNext}
    canPrevious={traversal.traversal.canPrevious}
    paged={view.paged}
    page={view.active}
    firstRetained={view.pages[0]!.number}
    loaded={items.length}
    complete={view.pages.at(-1)!.value.next_cursor === null}
    more={() => {
      workspace?.select();
      void traversal?.traversal.more();
    }}
    next={() => {
      workspace?.select();
      void traversal?.traversal.next();
    }}
    previous={() => {
      workspace?.select();
      traversal?.traversal.previous();
    }}
  />
  <button onclick={() => workspace?.restart()}>Reload from first page</button>
{/if}

<style>
  form {
    margin-block: 1rem;
  }
  #query {
    width: min(100%, 42rem);
    display: block;
    margin-block: 0.5rem;
  }
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
  }
  .filters label {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  button {
    margin: 0.25rem;
  }
</style>
