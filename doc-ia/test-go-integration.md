# Tests d'intégration Go (mock Incus)

**Fichier** : `backend/internal/integration/workflow_test.go`

## Objectif

Tester l'API Plati de bout en bout sans dépendance Incus. Un mock `IncusClient` intercepte tous les appels et vérifie le contenu (cloud-init, volumes, lifecycle).

## Lancement

```bash
cd backend && go test ./internal/integration/... -v
```

Durée : ~1 seconde. Aucune dépendance externe.

## Architecture du test

```
httptest.Server (router complet)
       ↓
  real SQLite (:memory:)
  real JWT auth (bcrypt + cookie)
  real handlers / services
       ↓
  mockIncusClient (enregistre les appels)
```

- **Base de données** : SQLite en mémoire, migrations appliquées, admin user seedé.
- **Authentification** : vrai bcrypt hash + vrai JWT signé dans un cookie (via `cookiejar`).
- **Incus** : mock injecté dans le Pool via `pool.SetClient("test-server", mock)`.

## Tests (7)

| Test | Ce qu'il vérifie |
|------|-----------------|
| `TestHealthCheck` | `/health` → `{"status":"ok","database":"ok"}` |
| `TestAdminLogin` | Mauvais mdp → 401, bon mdp → 200 + cookie JWT, `/auth/me` → admin |
| `TestSSHKeyCRUD` | POST create → GET list → DELETE → GET list vide |
| `TestTemplateImportAndList` | Import JSON → list → slug `node-dev` présent |
| `TestInstanceLifecycleWithMockIncus` | Create → vérifie SSH key dans cloud-init, volume créé, IP stockée → Stop → Start → Delete → mock vidé |
| `TestRebuildKeepsWorkspace` | Create → Rebuild → volume workspace préservé, instance recréée |
| `TestAdminCanCreateInstanceForUser` | Admin crée une instance pour un user normal via `/admin/instances` |

## Ce qui est testé en profondeur

- **Injection SSH dans cloud-init** : le test vérifie que la clé publique enregistrée via l'API apparaît dans le `user.user-data` envoyé à Incus.
- **Volume workspace** : créé au bon nom (`plati-{uid}-{name}-workspace`), supprimé au delete, **préservé** au rebuild.
- **Cycle de vie complet** : create → running → stop → start → delete, avec vérification DB + mock à chaque étape.
