# Exposer une API locale sur un tailnet — recette Docker (Tailscale + proxy)

> **Doc portable.** Elle décrit un montage réutilisable tel quel dans n'importe
> quel projet : l'application tourne **sur l'hôte** (systemd, tâche planifiée,
> `npm start`, peu importe) et un **couple de conteneurs** — `tailscale` +
> `socat` — la publie sur le tailnet en HTTPS, sans faire rejoindre l'hôte
> entier à Tailscale et sans occuper de port privilégié.
>
> Dans ce dépôt, c'est ce qui expose `api.py` sous
> `https://transcription.burro-piranha.ts.net`. Pour un autre projet, remplacer
> partout `transcription` / `17832` par le nom et le port de l'application.

---

## 1. Ce que ça fait, et pourquoi c'est monté comme ça

```
                Tailnet
                   │
                   ▼   nœud Tailscale = le conteneur (hostname ${TS_HOSTNAME})
        ┌──────────────────────────────────────────┐
        │ conteneur « tailscale »                   │
        │   tailscaled --tun=userspace-networking   │
        │   + Tailscale Serve (80 → app, 443 → app) │
        │             │                             │
        │             ▼ netstack ne route QUE vers  │
        │          127.0.0.1:17832                  │
        │             │                             │
        │ conteneur « proxy » (network_mode: service:tailscale)
        │   socat TCP-LISTEN:17832,bind=127.0.0.1   │
        └─────────────┼─────────────────────────────┘
                      ▼
            <gateway Docker>:17832   →  l'app sur l'HÔTE
```

Trois décisions expliquent la forme du montage ; les changer casse la chaîne :

| Décision | Pourquoi |
|---|---|
| **Tailscale dans Docker, app sur l'hôte** | On ajoute un *nœud* au tailnet sans inscrire la machine entière (elle peut être partagée, ou déjà membre d'un autre tailnet). L'app garde son environnement hôte : GPU, CUDA, venvs, `rclone`, `$HOME`. |
| **`TS_USERSPACE=true`** | Pas besoin de `/dev/net/tun` ni de `NET_ADMIN` : le conteneur reste non privilégié. **Contrepartie** : en mode userspace, tailscaled ne route le trafic entrant que vers `127.0.0.1` — qui, dans le conteneur, est le conteneur, pas l'hôte. D'où le proxy. |
| **Sidecar `socat` en `network_mode: service:tailscale`** | Il partage le *namespace réseau* de tailscaled, donc son `127.0.0.1` est bien celui que netstack vise. Il fait le seul saut manquant : `127.0.0.1:PORT` → `<gateway Docker>:PORT` = l'hôte. |

Conséquence importante : **les ports 80 et 443 sont ceux du nœud Tailscale**
(pile netstack du conteneur), pas de l'hôte. Aucun port privilégié n'est pris
sur la machine, et Serve tourne sans root.

---

## 2. Prérequis

**Côté hôte**

- Docker + `docker compose` (v2).
- L'application écoute sur une IP **joignable depuis Docker** :
  `0.0.0.0:<PORT>` en pratique. Un bind `127.0.0.1` **strict ne marche pas**.
- Le port applicatif protégé au firewall côté LAN, puisqu'il est ouvert sur
  `0.0.0.0` (ex. `ufw deny 17832/tcp`, puis autoriser le sous-réseau `docker0`).

**Côté tailnet (admin console)**

- Une **clé d'authentification** (Settings → Keys). Prendre une clé
  **réutilisable** si le conteneur peut être recréé.
- **MagicDNS** activé, et **HTTPS Certificates** activé (DNS → HTTPS
  Certificates). Sans eux, l'émission ACME échoue : le nœud est en ligne mais
  seul l'accès direct au port applicatif répond, pas 80/443.

---

## 3. Les fichiers

Quatre fichiers, plus l'unité de service de l'app (§4). Chemins tels qu'utilisés
dans ce dépôt ; à adapter si l'arborescence diffère.

| Fichier | Rôle | Commité ? |
|---|---|---|
| `docker-compose.tailscale.yml` | les deux services (`tailscale`, `proxy`) | oui |
| `deploy/tailscale/serve.json` | config Tailscale Serve (80/443 → app) | oui |
| `.env.tailscale.example` | modèle de variables, sans secret | oui |
| `.env.tailscale` | variables réelles, **contient la clé d'auth** | **non — gitignoré** |

