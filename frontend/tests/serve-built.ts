import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { resolve, sep, extname } from 'node:path';

const root = resolve('build');
const types: Record<string, string> = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
};

// Test-only reference host: production dispatch and security tests belong to F10.
const server = createServer(async (request, response) => {
  try {
    if (!request.url) throw new Error('Missing request URL');
    const path = decodeURIComponent(new URL(request.url, 'http://localhost').pathname);
    if (path === '/healthz' || path.startsWith('/api/')) {
      response.writeHead(503).end('No backend in the static smoke harness');
      return;
    }
    const file = resolve(root, `.${path}`);
    if (file !== root && !file.startsWith(root + sep)) {
      response.writeHead(400).end();
      return;
    }
    let body;
    let extension = extname(file);
    try {
      body = await readFile(file);
    } catch (error) {
      if (
        !(error instanceof Error) ||
        !('code' in error) ||
        (error.code !== 'ENOENT' && error.code !== 'EISDIR')
      )
        throw error;
      if (
        path.startsWith('/_app/') ||
        extension ||
        !request.headers.accept?.includes('text/html')
      ) {
        response.writeHead(404).end();
        return;
      }
      body = await readFile(resolve(root, 'index.html'));
      extension = '.html';
    }
    response.setHeader('Content-Type', types[extension] ?? 'application/octet-stream');
    response.end(body);
  } catch {
    response.writeHead(500).end();
  }
});
server.listen(4173, '127.0.0.1');
for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, () => server.close());
}
