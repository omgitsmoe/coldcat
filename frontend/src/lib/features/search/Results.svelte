<script lang="ts">
  import type { SearchPage } from './workspace';
  import { resolve } from '$app/paths';
  import { contentURL, directoryBrowseURL, routes } from '../../state/routes';
  import { containingDirectory } from '../../format/path';
  import { formatBytes, formatCount } from '../../format/decimal';
  import { presentDate } from '../../format/date';
  let {
    items,
    selected,
    returnTo,
    select,
  }: {
    items: SearchPage['items'];
    selected?: string;
    returnTo: string;
    select: (id: string) => void;
  } = $props();
</script>

<table>
  <caption
    >Matching observations — counts are catalog-wide, not limited by membership filters</caption
  >
  <thead
    ><tr
      ><th>File and path</th><th>Disk / inventory</th><th>Size</th><th>Current copies</th><th
        >Actions</th
      ></tr
    ></thead
  >
  <tbody>
    {#each items as item (item.observation.id)}
      {@const context = { returnTo, observation: item.observation.id }}
      {@const captured = presentDate(item.snapshot.captured_at, 'captured_at')}
      <tr class:active={selected === item.observation.id}>
        <td>
          <a
            href="{resolve('/')}{contentURL(item.content.id, { scope: 'current' }, context).slice(
              1,
            )}"
            data-selected-content={selected === item.observation.id ? 'true' : undefined}
            aria-current={selected === item.observation.id ? 'true' : undefined}
            onclick={() => select(item.observation.id)}>{item.basename}</a
          >
          <div class="literal-path">{item.observation.path}</div>
          <small>{item.relevance}</small>
        </td>
        <td
          >{item.disk.label}<br />
          <time datetime={captured.exactUTC!} title={captured.exactUTC!}
            >Captured at {captured.text}</time
          >
          <div>{item.is_current ? 'Current' : 'Historical'} · Inventory {item.snapshot.id}</div>
        </td>
        <td>{formatBytes(item.content.size)}</td>
        <td
          >{formatCount(item.content.current_disk_count)} current disks<br />
          {formatCount(item.content.current_location_count)} current locations
          {#if item.content.scope === 'history'}<div>
              {formatCount(item.content.observation_count)} scoped historical observations
            </div>{/if}
        </td>
        <td>
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
          <button
            aria-pressed={selected === item.observation.id}
            onclick={() => select(item.observation.id)}>Select {item.basename}</button
          >
        </td>
      </tr>
    {/each}
  </tbody>
</table>

<style>
  table {
    width: 100%;
    border-collapse: collapse;
    margin-block: 1rem;
  }
  caption {
    text-align: left;
    margin-block: 0.5rem;
  }
  th,
  td {
    text-align: left;
    vertical-align: top;
    padding: 0.5rem;
    border-bottom: 1px solid #777;
  }
  .active {
    outline: 2px solid currentColor;
    outline-offset: -2px;
  }
  .literal-path {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  td a,
  td button {
    display: inline-block;
    margin: 0.2rem;
  }
  @media (max-width: 700px) {
    table,
    tbody,
    tr,
    td {
      display: block;
    }
    thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
    tr {
      border: 1px solid #777;
      margin-block: 1rem;
    }
    td {
      border: 0;
    }
  }
</style>
