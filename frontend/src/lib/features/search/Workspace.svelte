<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import { page } from '$app/state';
  import { afterNavigate, beforeNavigate, goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { SvelteURLSearchParams } from 'svelte/reactivity';
  import { useConnection } from '../../state/context';
  import { parseSearch, searchURL, type SearchRoute } from '../../state/routes';
  import { searchKeyAction } from '../../state/keyboard';
  import {
    acquireSearch,
    retainSearch,
    searchIssue,
    SearchWorkspace,
    type SearchPage,
  } from './workspace';
  import {
    advancedCount,
    boundsMode,
    boundsQuery,
    searchSummary,
    type BoundsMode,
  } from './filters';
  import ReplicaBounds from '../../components/ReplicaBounds.svelte';
  import ResourcePicker from '../../components/ResourcePicker.svelte';
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
  let match = $state<NonNullable<SearchRoute['match']>>('substring');
  let scope = $state<'current' | 'history'>('current');
  let metric = $state<'disks' | 'locations'>('disks');
  let disk = $state('');
  let snapshot = $state('');
  let directoryEnabled = $state(false);
  let directory = $state('');
  let exact = $state('');
  let minimum = $state('');
  let maximum = $state('');
  let boundMode = $state<BoundsMode>('any');
  let advancedOpen = $state(false);
  let applied = $state<SearchRoute>();
  const activeAdvanced = $derived(
    [
      disk !== '',
      snapshot !== '',
      directoryEnabled,
      metric === 'locations',
      boundMode !== 'any',
    ].filter(Boolean).length,
  );
  const draftError = $derived.by(() => {
    try {
      const result = searchIssue(routeFromDraft());
      return result?.kind === 'invalid' ? result.message : '';
    } catch (error) {
      return error instanceof Error ? error.message : 'Invalid inputs';
    }
  });
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
    if (workspace.route && searchIssue(workspace.route)?.kind !== 'invalid')
      applied = workspace.route;
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
    boundMode = boundsMode(exact, minimum, maximum);
    advancedOpen = advancedCount(route) > 0;
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
    ]) {
      if (value !== '') params.set(key!, value!);
    }
    for (const [key, value] of Object.entries(
      boundsQuery({
        mode: boundMode,
        exact,
        minimum,
        maximum,
      }),
    ))
      params.set(key, value);
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
      if (draftError) throw new Error(draftError);
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
      boundMode = boundsMode(exact, minimum, maximum);
      const raw = page.url.searchParams;
      field = raw.get('field') === 'path' ? 'path' : 'name';
      const rawMatch = raw.get('match');
      match =
        rawMatch === 'exact' || rawMatch === 'wildcard' || rawMatch === 'expression'
          ? rawMatch
          : 'substring';
      scope = raw.get('scope') === 'history' ? 'history' : 'current';
      metric = raw.get('replica_metric') === 'locations' ? 'locations' : 'disks';
      advancedOpen =
        disk !== '' ||
        snapshot !== '' ||
        directoryEnabled ||
        metric === 'locations' ||
        boundMode !== 'any';
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
<form class="query-toolbar" role="search" onsubmit={submit}>
  <label class="field query-field" for="query"
    >Filename or disk-relative path
    <input
      id="query"
      class="input-search"
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
      aria-describedby="search-syntax-hint"
    /></label
  >
  <button class="button-primary" type="submit">Search</button>
  <button
    class="button-quiet"
    type="button"
    onclick={() => {
      q = '';
      changedQuery();
      input.focus();
    }}>Clear</button
  >
