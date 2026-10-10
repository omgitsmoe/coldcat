import { ApiError } from '../api/errors';

export function errorPresentation(error: unknown, resource = 'Resource') {
  if (!(error instanceof ApiError)) return { title: 'Unexpected failure', detail: 'Please retry.' };
  if (error.uncertainWrite)
    return {
      title: 'Write outcome unknown',
      detail: 'Reload and reconcile before submitting again.',
    };
  if (error.status === 409 && error.code === 'stale_cursor')
    return {
      title: 'Inventory changed; reload results',
      detail: 'Restart from the first page with the same inputs.',
    };
  if (error.kind === 'transport' || error.status === 503)
    return {
      title: 'Backend unavailable',
      detail: 'Retained data is not freshly verified. Retry the connection.',
    };
  if (error.status === 404) return { title: `${resource} not found`, detail: error.message };
  if (error.status === 400 || error.kind === 'request')
    return {
      title: 'Check submitted parameters',
      detail: error.message,
    };
  if (error.status === 409) return { title: 'Conflict', detail: error.message };
  if (error.kind === 'cancelled') return { title: 'Request cancelled', detail: '' };
  return {
    title: error.kind === 'contract' ? 'Unexpected backend response' : 'Request failed',
    detail: [error.status, error.code].filter((value) => value !== undefined).join(' · '),
  };
}
