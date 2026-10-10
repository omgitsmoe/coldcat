<script lang="ts">
  import { resolve } from '$app/paths';
  import { useConnection } from '../../state/context';
  import { RequestSlot, type RequestState } from '../../state/request';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { contentURL } from '../../state/routes';
  import { algorithms, lookupInput } from './lookup';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  const connection = useConnection();
  const request = new RequestSlot<Result<'GET /api/v1/contents/lookup'>>();
  let view = $state<RequestState<Result<'GET /api/v1/contents/lookup'>>>({ status: 'idle' });
  let algorithm = $state('sha256');
  let digest = $state('');
  let issue = $state('');
  $effect(() => {
    const unsubscribe = request.subscribe((value) => {
      if (value.status === 'failure') connection.report(value.error);
      view = value;
    });
    const unbind = connection.onInvalidate(() => request.cancel());
    return () => {
      unsubscribe();
      unbind();
      request.dispose();
    };
  });
  function edited() {
    issue = '';
    request.cancel();
  }
  function submit() {
    request.cancel();
    issue = '';
    try {
      const input = lookupInput(algorithm, digest);
      void request.run((signal) => connection.client.lookupContent(input, signal));
    } catch (error) {
      issue = error instanceof Error ? error.message : 'Invalid hash identity';
    }
  }
  const missing = $derived(
    view.status === 'failure' &&
      view.error instanceof ApiError &&
      view.error.kind === 'http' &&
      view.error.status === 404,
  );
</script>

<h2>Hash lookup</h2>
<p>
  Look up an explicit algorithm and digest across all complete inventories. Different algorithms do
  not establish content equality.
</p>
<form
  onsubmit={(event) => {
    event.preventDefault();
    submit();
  }}
>
  <label for="hash-algorithm">Algorithm</label><select
    id="hash-algorithm"
    bind:value={algorithm}
    onchange={edited}
  >
    {#each algorithms as item (item)}<option value={item}>{item}</option>{/each}
  </select>
  <label for="hash-digest">Digest (hex)</label><input
    id="hash-digest"
    bind:value={digest}
    oninput={edited}
    autocomplete="off"
    spellcheck="false"
  />
  <button>Look up content</button>
</form>
{#if issue}<p role="alert">{issue}</p>{/if}
{#if missing}<p role="status">No content found for this algorithm and digest.</p>
{:else}<RequestFeedback
    status={view.status}
    error={view.status === 'failure' ? view.error : undefined}
    resource="Content"
    retry={submit}
  />{/if}
{#if view.status === 'success'}<p role="status">
    Known content: {view.value.hash.algorithm} / {view.value.hash.hex}
  </p>
  <a href="{resolve('/')}{contentURL(view.value.id, { scope: 'current' }).slice(1)}"
    >Open content {view.value.id}</a
  >{/if}

<style>
  label {
    display: block;
    margin-block: 0.75rem;
  }
  input {
    display: block;
    width: min(100%, 45rem);
  }
  p {
    overflow-wrap: anywhere;
  }
</style>
