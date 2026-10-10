<script lang="ts">
  import { presentDate, type DateMeaning } from '../format/date';
  let { value, meaning }: { value: string | null; meaning: DateMeaning } = $props();
  const date = $derived(presentDate(value, meaning));
</script>

<div>
  {date.label}: {#if date.exactUTC === null}Unknown
  {:else}<time datetime={date.exactUTC} title={date.exactUTC}>{date.text}</time>
    <details><summary>Exact UTC</summary><code>{date.exactUTC}</code></details>{/if}
</div>
