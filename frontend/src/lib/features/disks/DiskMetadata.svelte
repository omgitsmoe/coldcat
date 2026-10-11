<script lang="ts">
  import type { Result } from '../../api/client';
  import SizeValue from '../../components/SizeValue.svelte';
  let { item }: { item: Result<'GET /api/v1/disks'>['items'][number] } = $props();
</script>

<dl class="metrics">
  <div class="metric">
    <dt>Label</dt>
    <dd class="literal">{item.label}</dd>
  </div>
  <div class="metric">
    <dt>Declared capacity</dt>
    <dd><SizeValue value={item.capacity} units="decimal" /></dd>
  </div>
  <div class="metric">
    <dt>Serial</dt>
    <dd class="literal">{item.serial ?? 'Unknown'}</dd>
  </div>
</dl>
<details>
  <summary>Disk notes{item.notes === null ? ' · None' : ''}</summary>
  <dl>
    <dt>Notes</dt>
    <dd class="literal">{item.notes ?? 'None'}</dd>
  </dl>
</details>

<style>
  .metric dd.literal {
    font-size: var(--text-small);
    line-height: var(--line-ui);
  }
  .literal {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  dt {
    font-weight: bold;
  }
  dd {
    margin: 0 0 0.5rem;
  }
</style>
