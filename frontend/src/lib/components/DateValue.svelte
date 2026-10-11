<script lang="ts">
  import { presentDate, type DateMeaning } from '../format/date';
  let { value, meaning }: { value: string | null; meaning: DateMeaning } = $props();
  const date = $derived(presentDate(value, meaning));
</script>

{#if date.exactUTC === null}
  <div class="date-value">{date.label}: Unknown</div>
{:else}
  <details class="date-value">
    <summary aria-label="{date.label}: {date.text}; exact UTC">
      {date.label}: <time datetime={date.exactUTC}>{date.text}</time>
    </summary>
    <div class="exact">Exact UTC: <code>{date.exactUTC}</code></div>
  </details>
{/if}

<style>
  .date-value {
    font-size: var(--text-small);
    font-variant-numeric: tabular-nums;
  }
  summary {
    cursor: pointer;
  }
  .exact {
    color: var(--color-muted);
    overflow-wrap: anywhere;
  }
</style>