</form>
<p id="search-syntax-hint" class="muted syntax-hint">
  {#if match === 'wildcard'}
    Wildcard matches the whole field. <code>personal*foo</code> matches
    <code>personal/bar/foo</code> in Path; use <code>*personal*foo*</code> for fragments anywhere.
  {:else if match === 'expression'}
    Combine literal substrings: <code>personal AND foo</code> or
    <code>(personal OR work) AND "tax return"</code>. AND binds before OR.
  {:else if match === 'exact'}
    Exact matches the whole field, ignoring case. Short queries and punctuation are literal.
  {:else}
    Literal substring, ignoring case (3+ Unicode characters). More options: Wildcard and AND / OR.
  {/if}
</p>
<form
  class="filter-workspace"
  aria-label="Search filters"
  onsubmit={(event) => {
    event.preventDefault();
    apply();
  }}
  oninput={editedFilter}
  onchange={editedFilter}
>
  <div class="control-group common-filters">
    <label class="field"
      >Field <select bind:value={field}
        ><option value="name">Name</option><option value="path">Path</option></select
      ></label
    >
    <label class="field"
      >Match <select bind:value={match}
        ><option value="substring">Contains</option><option value="exact">Exact</option>
        <option value="wildcard">Wildcard</option><option value="expression">AND / OR</option
        ></select
      ></label
    >
    <label class="field"
      >Scope <select bind:value={scope}
        ><option value="current">Current</option><option value="history">History</option></select
      ></label
    >
    <button class={dirty ? 'button-primary' : 'button-secondary'}>Apply filters</button>
  </div>
  <details class="advanced-filters" bind:open={advancedOpen}>
    <summary>Advanced filters ({activeAdvanced} active)</summary>
    <div class="advanced-groups">
      <fieldset>
        <legend>Membership</legend>
        <div class="control-group">
          <ResourcePicker bind:value={disk} edited={editedFilter} />
          <ResourcePicker
            kind="inventory"
            diskID={disk}
            bind:value={snapshot}
            edited={editedFilter}
          />
          <label class="check-field"
            ><input type="checkbox" bind:checked={directoryEnabled} /> Filter directory membership</label
          >
          <label class="field directory-field"
            >Directory (empty is root)
            <input class="literal" bind:value={directory} disabled={!directoryEnabled} />
          </label>
        </div>
        <p class="muted">
          Snapshot must belong to Disk when both are supplied (validated by the server).
        </p>
        {#if snapshot}<button
            type="button"
            class="button-quiet"
            onclick={() => {
              scope = 'history';
              editedFilter();
              apply();
            }}>Search this older snapshot in history</button
          >{/if}
      </fieldset>
      <fieldset>
        <legend>Redundancy</legend>
        <label class="field"
          >Other-copy metric <select bind:value={metric}>
            <option value="disks">Disks</option><option value="locations">Locations</option>
          </select></label
        >
        <ReplicaBounds
          bind:mode={boundMode}
          bind:exact
          bind:minimum
          bind:maximum
          invalid={!!draftError}
        />
        <p class="muted">
          Other-copy bounds require Current scope. Counts remain catalog-wide despite membership
          filters.
        </p>
      </fieldset>
    </div>
  </details>
  {#if dirty}<p class="status-warning" role="status">
      Unapplied changes — previous results retired. Apply filters to search.
    </p>{/if}
  {#if draftError}<p class="status-error" role="alert">{draftError}</p>{/if}
</form>
{#if applied}<p class="applied-summary">
    <strong>{dirty ? 'Last applied' : 'Applied'}:</strong>
    {searchSummary(applied)}
  </p>{/if}
<div class="result-toolbar">
  <details class="syntax-help">
    <summary>Search syntax help</summary>
    <ul>
      <li>
        All modes ignore case. Name searches the final filename; Path searches the full relative
        path.
      </li>
      <li>Contains and Exact treat spaces, quotes, operators and wildcard characters literally.</li>
      <li>
        Wildcard: <code>*</code> matches any characters (including <code>/</code>),
        <code>?</code> matches one Unicode character. Escape literal characters with
        <code>\*</code>, <code>\?</code> or <code>\\</code>. Brackets are literal. Include at least
        one uninterrupted literal run of 3 characters; at most 32 indexed runs.
      </li>
      <li>
        AND / OR: use explicit uppercase operators and parentheses. Quote literals containing spaces
        or syntax, such as <code>"tax return"</code> or <code>"AND"</code>. Escape quotes and
        backslashes with <code>\"</code> and <code>\\</code>. Each literal needs 3 characters; up to
        32 literals and 16 nesting levels. Wildcards, NOT and regex are not supported in
        expressions.
      </li>
      <li>
        Wildcard and AND / OR results sort by path, then observation ID; Contains boosts exact and
        prefix matches.
      </li>
    </ul>
  </details>
  {#if selected}<div class="actions" aria-live="polite">
      <span>Selected observation {selected}.</span>
      <button class="button-compact" onclick={openSelected}>Open selected content</button>
      <button class="button-compact button-quiet" onclick={() => workspace?.select()}
        >Cancel selection</button
      >
    </div>{/if}
  <details class="keyboard-help">
    <summary>Keyboard help</summary>
    <p>
      / or Ctrl/Cmd+K focuses search outside editable controls. On desktop, typing outside controls
      inserts the character into search. Arrows select results; Enter opens selected content or
      searches immediately. Escape cancels selection without erasing the query. All actions also
      have visible controls.
    </p>
    <label class="check-field"
      ><input
        type="checkbox"
        bind:checked={typeToSearch}
        onchange={() =>
          globalThis.localStorage.setItem('coldcat.typeToSearch', typeToSearch ? 'on' : 'off')}
      /> Type to search on desktop</label
    >
  </details>
</div>
{#if scope === 'history'}<p>
    <strong>History scope — repeated and older inventory observations are included.</strong>
  </p>{/if}
{#if parseError && parseError !== draftError}<p role="alert">{parseError}</p>
  <details>
    <summary>Submitted URL</summary>
    <pre>{page.url.search}</pre>
  </details>
{:else if issue && !dirty && issue !== draftError}<p aria-live="polite">{issue}</p>
  {#if match === 'substring' && Array.from(q).length < 3}<p class="muted">
      Contains requires three Unicode code points; Exact can match a shorter literal query.
    </p>{/if}
{:else if debouncing}<p role="status">Waiting for typing…</p>{/if}
<RequestFeedback
  status={view.status}
  error={view.error}
  resource="Search"
  retained={view.pages.length > 0}
  emptyLabel={items.length === 0
    ? 'No matching observations. Try another query or deliberately choose History scope.'
    : undefined}
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
{#if items.length}
  <Results {items} {selected} {returnTo} select={(id) => workspace?.select(id)} />
{/if}
{#if view.pages.length && traversal}
  <PageControls
    reload={() => workspace?.restart()}
    reloadLabel="Reload from first page"
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
{/if}

<style>
  .syntax-hint {
    margin-block: 0;
  }
  .syntax-help[open] {
    flex-basis: 100%;
  }
  .query-toolbar,
  .result-toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: var(--space-2);
    margin-block: var(--space-3);
  }
  .query-toolbar {
    margin-block-end: var(--space-1);
  }
  .query-field {
    flex: 1;
    min-inline-size: 16rem;
  }
  .query-field input {
    inline-size: 100%;
  }
  .filter-workspace {
    margin-block: var(--space-3);
  }
  .common-filters {
    align-items: end;
  }
  .advanced-filters {
    margin-block-start: var(--space-2);
  }
  .advanced-groups {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
    margin-block-start: var(--space-3);
  }
  fieldset {
    min-inline-size: 0;
  }
  fieldset input:not([type='checkbox']) {
    inline-size: 10rem;
  }
  .directory-field {
    flex: 1 1 100%;
  }
  .directory-field input:not([type='checkbox']) {
    inline-size: 100%;
  }
  .applied-summary {
    color: var(--color-muted);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin-block: var(--space-2);
  }
  .result-toolbar {
    align-items: center;
  }
  .keyboard-help {
    margin-inline-start: auto;
  }
  .keyboard-help[open] {
    flex-basis: 100%;
  }
  @media (max-width: 900px) {
    .advanced-groups {
      grid-template-columns: 1fr;
    }
    .query-field {
      flex-basis: 100%;
    }
  }
</style>
