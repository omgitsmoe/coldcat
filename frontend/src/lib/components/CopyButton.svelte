<script lang="ts">
  import { onDestroy } from 'svelte';
  let { text, label }: { text: string; label: string } = $props();
  let status = $state<'idle' | 'copying' | 'success' | 'failure'>('idle');
  let generation = 0;
  $effect(() => {
    void text;
    generation++;
    status = 'idle';
  });
  onDestroy(() => generation++);
  async function copy() {
    const current = ++generation;
    status = 'copying';
    try {
      await globalThis.navigator.clipboard.writeText(text);
      if (generation === current) status = 'success';
    } catch {
      if (generation === current) status = 'failure';
    }
  }
</script>

<button disabled={status === 'copying'} onclick={copy}>{label}</button>
{#if status === 'success'}<span role="status">Copied.</span>
{:else if status === 'failure'}<span role="alert"
    >Copy failed. Select and copy the text manually.</span
  >{/if}
