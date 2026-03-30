# Test d'intégration Python (container Docker réel)

**Fichier** : `tests/integration_system.py`

## Objectif

Tester le workflow complet d'un environnement de dev Plati avec un vrai container, vrai SSH, vrai clone de code, vrai serveur applicatif. Docker remplace Incus pour l'exécution.

## Lancement

```bash
make test-integration

# Avec overrides :
REPO_TO_CLONE=/home/user/repos/mon-app \
APP_PORT=8000 \
APP_START_CMD="python -m http.server 8000" \
CONTAINER_USER=ubuntu \
make test-integration
```

Durée : ~60-90 secondes. Nécessite Docker.

## Prérequis

- Docker daemon actif
- Clé SSH `~/.ssh/id_rsa` + `~/.ssh/id_rsa.pub` (lues au runtime, jamais versionnées)
- `pip install bcrypt paramiko requests pyyaml docker`

## Architecture du test

```
┌──────────────────────────────────────────────┐
│ Python unittest (orchestrateur)              │
│                                              │
│  ┌───────────────┐    ┌────────────────────┐ │
│  │ Plati Server   │    │ Docker Ubuntu 24.04│ │
│  │ port 8082      │    │ SSH + Node.js + git│ │
│  │ fresh SQLite   │    │ IP: 172.17.0.x     │ │
│  │ known password │    │ /workspace mount   │ │
│  └───────┬───────┘    └─────────┬──────────┘ │
│          │  API calls            │ SSH / HTTP  │
│          └──────────┬────────────┘             │
│                     │                          │
│              assertions Python                 │
└──────────────────────────────────────────────┘
```

- **PlatiTestServer** : serveur frais sur port 8082, DB SQLite temp, mot de passe admin connu (`integration-test-2024`).
- **DockerTestContainer** : Ubuntu 24.04 avec openssh-server, Node.js 22, git, rsync, curl. Clé SSH injectée dans `authorized_keys`.
- L'instance est injectée directement dans la DB Plati (bypass Incus) pour tester l'API de listing/suppression.

## Tests (14)

| # | Test | Ce qu'il vérifie |
|---|------|-----------------|
| 01 | `health` | API Plati `/health` → `ok` |
| 02 | `admin_login` | Mauvais mdp → 401, bon mdp → 200 + cookie JWT, `/auth/me` → admin |
| 03 | `register_ssh_key` | Clé SSH locale enregistrée via POST `/api/v1/ssh-keys` |
| 04 | `list_templates` | 4 templates chargés : `node-dev`, `python-dev`, `site-ca`, `transcription-catie` |
| 05 | `inject_instance_into_db` | Container Docker inséré dans la DB Plati comme instance "running" |
| 06 | `list_instances_via_api` | GET `/api/v1/instances` retourne l'instance avec la bonne IP |
| 07 | `ssh_connect` | Connexion SSH (paramiko) → `whoami` = `ubuntu` |
| 08 | `node_version` | `node --version` via SSH → `v22.x` |
| 09 | `clone_code` | rsync du repo `site-ca` local → `/workspace/site-ca/` dans le container |
| 10 | `npm_install` | `npm ci` dans le container → exit 0 |
| 11 | `start_dev_server` | `setsid npm run dev` → curl 127.0.0.1:3000 → HTTP 200 |
| 12 | `curl_app` | `requests.get(http://<container_ip>:3000)` → HTTP 200, 63KB HTML |
| 13 | `workspace_ls` | `ls /workspace/site-ca/` → fichiers du repo visibles |
| 14 | `delete_instance_via_api` | DELETE `/api/v1/instances/{id}` → API appelée (HTTP 500 attendu car pas d'Incus réel) |

## Variables d'environnement

| Variable | Défaut | Description |
|----------|--------|-------------|
| `PLATI_TEST_PORT` | `8082` | Port du serveur Plati de test |
| `ADMIN_PASSWORD` | (hardcodé) | Mot de passe admin du serveur de test |
| `REPO_TO_CLONE` | `/home/homaserver2/repos/site-ca` | Repo à copier dans le container |
| `APP_PORT` | `3000` | Port de l'app dans le container |
| `APP_START_CMD` | `npm run dev -- --port 3000 --hostname 0.0.0.0` | Commande de démarrage |
| `CONTAINER_USER` | `ubuntu` | User SSH dans le container |
| `SSH_KEY_PATH` | `~/.ssh/id_rsa` | Clé privée SSH (non versionnée) |

## Nettoyage

Le `tearDownClass` supprime automatiquement :
- La connexion SSH
- Le container Docker (`docker rm -f`)
- Le serveur Plati de test (SIGTERM)
- Les fichiers temporaires (DB, config YAML)