### 3.1 `docker-compose.tailscale.yml`

```yaml
services:
  tailscale:
    image: tailscale/tailscale:latest
    container_name: transcription-tailscale
    # Nom du nœud tel qu'il apparaît dans le tailnet / MagicDNS.
    hostname: ${TS_HOSTNAME:-transcription-catie}
    environment:
      TS_AUTHKEY: ${TS_AUTHKEY:?TS_AUTHKEY manquant — voir .env.tailscale.example}
      TS_STATE_DIR: /var/lib/tailscale
      # userspace-networking : pas besoin de /dev/net/tun ni de NET_ADMIN.
      TS_USERSPACE: "true"
      # Ex. tags ACL : TS_EXTRA_ARGS=--advertise-tags=tag:transcription
      TS_EXTRA_ARGS: ${TS_EXTRA_ARGS:-}
      # Publie l'app sur les ports 80/443 DU NŒUD (netstack), cert Tailscale.
      TS_SERVE_CONFIG: /config/serve.json
    volumes:
      - tailscale-state:/var/lib/tailscale
      - ./deploy/tailscale/serve.json:/config/serve.json:ro
    restart: unless-stopped

  # Mini-proxy dans le namespace réseau de tailscaled :
  #   127.0.0.1:${APP_PORT}  ->  <hôte>:${APP_PORT}
  proxy:
    image: alpine/socat:latest
    container_name: transcription-tailscale-proxy
    network_mode: "service:tailscale"
    depends_on:
      - tailscale
    environment:
      APP_PORT: ${APP_PORT:-17832}
      # Vide = détection auto de la gateway Docker (= l'hôte).
      APP_HOST: ${APP_HOST:-}
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        set -eu
        PORT="$${APP_PORT:-17832}"
        TARGET="$${APP_HOST:-}"
        if [ -z "$$TARGET" ]; then
          TARGET=$$(ip route 2>/dev/null | awk '/^default/ {print $$3; exit}') || true
        fi
        [ -n "$$TARGET" ] || TARGET=172.17.0.1
        echo "[proxy] 127.0.0.1:$$PORT -> $$TARGET:$$PORT"
        exec socat -d TCP4-LISTEN:$$PORT,bind=127.0.0.1,fork,reuseaddr TCP4:$$TARGET:$$PORT
    restart: unless-stopped

volumes:
  tailscale-state:
```

Points à ne pas modifier à la légère :

- **`$$`** dans le bloc `command` : c'est l'échappement Compose. `$$PORT` arrive
  comme `$PORT` dans le shell du conteneur ; écrire `$PORT` ferait interpoler la
  variable par Compose *à la lecture du fichier* (donc vide).
- **`network_mode: "service:tailscale"`** : sans ça, le `socat` écoute dans son
  propre namespace et tailscaled ne le voit pas.
- **`bind=127.0.0.1`** : c'est exactement l'adresse que vise netstack.
- **volume `tailscale-state`** : il porte l'identité du nœud. Le conserver évite
  de redemander une clé d'auth à chaque recréation — indispensable avec une clé
  à usage unique.

### 3.2 `deploy/tailscale/serve.json`

```json
{
  "TCP": {
    "80":  { "HTTP": true },
    "443": { "HTTPS": true }
  },
  "Web": {
    "${TS_CERT_DOMAIN}:80": {
      "Handlers": {
        "/": { "Proxy": "http://127.0.0.1:17832" }
      }
    },
    "${TS_CERT_DOMAIN}:443": {
      "Handlers": {
        "/": { "Proxy": "http://127.0.0.1:17832" }
      }
    }
  },
  "AllowFunnel": {}
}
```

- `${TS_CERT_DOMAIN}` est substitué **par le conteneur Tailscale** au démarrage
  (`<hostname>.<tailnet>.ts.net`) : ne pas l'écrire en dur, le fichier reste
  ainsi portable d'un tailnet à l'autre.
- Le `Proxy` pointe vers `127.0.0.1:<PORT>` = le `socat`. **Ce port est écrit en
  dur ici** : Serve ne lit pas `${APP_PORT}`. Changer le port applicatif oblige
  donc à éditer ce fichier aussi (cf. §6).
