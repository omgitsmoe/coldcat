<script lang="ts">
  import { page } from '$app/state';
  import ObservationDetail from '../../../lib/features/observations/ObservationDetail.svelte';
</script>

{#key page.params.id + page.url.search}
  <ObservationDetail id={page.params.id!} search={page.url.search} />
{/key}
