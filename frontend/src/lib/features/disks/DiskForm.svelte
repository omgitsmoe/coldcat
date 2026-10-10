<script lang="ts">
  import { onDestroy } from 'svelte';
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { buildDiskPatch } from '../../api/disks';
  import { capacityInput, formatBytes } from '../../format/decimal';
  import { useConnection } from '../../state/context';
  import DiskMetadata from './DiskMetadata.svelte';

  type Disk = Result<'GET /api/v1/disks/{id}'>;
  let { initial, refreshed }: { initial?: Disk; refreshed: (disk: Disk) => void } = $props();
  const connection = useConnection();
  let before = $state<Disk>();
  let label = $state('');
  let capacity = $state('');
  let serial = $state('');
  let notes = $state('');
  let busy = $state(false);
  let blocked = $state(false);
  let error = $state('');
  let message = $state('');
  let verified = $state<Disk>();
  let created = $state(false);
  let cursor = $state<string | null | undefined>();
  let checkedRevision: string | undefined;
  let submittedLabel = '';
  let initialized = false;
  let ownInvalidation = false;
  let disposed = false;
  const controller = new globalThis.AbortController();

  $effect(() => {
    if (initialized) return;
    initialized = true;
    before = initial;
    if (initial) {
      label = initial.label;
      capacity = initial.capacity;
      serial = initial.serial ?? '';
      notes = initial.notes ?? '';
    }
  });
  const preview = $derived.by(() => {
    try {
      return formatBytes(capacityInput(capacity));
    } catch {
      return '';
    }
  });
  const unbind = connection.onInvalidate(() => {
    if (ownInvalidation) return;
    verified = undefined;
    cursor = undefined;
    checkedRevision = undefined;
    if (before || blocked || busy) {
      blocked = true;
      error = 'Catalog context changed. Reconcile before submitting; your inputs are retained.';
    }
  });
  onDestroy(() => {
    disposed = true;
    controller.abort();
    unbind();
  });

  function invalidate() {
    ownInvalidation = true;
    connection.metadataChanged();
    ownInvalidation = false;
  }

  async function loadDisk(id: string) {
    const epoch = connection.epoch;
    const disk = await connection.client.disk(id, controller.signal);
    if (disposed) return;
    if (epoch !== connection.epoch)
      throw new Error('Context changed during reload; reconcile again.');
    if (before) {
      if (label === before.label) label = disk.label;
      if (serial === (before.serial ?? '')) serial = disk.serial ?? '';
      if (notes === (before.notes ?? '')) notes = disk.notes ?? '';
      let capacityChanged: boolean;
      try {
        capacityChanged = capacityInput(capacity) !== before.capacity;
      } catch {
        capacityChanged = true;
      }
      if (!capacityChanged) capacity = disk.capacity;
    }
    before = disk;
    verified = disk;
    invalidate();
    refreshed(disk);
    blocked = false;
    return disk;
  }

  function failure(cause: unknown, writing = false) {
    if (disposed) return;
    connection.report(cause);
    if (writing && cause instanceof ApiError && cause.uncertainWrite) {
      blocked = true;
      error = 'The write may have succeeded. Reconcile before resubmitting; no automatic retry.';
      invalidate();
    } else if (cause instanceof ApiError && cause.status === 409) {
      error = `Conflict: ${cause.message}. Your inputs are retained.`;
    } else error = cause instanceof Error ? cause.message : 'Disk request failed';
  }

  async function submit(event: globalThis.SubmitEvent) {
    event.preventDefault();
    if (busy || blocked || created) return;
    error = '';
    message = '';
    let draft;
    try {
      if (label === '') throw new Error('Label must not be empty');
      draft = {
        label,
        capacity: capacityInput(capacity),
        serial: serial === '' ? null : serial,
        notes: notes === '' ? null : notes,
      };
    } catch (cause) {
      failure(cause);
      return;
    }
    const patch = before ? buildDiskPatch(before, draft) : undefined;
    if (patch && Object.keys(patch).length === 0) {
      message = 'No changes to save.';
      return;
    }
    busy = true;
    submittedLabel = label;
    const epoch = connection.epoch;
    let written = false;
    try {
      const disk = before
        ? await connection.client.updateDisk(before.id, patch!, controller.signal)
        : await connection.client.createDisk(draft, controller.signal);
      if (disposed) return;
      written = true;
      blocked = true;
      if (epoch !== connection.epoch) {
        throw new Error('Catalog context changed during the write. Reconcile before resubmitting.');
      }
      const creating = !before;
      before = disk;
      invalidate();
      await loadDisk(disk.id);
      if (disposed) return;
      created = creating;
      message = creating
        ? 'Disk created and metadata refreshed.'
        : 'Disk updated and metadata refreshed.';
    } catch (cause) {
      failure(cause, !written);
      if (written)
        error = `Write succeeded but metadata is unverified. Reconcile before resubmitting. ${error}`;
    } finally {
      if (!disposed) busy = false;
    }
  }

  async function reconcile(next = false) {
    if (busy) return;
    if (!next) {
      cursor = undefined;
      checkedRevision = undefined;
    }
    busy = true;
    error = '';
    message = '';
    try {
      if (before) {
        await loadDisk(before.id);
        if (!disposed) {
          created = !initial;
          message = 'Review refreshed metadata against your retained inputs before saving.';
        }
      } else {
        const epoch = connection.epoch;
        const page = await connection.client.disks(
          { limit: 50, ...(next && cursor ? { cursor } : {}) },
          controller.signal,
        );
        if (disposed) return;
        if (epoch !== connection.epoch) throw new Error('Context changed; restart reconciliation.');
        if (next && checkedRevision !== page.revision) {
          cursor = undefined;
          throw new Error('Inventory changed; restart reconciliation.');
        }
        checkedRevision = page.revision;
        const match = page.items.find((disk) => disk.label === submittedLabel);
        if (match) {
          cursor = undefined;
          await loadDisk(match.id);
          if (disposed) return;
          created = true;
          message = 'Existing disk found. Review its metadata; no creation was retried.';
          cursor = undefined;
        } else {
          cursor = page.next_cursor;
          message =
            cursor === null
              ? 'No matching label found. A disk might have been renamed; review the disk list before allowing another creation.'
              : 'No matching label on this page. Check the next disk page before resubmitting.';
          invalidate();
        }
      }
    } catch (cause) {
      cursor = undefined;
      checkedRevision = undefined;
      failure(cause);
    } finally {
      if (!disposed) busy = false;
    }
  }
