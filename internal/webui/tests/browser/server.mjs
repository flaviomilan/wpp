import {createServer} from 'node:http';
import {readFile} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';

// Only public assets are served. APIs are intercepted by Playwright fixtures.
const assets = new URL('../../assets/', import.meta.url);
const types = {html:'text/html',css:'text/css',js:'text/javascript',mjs:'text/javascript'};
const server = createServer(async (request, response) => {
  const name = new URL(request.url, 'http://localhost').pathname.slice(1) || 'index.html';
  if (!/^[a-z][a-z.-]*\.(html|css|m?js)$/.test(name)) {
    response.writeHead(404).end(); return;
  }
  try {
    const body = await readFile(fileURLToPath(new URL(name, assets)));
    response.writeHead(200, {'Content-Type':types[name.split('.').pop()]+'; charset=utf-8'}).end(body);
  } catch { response.writeHead(404).end(); }
});
server.listen(4199, '127.0.0.1');
