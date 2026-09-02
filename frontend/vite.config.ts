import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const backendUrl = `http://${process.env.BACKEND_HOST ?? 'localhost'}:8080`;

// Port que le client HMR doit viser. Le client Vite construit son URL WebSocket
// en `hostname:${hmrPort || port de la page}` : derrière Tailscale Serve la page
// est en https sans port explicite, donc sans ce réglage l'URL serait
// « wss://<hôte>: », invalide. On force donc 443 (cf. VITE_HMR_CLIENT_PORT dans
// docker-compose.dev.yml). Non défini = comportement Vite par défaut, correct
// pour un accès direct au serveur de dev.
const hmrClientPort = process.env.VITE_HMR_CLIENT_PORT;

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    allowedHosts: ['plati.burro-piranha.ts.net'],
    // Protocole et hôte laissés libres : le client les déduit de la page, donc
    // wss + le nom tailnet quand on passe par Serve.
    hmr: hmrClientPort ? { clientPort: Number(hmrClientPort) } : undefined,
    // Mode dev uniquement — Vite ne lit `server.proxy` que sous `vite dev`.
    // Le build de prod est servi par server.js, qui refait ce routage (et gère
    // les WebSockets) : garder les deux listes de préfixes synchronisées.
    proxy: {
      '/api': {
        target: backendUrl,
        ws: true
      },
      '/auth': backendUrl,
      '/health': backendUrl
    }
  }
});