</script>

<section aria-label={initial ? 'Edit disk' : 'Create disk'}>
  <h2>{initial ? 'Edit disk' : 'Create disk'}</h2>
  <form onsubmit={submit}>
    <fieldset disabled={busy || created}>
      <label>Label <input type="text" bind:value={label} required /></label>
      <label
        >Capacity in bytes <input
          type="text"
          inputmode="numeric"
          bind:value={capacity}
          required
        /></label
      >
      <p>Exact decimal bytes, 0–9223372036854775807. Declared capacity is not measured usage.</p>
      {#if preview}<p>Capacity preview: {preview}</p>{/if}
      <label>Serial <input type="text" bind:value={serial} /></label>
      <label>Notes <textarea bind:value={notes}></textarea></label>
      <p>Empty serial or notes clears that field. Other text is preserved exactly.</p>
      <button type="submit" disabled={blocked}>{initial ? 'Save changes' : 'Create disk'}</button>
    </fieldset>
  </form>
  {#if busy}<p role="status">{blocked ? 'Reconciling…' : 'Saving…'}</p>{/if}
  {#if error}<p role="alert">{error}</p>{/if}
  {#if message}<p role="status">{message}</p>{/if}
  {#if blocked && !created}
    <button disabled={busy} onclick={() => void reconcile()}>
      {before ? 'Reconcile disk' : 'Reconcile creation'}
    </button>
    {#if !before && cursor}<button disabled={busy} onclick={() => void reconcile(true)}
        >Check next disk page</button
      >{/if}
    {#if !before && cursor === null}
      <button
        disabled={busy}
        onclick={() => {
          blocked = false;
          cursor = undefined;
          error = '';
          message = 'Resubmission allowed by your explicit review. No write has been retried.';
        }}>I reviewed the disks; allow another creation</button
      >
    {/if}
  {/if}
  {#if verified}
    <h3>Refreshed metadata — compare with your inputs</h3>
    <DiskMetadata item={verified} />
    {#if created}<a href={resolve('/disks/[id]', { id: verified.id })}>Open verified disk</a>{/if}
  {/if}
</section>

<style>
  fieldset {
    display: grid;
    gap: 0.5rem;
    border: 0;
    padding: 0;
  }
  label {
    display: grid;
    gap: 0.2rem;
  }
  input,
  textarea {
    max-width: 40rem;
  }
  button {
    justify-self: start;
  }
</style>
