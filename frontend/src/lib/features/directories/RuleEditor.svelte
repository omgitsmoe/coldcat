<script lang="ts">
  import { ruleInput, type Rules } from './rules';
  let {
    draft,
    changed,
    apply,
  }: {
    draft: Rules;
    changed: (rules: Rules) => void;
    apply: (rules: Rules) => void;
  } = $props();
  const error = $derived.by(() => {
    try {
      ruleInput(draft);
      return '';
    } catch (error) {
      return error instanceof Error ? error.message : 'Invalid rules';
    }
  });
  function update(kind: keyof Rules, index: number, value: string) {
    changed({ ...draft, [kind]: draft[kind].map((row, i) => (i === index ? value : row)) });
  }
</script>

<form
  onsubmit={(event) => {
    event.preventDefault();
    if (!error) apply(ruleInput(draft));
  }}
>
  <h3>Allow / block rules</h3>
  <p>
    One literal pattern per row; whitespace and backslash escaping are significant. Rules match
    relative file paths, case-sensitively, on both trees. Block wins over allow. No allow rules
    selects all files before blocking. The backend interprets doublestar globs.
  </p>
  <p>
    Examples: <code>**/*.jpg</code>, <code>{'{photos,video}/**'}</code>,
    <code>file\?.txt</code> (literal question mark). Maximum 100 rules combined, 1–1024 UTF-8 bytes per
    row and 16384 UTF-8 bytes combined; no NUL.
  </p>
  {#each ['allow', 'block'] as kind (kind)}
    {@const key = kind as keyof Rules}
    {@const label = key === 'allow' ? 'Allow' : 'Block'}
    <fieldset>
      <legend>{label} rules</legend>
      {#each draft[key] as row, index (index)}
        <div>
          <label
            >{label} pattern {index + 1}
            <input value={row} oninput={(event) => update(key, index, event.currentTarget.value)} />
          </label>
          <button
            type="button"
            aria-label={`Remove ${key} rule ${index + 1}`}
            onclick={() => changed({ ...draft, [key]: draft[key].filter((_, i) => i !== index) })}
            >Remove</button
          >
        </div>
      {/each}
      <button type="button" onclick={() => changed({ ...draft, [key]: [...draft[key], ''] })}>
        Add {key} rule
      </button>
    </fieldset>
  {/each}
  {#if error}<p role="alert">{error}</p>{/if}
  <button disabled={!!error}>Apply and compare</button>
</form>
