import { decimal } from './decimal';

export function literalText(value: string): void {
  if (!value.isWellFormed() || value.includes('\0')) throw new Error('Invalid literal text');
}

export function validatePath(path: string): void {
  literalText(path);
  if (path === '') return;
  if (path.split('/').some((part) => part === '' || part === '.' || part === '..')) {
    throw new Error('Expected disk-relative slash-separated path');
  }
}

export function breadcrumbs(path: string): Array<{ name: string; path: string }> {
  validatePath(path);
  if (path === '') return [];
  const parts = path.split('/');
  return parts.map((name, index) => ({ name, path: parts.slice(0, index + 1).join('/') }));
}

export function containingDirectory(path: string): string {
  validatePath(path);
  return path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';
}

export function directoryURL(snapshotID: string, path: string): string {
  if (decimal(snapshotID) === 0n) {
    throw new Error('Invalid snapshot ID');
  }
  validatePath(path);
  return `/snapshots/${encodeURIComponent(snapshotID)}/directory?${new URLSearchParams({ path })}`;
}
