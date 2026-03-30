# Plan Docker pour Plati

## Architecture cible

```
┌─────────────────────────────────┐
│           nginx (:80)           │
│  /api/*, /auth/* → backend:8080│
│  /*             → frontend:3000│
└─────────────────────────────────┘
        │                │
  ┌─────┴─────┐   ┌──────┴──────┐
  │  backend  │   │  frontend   │
  │  Go :8080 │   │  Node :3000 │
  └───────────┘   └─────────────┘
        │
   [volume: data/]  ← plati.db
   [volume: config/] ← plati.yaml + certs
```

## Étapes

### 1. Dockerfile backend (multi-stage)

- Stage build : `golang:1.23` avec CGO activé (requis pour SQLite)
- Stage runtime : `alpine` avec libc compatible
- Copier le binaire `plati-server`
- Exposer port 8080
- Healthcheck : `GET /health`

### 2. Dockerfile frontend

- Changer `adapter-auto` → `@sveltejs/adapter-node`
- Stage build : `node:22` avec `npm run build`
- Stage runtime : `node:22-alpine`
- Exposer port 3000

### 3. docker-compose.yml

Services :
- **nginx** : reverse proxy, route `/api/*` et `/auth/*` vers backend, le reste vers frontend
- **backend** : image Go, volumes pour data + config + certs TLS Incus
- **frontend** : image Node SvelteKit

Volumes :
- `plati-data` : SQLite DB (`/app/data/`)
- `plati-config` : `plati.yaml` + templates (`/app/config/`)
- `plati-certs` : certificats TLS client Incus (`/app/certs/`)

### 4. Adapter la config

- `database.path` → `/app/data/plati.db`
- `templates_dir` → `/app/config/templates`
- `frontend_url` → URL correspondant au nginx (ex: `http://localhost`)
- Chemins TLS → `/app/certs/client.crt`, `/app/certs/client.key`

## Points d'attention

- CGO obligatoire pour `mattn/go-sqlite3` → pas de build statique pur, utiliser `alpine` avec `musl`
- Les fichiers `.db-wal` et `.db-shm` sont créés à côté de la DB, le volume doit couvrir le dossier entier
- CORS : `frontend_url` doit correspondre à l'origine vue par le navigateur
- Entra callback URL doit être mise à jour pour l'URL de production
