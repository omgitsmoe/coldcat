<script lang="ts">
  import '../app.css';
  import { resolve } from '$app/paths';
  import { page } from '$app/state';
  import Shell from '../lib/components/Shell.svelte';
  import GlobalShortcut from '../lib/features/search/GlobalShortcut.svelte';
  let { children } = $props();
  const titles: Record<string, string> = {
    '/': 'Search',
    '/contents': 'Distinct contents',
    '/contents/[id]': 'Content',
    '/observations/[id]': 'Observation',
    '/disks': 'Disks',
    '/disks/[id]': 'Disk',
    '/snapshots/[id]': 'Inventory',
    '/snapshots/[id]/directory': 'Directory browser',
  };
  const title = $derived(page.route.id === null ? 'Page unavailable' : titles[page.route.id]);
</script>

<svelte:head
  ><title>{title}{page.params.id ? ` ${page.params.id}` : ''} · Coldcat</title></svelte:head
>

<Shell>
  <GlobalShortcut />
  {#snippet brand()}<a class="brand" href={resolve('/')}>Coldcat</a>{/snippet}
  {#snippet navigation()}
    <nav aria-label="Main navigation">
      <a href={resolve('/')}>Search</a>
      <a href={resolve('/contents')}>Contents</a>
      <a href={resolve('/disks')}>Disks</a>
    </nav>
  {/snippet}
  {@render children()}
</Shell>
