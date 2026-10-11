<script lang="ts">
  import { readableBytes, type ByteUnits } from '../format/decimal';
  let { value, units = 'binary' }: { value: string | null; units?: ByteUnits } = $props();
  const size = $derived(readableBytes(value, units));
</script>

{#if size.scaled}
  <details class="size-value">
    <summary aria-label="{size.approximate ? 'Approximately ' : ''}{size.text}; exact bytes">
      {#if size.approximate}<span aria-hidden="true">≈&nbsp;</span>{/if}{size.text}
    </summary>
    <span class="exact">Exact bytes: <code>{size.exactBytes}</code></span>
  </details>
{:else}<span class="size-value">{size.text}</span>{/if}

<style>
  .size-value {
    display: inline-block;
    max-width: 100%;
    vertical-align: top;
    font-variant-numeric: tabular-nums;
  }
  summary {
    cursor: pointer;
  }
  .exact {
    display: block;
    color: var(--color-muted);
    overflow-wrap: anywhere;
  }
</style>