- `"AllowFunnel": {}` garde le service **privé au tailnet**. Ne rien y mettre à
  moins de vouloir une exposition publique sur Internet (Funnel).

### 3.3 `.env.tailscale.example` (à copier en `.env.tailscale`)

```bash
# Clé d'authentification Tailscale (admin console → Settings → Keys).
# Prendre une clé RÉUTILISABLE si le conteneur peut être recréé.
TS_AUTHKEY=tskey-auth-xxxxxxxxxxxx

# Nom du nœud dans le tailnet (MagicDNS) :
#   https://<TS_HOSTNAME>.<tailnet>.ts.net   (443, cert Tailscale via Serve)
#   http://<TS_HOSTNAME>.<tailnet>.ts.net    (80)
TS_HOSTNAME=transcription-catie

# Arguments supplémentaires pour `tailscale up` (tags ACL, etc.)
#TS_EXTRA_ARGS=--advertise-tags=tag:transcription

# Port de l'app sur l'hôte (doit correspondre au PORT du service systemd).
# C'est la cible de Serve ; les ports publics du nœud sont 80 et 443.
APP_PORT=17832

# Adresse de l'app vue depuis Docker. Vide = gateway du réseau Docker (l'hôte).
# Forcer si besoin : 172.17.0.1, ou l'IP LAN de la machine.
#APP_HOST=172.17.0.1
```

Et dans `.gitignore` :

```gitignore
# Secrets Tailscale (compose)
.env.tailscale
```

---

## 4. Le serveur : l'application sur l'hôte

