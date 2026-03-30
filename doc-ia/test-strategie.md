# Stratégie de tests Plati

## Vue d'ensemble

```
                     Rapidité ←────────────→ Réalisme
                         │                      │
  Go mock integration    │   Python/Docker      │   Selenium E2E
  (1s, sans dépendance)  │   (60s, Docker)      │   (Chrome + serveurs)
          ↓               │        ↓              │        ↓
  workflow_test.go        │  integration_system   │  selenium_tests
                         │                      │
  Business logic         │  Infra réelle        │  UI complète
  API HTTP complet       │  SSH, clone, curl    │  Navigateur
  Cloud-init correct     │  npm install/run     │  Login, nav, CRUD
  Volume lifecycle       │  Next.js dev server  │  Template cards
```

## 3 niveaux de tests

### 1. Tests Go (mock Incus) — `make test-backend`

- **Cible** : logique métier, API handlers, auth, DB
- **Durée** : ~1s
- **Dépendances** : aucune (SQLite in-memory + mock)
- **Quand les lancer** : à chaque commit, en CI

### 2. Tests Python Docker — `make test-integration`

- **Cible** : workflow complet avec vrai container (SSH, git, npm, HTTP)
- **Durée** : ~60-90s
- **Dépendances** : Docker, clé SSH locale, repo source
- **Quand les lancer** : avant merge, validation manuelle, CI avec Docker

### 3. Tests Selenium — `make test-e2e`

- **Cible** : interface utilisateur bout-en-bout
- **Durée** : ~30s
- **Dépendances** : Chrome/Chromium, serveurs Plati + frontend lancés
- **Quand les lancer** : avant release, changements frontend

## Matrice de couverture

| Fonctionnalité | Go mock | Python Docker | Selenium |
|----------------|:-------:|:-------------:|:--------:|
| Auth (login/logout) | x | x | x |
| SSH key CRUD | x | x | x |
| Template import/list | x | x | x |
| Instance create | x | | |
| Instance start/stop | x | | |
| Instance rebuild | x | | |
| Instance delete | x | x | |
| SSH key → cloud-init | x | | |
| Volume lifecycle | x | | |
| Vrai SSH dans container | | x | |
| Vrai git clone / rsync | | x | |
| Vrai npm install | | x | |
| Vrai serveur de dev | | x | |
| Vrai curl HTTP app | | x | |
| Navigation UI | | | x |
| Login page render | | | x |
| Admin tabs | | | x |

## Commandes

```bash
# Tous les tests rapides
make test

# Tests d'intégration avec Docker (workflow complet)
make test-integration

# Tests E2E navigateur
make test-e2e

# Tout
make test && make test-integration && make test-e2e
```
