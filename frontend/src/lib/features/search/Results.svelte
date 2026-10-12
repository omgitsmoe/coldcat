<script lang="ts">
  import type { Result } from '../../api/client';
  import { resolve } from '$app/paths';
  import { contentURL, directoryBrowseURL, routes } from '../../state/routes';
  import { containingDirectory } from '../../format/path';
  import { formatCount } from '../../format/decimal';
  import SizeValue from '../../components/SizeValue.svelte';
  import DateValue from '../../components/DateValue.svelte';
  let {
    items,
    selected,
    returnTo,
    select,
  }: {
    items: Result<'GET /api/v1/search'>['items'];
    selected?: string;
    returnTo: string;
    select: (id: string) => void;
  } = $props();
</script>

<table class="data-table search-results">
  <colgroup
    ><col class="file-column" /><col class="inventory-column" /><col class="size-column" /><col
      class="copies-column"
    /><col class="actions-column" /></colgroup
  >
  <caption
    >Matching observations — counts are catalog-wide, not limited by membership filters</caption
  >
  <thead
    ><tr
      ><th scope="col">File and path</th><th scope="col">Disk / inventory</th><th
        scope="col"
        class="numeric">Size</th
      ><th scope="col" class="numeric">Current copies</th><th scope="col">Actions</th></tr
    ></thead
  >
  <tbody>
    {#each items as item (item.observation.id)}
      {@const context = { returnTo, observation: item.observation.id }}
      <tr class:active={selected === item.observation.id}>
        <td class="file-cell">
          <a
            href="{resolve('/')}{contentURL(item.content.id, { scope: 'current' }, context).slice(
              1,
            )}"
            data-selected-content={selected === item.observation.id ? 'true' : undefined}
            aria-current={selected === item.observation.id ? 'true' : undefined}
            onclick={() => select(item.observation.id)}>{item.basename}</a
          >
          <div class="literal-path literal muted">{item.observation.path}</div>
          <small class="muted">{item.relevance}</small>
        </td>
        <td class="inventory-cell"
          ><span class="record-label">Disk / inventory</span>{item.disk.label}<br />
          <DateValue value={item.snapshot.captured_at} meaning="captured_at" />
          <div>{item.is_current ? 'Current' : 'Historical'} · Inventory {item.snapshot.id}</div>
        </td>
        <td class="numeric size-cell"
          ><span class="record-label">Size</span><SizeValue value={item.content.size} /></td
        >
        <td class="numeric copies-cell"
          ><span class="record-label">Current copies</span>{formatCount(
            item.content.current_disk_count,
          )} current disks<br />
          {formatCount(item.content.current_location_count)} current locations
          {#if item.content.scope === 'history'}<div>
              {formatCount(item.content.observation_count)} scoped historical observations
            </div>{/if}
        </td>
        <td class="row-actions">
          <button
            class="button-compact button-quiet"
            aria-label="Select {item.basename} — {item.observation.path} — observation {item
              .observation.id}"
            aria-pressed={selected === item.observation.id}
            onclick={() => select(item.observation.id)}>Select</button
          >
          <a
            href="{resolve('/')}{routes.observation(item.observation.id, context).slice(1)}"
            onclick={() => select(item.observation.id)}>Observation</a
          >
          <a
            href="{resolve('/')}{directoryBrowseURL(item.snapshot.id, {
              path: containingDirectory(item.observation.path),
            }).slice(1)}"
            onclick={() => select(item.observation.id)}>Containing directory</a
          >
        </td>
      </tr>
    {/each}
  </tbody>
</table>

<style>
  .file-column {
    width: 43%;
  }
  .inventory-column {
    width: 20%;
  }
  .size-column {
    width: 9%;
  }
  .copies-column {
    width: 15%;
  }
  .actions-column {
    width: 13%;
  }
  .active {
    background: var(--color-raised);
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  .file-cell > a {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .row-actions > * {
    display: block;
    margin-block-end: var(--space-1);
  }
  .record-label {
    display: none;
  }
  @media (max-width: 900px) {
    caption {
      display: block;
      inline-size: 100%;
    }
    table,
    tbody {
      display: block;
    }
    colgroup {
      display: none;
    }
    tr {
      display: grid;
      grid-template-columns: 1fr 1fr;
      border-block-end: 1px solid var(--color-border);
      padding-block: var(--space-2);
    }
    .data-table td {
      display: block;
      height: auto;
      border: 0;
      min-inline-size: 0;
    }
    .file-cell,
    .row-actions {
      grid-column: 1 / -1;
    }
    .data-table .numeric {
      text-align: left;
    }
    .record-label {
      display: block;
      color: var(--color-muted);
      font-weight: 600;
    }
    .data-table .row-actions {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-3);
    }
    .row-actions > * {
      margin: 0;
    }
    thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
  }
</style>
