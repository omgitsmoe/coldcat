export interface SearchKeyOptions {
  workspace: boolean;
  desktop: boolean;
  typeToSearch: boolean;
  selection: boolean;
  dialog: boolean;
}

export function searchKeyAction(
  event: KeyboardEvent,
  options: SearchKeyOptions,
): 'focus' | 'type' | null {
  if (
    event.defaultPrevented ||
    event.isComposing ||
    event.keyCode === 229 ||
    options.dialog ||
    options.selection ||
    event.altKey ||
    ['Dead', 'Process', 'Unidentified'].includes(event.key)
  )
    return null;
  const target = event.composedPath()[0] ?? event.target;
  if (
    target instanceof Element &&
    target.closest(
      'input, textarea, select, button, a, [contenteditable]:not([contenteditable="false"]), ' +
        '[role="textbox"], [role="combobox"], [role="slider"], [role="spinbutton"], ' +
        '[role="button"], [role="menuitem"], [role="option"], [role="treeitem"], ' +
        '[role="listbox"], [role="grid"]',
    )
  )
    return null;
  if (event.key.toLowerCase() === 'k' && event.ctrlKey !== event.metaKey && !event.shiftKey) {
    return 'focus';
  }
  if (event.ctrlKey || event.metaKey) return null;
  if (event.key === '/' && !event.shiftKey) return 'focus';
  if (
    options.workspace &&
    options.desktop &&
    options.typeToSearch &&
    Array.from(event.key).length === 1
  )
    return 'type';
  return null;
}
