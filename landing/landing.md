# Plati — Ton environnement de dev, prêt en un clic

> Des environnements de développement Incus persistants, accessibles à la demande, sans friction.

---

## Le problème

Configurer un environnement de dev prend du temps. Le maintenir à jour aussi. Et quand tu changes de machine, tu recommences de zéro. Les environnements divergent entre développeurs, les "ça marche chez moi" s'accumulent, et les onboardings prennent des jours.

**Plati supprime tout ça.**

---

## Ce qu'est Plati

Plati est une plateforme interne qui provisionne et gère des environnements de développement basés sur [Incus](https://linuxcontainers.org/incus/). Chaque développeur obtient un container isolé, reproductible, persistant — accessible via SSH depuis n'importe où.

Un template → un click → un environnement identique pour toute l'équipe.

---

## Fonctionnalités

### Création en un clic
Choisissez un template (Ubuntu, Debian, image custom…), cliquez sur *Créer* — Plati provisionne le container, monte le volume de travail, injecte vos clés SSH et démarre l'instance. Prêt en quelques secondes.

### Espaces de travail persistants
Le répertoire `/workspace` survit aux rebuilds. Vos fichiers, votre code, votre historique git — tout reste intact même quand l'image est reconstruite depuis zéro.

### Accès SSH natif
Plati injecte automatiquement votre clé SSH dans chaque instance. Connectez-vous directement sans mot de passe, depuis votre terminal ou votre IDE (VS Code Remote, JetBrains Gateway…).

### Auto-sleep intelligent
Les instances inactives sont automatiquement arrêtées après une période configurable (défaut : 4h). Elles redémarrent en quelques secondes au prochain accès. Économies de ressources, sans perte de données.

### Rebuild sans friction
Mettez à jour votre environnement en un click : Plati détache votre volume, recrée le container depuis l'image fraîche, réattache vos données. Pas de perte, pas d'accumulation de dette technique sur l'image.

### Secrets chiffrés
Stockez vos tokens, clés API et variables d'environnement sensibles dans Plati. Ils sont chiffrés au repos (AES-256-GCM) et injectés dans vos instances à la création.

### Templates configurables
Les administrateurs définissent des templates YAML : image de base, ressources (CPU, RAM, disque), cloud-init, scripts de premier démarrage, intégrations (Tailscale, VS Code Server, sshx…). Les développeurs n'ont qu'à choisir.

### Intégration Tailscale
Accédez à vos instances depuis n'importe quel réseau via Tailscale. Plati gère l'injection de la clé d'auth automatiquement — en mode plateforme (clé partagée) ou personnel.

### Multi-serveurs
Distribuez vos instances sur plusieurs serveurs Incus. Plati sélectionne automatiquement le serveur disponible au moment de la création.

### Authentification Microsoft Entra (SSO)
Connectez Plati à votre tenant Azure AD pour une authentification SSO transparente. Compatible avec les organisations déjà sur Microsoft 365.

---

## Bénéfices

| Pour les développeurs | Pour les équipes |
|---|---|
| Environnement prêt en secondes | Tous les devs sur la même base image |
| Fini le "ça marche chez moi" | Onboarding réduit de jours à minutes |
| Workspace persistant entre les sessions | Ressources optimisées grâce à l'auto-sleep |
| Accès SSH depuis n'importe quel outil | Secrets centralisés et chiffrés |
| Rebuild propre sans perdre son travail | Audit et contrôle via l'interface admin |

---

## Comment ça marche

```
1. L'admin crée un template (image + ressources + scripts)
2. Le dev choisit un template et clique sur "Créer"
3. Plati provisionne : volume → container → cloud-init → SSH
4. Le dev se connecte via SSH ou VS Code Remote
5. L'instance se met en veille si inactivité > 4h
6. Un click suffit à la réveiller
```

---

## Stack technique

Plati est une application Go + SvelteKit légère, conçue pour tourner sur votre infrastructure interne :

- **Backend** : Go 1.23, chi, sqlx, SQLite (WAL)
- **Frontend** : SvelteKit 2, Svelte 5, Tailwind CSS 4
- **Virtualisation** : Incus (LXD fork), TLS mutual auth
- **Déploiement** : Docker Compose, moins de 100 Mo

Pas de dépendance cloud. Pas de données qui sortent de votre infrastructure.

---

## Prêt à simplifier vos environnements de dev ?

Plati est open source et conçu pour être déployé en interne en moins d'une heure.

**[Voir la documentation](../README.md)** · **[Démarrer en local](../README.md#quick-start)** · **[Explorer les templates](../config/templates/)**

---

*Plati est développé en interne. Contributions et retours bienvenus.*
