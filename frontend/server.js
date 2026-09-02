// Serveur de production : sert le build SvelteKit (adapter-node) et proxifie
// vers le backend Go les mêmes préfixes que le proxy Vite du mode dev
// (cf. `server.proxy` dans vite.config.ts, qui n'existe QU'EN dev).
//
// Le proxy doit vivre ici et pas dans un hook SvelteKit : /api porte des
// WebSockets (terminal, creation-stream, debug-logs) et un `handle` n'a pas
// accès à l'upgrade HTTP. Tailscale Serve, en amont, se contente de renvoyer
// tout ${APP_PORT} — donc ce serveur reste aussi la bonne cible pour le port
// de debug publié sur 127.0.0.1:5300.
//
// Variables : PORT, HOST, BACKEND_HOST, BACKEND_PORT.
import { createServer } from 'node:http';
import { createProxyServer } from 'http-proxy-3';
import { handler } from './build/handler.js';

const backendHost = process.env.BACKEND_HOST ?? '127.0.0.1';
const backendPort = process.env.BACKEND_PORT ?? '8080';
const target = `http://${backendHost}:${backendPort}`;

const host = process.env.HOST ?? '0.0.0.0';
const port = Number(process.env.PORT ?? 3000);

// Mêmes préfixes que vite.config.ts — les garder synchronisés.
const PROXIED = ['/api', '/auth', '/health'];

function isProxied(url) {
  const path = (url ?? '/').split('?')[0];
  return PROXIED.some((prefix) => path === prefix || path.startsWith(`${prefix}/`));
}

const proxy = createProxyServer({ target, ws: true, xfwd: true });

proxy.on('error', (err, _req, res) => {
  console.error(`[proxy] ${target} : ${err.message}`);
  // `res` est une réponse HTTP ou, sur une erreur d'upgrade, la socket brute.
  if (res && 'writeHead' in res) {
    if (!res.headersSent) res.writeHead(502, { 'content-type': 'text/plain' });
    res.end('Bad Gateway');
  } else if (res && 'destroy' in res) {
    res.destroy();
  }
});

const server = createServer((req, res) => {
  if (isProxied(req.url)) {
    proxy.web(req, res);
    return;
  }
  handler(req, res);
});

server.on('upgrade', (req, socket, head) => {
  if (isProxied(req.url)) {
    proxy.ws(req, socket, head);
    return;
  }
  socket.destroy();
});

server.listen(port, host, () => {
  console.log(`frontend  → http://${host}:${port}`);
  console.log(`backend   → ${target} (${PROXIED.join(', ')})`);
});