Le montage ne suppose qu'une chose de l'app : **écouter sur `0.0.0.0:<APP_PORT>`
et être démarrée automatiquement**. Les deux conteneurs et l'app sont
indépendants (chacun a son propre redémarrage : `restart: unless-stopped` d'un
côté, `Restart=always` de l'autre).

### Linux — unité systemd (extrait de `deploy/transcription-api.service`)

```ini
[Unit]
Description=Transcription HTTP API (FastAPI/uvicorn)
After=network-online.target
Wants=network-online.target
# Ne pas abandonner après une rafale de redémarrages (ex. réseau lent au boot).
# Cette clé va dans [Unit] : en [Service] elle est ignorée (systemd 249).
StartLimitIntervalSec=0

[Service]
Type=simple
User=jlaviole
Group=jlaviole
WorkingDirectory=/home/jlaviole/repos/transcription-catie

# Réseau : 0.0.0.0 est REQUIS — c'est ce qui rend l'app joignable depuis le
# sidecar. Le port doit correspondre à APP_PORT et à serve.json.
Environment=HOST=0.0.0.0
Environment=PORT=17832
Environment=PYTHONUNBUFFERED=1
# rclone et CUDA ont besoin du home de l'utilisateur :
Environment=HOME=/home/jlaviole

ExecStart=/home/jlaviole/repos/transcription-catie/venv-api/bin/python api.py

Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Installation : `./deploy/install-systemd.sh` (copie l'unité, `daemon-reload`,
`enable --now`). Arrêter toute instance lancée à la main avant, sinon le bind
échoue. Logs : `journalctl -u transcription-api.service -f`.

### Windows

Pas d'équivalent systemd sans outil tiers : on enregistre une **tâche
planifiée** au boot sous `SYSTEM`, avec relance automatique
(`deploy/install-windows-service.ps1`). Mêmes contraintes : `HOST=0.0.0.0` et
port cohérent.

---

## 5. Démarrer et vérifier

```bash
cp .env.tailscale.example .env.tailscale        # y mettre TS_AUTHKEY + TS_HOSTNAME
docker compose --env-file .env.tailscale -f docker-compose.tailscale.yml up -d
docker compose --env-file .env.tailscale -f docker-compose.tailscale.yml logs -f
```

Vérifications, dans l'ordre de la chaîne (la première qui échoue localise la
panne) :

```bash
# 1. l'app répond sur l'hôte
curl -s localhost:17832/health

# 2. le nœud est en ligne dans le tailnet
docker exec transcription-tailscale tailscale status | head -1

# 3. Serve mappe bien 80 et 443 vers 127.0.0.1:17832
docker exec transcription-tailscale tailscale serve status

# 4. le socat traverse jusqu'à l'hôte
docker exec transcription-tailscale wget -qO- http://127.0.0.1:17832/health

# 5. de bout en bout, depuis un autre poste du tailnet
curl https://<TS_HOSTNAME>.<tailnet>.ts.net/health        # 443, cert Tailscale
curl http://<TS_HOSTNAME>.<tailnet>.ts.net/health         # 80
curl http://<TS_HOSTNAME>.<tailnet>.ts.net:17832/health   # direct, sans Serve
```

Sortie attendue de l'étape 3 :

```
https://transcription.burro-piranha.ts.net (tailnet only)
|-- / proxy http://127.0.0.1:17832

http://transcription (tailnet only)
http://transcription.burro-piranha.ts.net (tailnet only)
|-- / proxy http://127.0.0.1:17832
```

Arrêt / recréation :

```bash
docker compose --env-file .env.tailscale -f docker-compose.tailscale.yml down
# le volume tailscale-state survit → pas de nouvelle clé d'auth au redémarrage
```

---

## 6. Adapter la recette à un autre projet

1. Copier `docker-compose.tailscale.yml`, `deploy/tailscale/serve.json`,
   `.env.tailscale.example` ; ajouter `.env.tailscale` au `.gitignore`.
2. Renommer les `container_name` (`<projet>-tailscale`,
   `<projet>-tailscale-proxy`) — ils sont globaux à la machine Docker.
3. Choisir `TS_HOSTNAME` : c'est le nom DNS public dans le tailnet.
4. Choisir `APP_PORT`. Éviter les ports très courants (8000, 8080…) : ils sont
   ouverts sur `0.0.0.0` de l'hôte.
5. **Répercuter le port aux trois endroits** — c'est le piège n°1 :
   `APP_PORT` dans `.env.tailscale`, les deux `Proxy` de `serve.json` (écrits en
   dur), et le `PORT` du service applicatif (systemd / tâche planifiée).
6. Faire écouter l'app sur `0.0.0.0`, et fermer le port au LAN côté firewall.

---

## 7. Dépannage

| Symptôme | Cause probable | Correctif |
|---|---|---|
| Le nœud apparaît en ligne, mais `https://…` ne répond pas | HTTPS Certificates ou MagicDNS désactivés dans le tailnet | Les activer dans l'admin console (DNS), puis recréer le conteneur |
| `https://` et `http://` KO, mais `:17832` direct OK | Serve mal chargé : `serve.json` non monté, ou JSON invalide | `docker exec … tailscale serve status`, vérifier le montage `:ro` et la syntaxe |
| Tout répond en 502 / connexion refusée | L'app écoute sur `127.0.0.1` strict, ou est arrêtée | `HOST=0.0.0.0` dans l'unité ; `systemctl status` |
| Le proxy logue une cible fausse | Gateway Docker non détectée (réseau non standard) | Forcer `APP_HOST=172.17.0.1` ou l'IP LAN dans `.env.tailscale` |
| `TS_AUTHKEY manquant` au `up` | `--env-file .env.tailscale` oublié | Toujours passer `--env-file` : Compose ne lit pas ce nom automatiquement |
| Le conteneur redemande une clé après recréation | Volume `tailscale-state` supprimé (`down -v`) | Ne pas utiliser `-v`, ou générer une clé réutilisable |
| Port applicatif déjà pris à l'installation du service | Instance lancée à la main encore vivante | `pkill -f '<venv>/bin/python api.py'` puis réinstaller |

---

## 8. Sécurité

- `.env.tailscale` porte la **clé d'auth** du tailnet : gitignoré, jamais commité.
  En cas de fuite, révoquer la clé dans l'admin console.
- `"AllowFunnel": {}` **vide** = accès restreint au tailnet. Ne l'activer qu'en
  connaissance de cause : Funnel publie sur l'Internet public.
- L'app écoutant sur `0.0.0.0`, elle est atteignable depuis le LAN **sans**
  passer par Tailscale : le contrôle d'accès du tailnet ne protège que la porte
  d'entrée `:80/:443`. Filtrer le port applicatif au firewall.
- Restreindre le nœud par ACL via un tag :
  `TS_EXTRA_ARGS=--advertise-tags=tag:<projet>`.
