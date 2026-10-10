export function backendTarget(value: string | undefined): string {
  const target = value === undefined ? 'http://127.0.0.1:8080' : value;
  const url = new URL(target);
  if (
    url.protocol !== 'http:' ||
    !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) ||
    url.username ||
    url.password ||
    url.pathname !== '/' ||
    url.search ||
    url.hash
  ) {
    throw new Error('COLDCAT_BACKEND must be an HTTP loopback origin without credentials or path');
  }
  return url.origin;
}

export function proxyRules(target: string) {
  return {
    '^/api/v1(?:/|\\?|$)': { target },
    '^/healthz(?:\\?|$)': { target },
  };
}
