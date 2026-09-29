# VM Windows, console BIOS/UEFI et ISO — état des lieux

> Rapport de faisabilité, **rien n'est implémenté**. Décision du 2026-09-29 : pour l'instant,
> les VM Windows se gèrent **directement dans Incus** (CLI ou UI web d'Incus), hors de Plati.
> Ce document garde la trace de ce qu'il faudrait pour l'intégrer plus tard.
>
> Sources : lecture du code Plati + docs, code source et notes de version d'Incus (voir §7).
> Les points marqués *(non vérifié)* n'ont pas pu être confirmés dans une source primaire.

## 0. Constat dans Plati aujourd'hui

- **Plati ne crée que des conteneurs.** `CreateInstance` (`backend/internal/incus/client.go:49`)
  ne renseigne pas `Type` → défaut Incus = conteneur. `models.Template` n'a aucun champ de type.
  Un conteneur n'a **ni BIOS, ni écran, ni ISO** : tout ce qui suit suppose d'abord un
  **support VM** (`type: virtual-machine`).
- **Tout passe par `exec`** : commandes d'init, mixins, Tailscale, `authorized_keys`,
  navigateur de fichiers (`find`), sonde des liens. OK sur une VM Linux (les images `/cloud`
  embarquent l'`incus-agent`), mais ces scripts sont bash/Linux — inutilisables sous Windows.
- **Le terminal** (`handlers/terminal.go:41`) relaie *une* WebSocket texte vers `exec`. La
  console graphique (SPICE) demande du **binaire** et **une WebSocket par canal**.
- **`ReadTimeout: 15s`** (`cmd/plati-server/main.go:216`) interdit d'uploader un ISO de
  plusieurs Go à travers Plati.
- Les proxys `frontend/server.js` et `vite.config.ts` gèrent déjà WebSocket et corps en flux.
- SDK : `github.com/lxc/incus/v6 v6.5.0`. Mais ce sont les versions du **serveur** Incus qui
  comptent pour les fonctions ci-dessous.

## 1. Afficher l'écran (BIOS/UEFI, installateur, bureau)

| Option | Ce qu'on voit | Effort | Verdict |
|---|---|---|---|
| **A. Console VGA (SPICE) dans le navigateur** | Tout : logo UEFI, menu firmware (Échap), installateur, bureau | Moyen | La voie pour le BIOS |
| **B. Console texte (série) dans xterm.js** | Boot Linux, GRUB, getty ; rien d'utile sous Windows | Faible | Bonus bon marché |
| **C. Capture d'écran PNG** | Image figée de l'écran | Très faible | Vignette / diagnostic |
| **D. RDP + Guacamole** | Bureau Windows une fois démarré | Moyen + un service | Confort, **inutile pour le BIOS** |
| **E. UI web d'Incus** (`/ui`) | Tout, upload d'ISO compris | Nul | **Retenu pour l'instant** (admins) |
| **F. VNC via `raw.qemu`** | — | — | Non supporté, contourne l'auth Incus : à éviter |

### A. Console VGA

- `POST /1.0/instances/{name}/console` avec `{"type":"vga","width":…,"height":…}` (extension
  `console_vga_type`) → opération avec deux secrets : `fds["0"]` (données) et `fds["control"]`.
- Le flux de données est du **SPICE brut** sur WebSocket binaire. SPICE ouvre **une connexion
  par canal** (display, inputs, cursor…) ; Incus accepte plusieurs connexions sur `"0"`.
- Go : `ConsoleInstanceDynamic(name, api.InstanceConsolePost, *InstanceConsoleArgs)` renvoie
  `(Operation, func(io.ReadWriteCloser) error, error)` — la fonction s'appelle une fois par
  connexion. Le backend Plati relaierait chaque WS navigateur vers un appel.
- **Une seule session par VM** : une seconde exige `force: true` (extension `console_force`)
  et coupe la première.
- Client navigateur : **spice-html5**, comme l'UI Incus — implémentation de référence :
  `src/pages/instances/InstanceGraphicConsole.tsx` (lxd-ui / incus-ui-canonical), qui se
  connecte à `wss://{host}/1.0/operations/{id}/websocket?secret={fds["0"]}`.
  `InstanceConsoleShortcuts.tsx` ajoute Ctrl+Alt+Suppr, Alt+Tab, Alt+F4, F1–F12, plein écran.

Limites de spice-html5 :

- Projet quasi dormant (dernière version 0.3.0) ; un fork se dit maintenu
  (`screamo-boop/spice-html5`).
- **AZERTY mal géré** : il mappe les caractères vers des scancodes US. Correctif documenté :
  patcher `common_scanmap` dans `utils.js`.
- Pas de presse-papiers ; `sendCtrlAltDel` cassé en amont ; audio et perfs limités
  *(non vérifié)*.

→ Suffisant pour un BIOS ou une installation, pas pour un bureau Windows au quotidien.

### B. Console texte et journal

- Même API avec `type: "console"` → réutilisable via `Terminal.svelte`.
- Journal en lecture seule : `GET /1.0/instances/{name}/console` (Go :
  `GetInstanceConsoleLog`), fonctionne aussi pour les VM — utile pour un boot raté.

### C. Capture d'écran

- `GET /1.0/instances/{name}/console?type=vga` → `image/png` (extension
  `instance_console_screenshot`, **Incus ≥ 6.8**), même pendant qu'une console est ouverte.
- Pas de méthode dans le SDK Go : GET brut.
- **CVE-2026-33711** (élévation de privilèges locale via ce chemin) : garder Incus à jour.

### Spécificités BIOS/UEFI

- Les VM démarrent sur **OVMF (UEFI)**. Menu firmware : **Échap pendant le logo** → la console
  doit être ouverte **dès le démarrage** (`incus start <vm> --console=vga` ; dans une UI, un
  bouton « Démarrer + console »).
- Aucune clé Incus pour allonger le délai du menu. Peut-être `raw.qemu` avec
  `menu=on,splash-time=…` *(non vérifié, et les overrides raw peuvent casser Incus)*.
- `security.csm=true` (+ `security.secureboot=false`) = firmware legacy BIOS — incompatible
  Windows 11.
- Le shell EFI a été retiré des paquets Zabbly stables.

## 2. ISO

- Natif : volume personnalisé de type `iso` (extension `custom_volume_iso`).
  - CLI : `incus storage volume import <pool> <fichier.iso> <nom> --type=iso`
  - REST : `POST /1.0/storage-pools/{pool}/volumes/custom`, corps = le fichier,
    en-têtes `Content-Type: application/octet-stream`, `X-Incus-name`, `X-Incus-type: iso`.
  - Go : `CreateStoragePoolVolumeFromISO(pool, StorageVolumeBackupArgs{Name, BackupFile})`.
- **Toujours en lecture seule** → attachable à plusieurs VM à la fois. VM uniquement.
- Montage : device disque `pool=… source=… boot.priority=10` (plus haut = démarre en premier).
- **Attache / détache / éjection à chaud** depuis Incus 6.15 (PR lxc/incus#2282). Nombre de
  slots PCI hotplug limité sur une VM en marche (issue #1086).
- **Pas d'import depuis une URL** côté Incus : upload seulement. Alternative : disque
  `source=/chemin/sur/hôte.iso`, mais le chemin est sur l'**hôte Incus**.
- L'UI Incus sait uploader des ISO (`UploadCustomIso.tsx`, avec barre de progression).

Si c'était intégré à Plati un jour :

- bibliothèque d'ISO gérée par l'admin (table `isos`, routes admin) ;
- upload en flux avec `ReadTimeout` levé pour cette seule route (`http.ResponseController`),
  ou import depuis une URL que le backend télécharge et pousse en flux (`io.Pipe`) ;
- action « insérer / éjecter un ISO » sur la page d'une instance VM ;
- attention au trajet d'un ISO de 5–6 Go : Tailscale Serve → `server.js` → backend → Incus.

## 3. Windows

- **Configuration recommandée par stgraber** (discuss.linuxcontainers.org, fil
  « io.bus=usb in Incus 6.16 ») :
  - disque racine en `io.bus=nvme` — l'installateur le voit sans pilote virtio ;
  - ISO d'installation et ISO `virtio-win` en `io.bus=usb` (vrais lecteurs CD depuis 6.16) ;
  - un device `tpm` (Windows 11 ; non hot-pluggable, VM arrêtée) ;
  - `image.os=Windows` : désactive les devices non supportés (9p…), RTC en heure locale,
    IOMMU Intel.
- **Secure Boot** actif par défaut avec les clés Microsoft.
- `distrobuilder repack-windows` (injection des pilotes virtio dans l'ISO) n'est plus
  nécessaire depuis 6.16.
- **« Press any key to boot from CD or DVD »** : il faut la console VGA ouverte au démarrage.
  Raté → `incus stop -f` puis relancer. Alternative : ISO reconstruit avec
  `efisys_noprompt.bin` *(non vérifié sur Incus)*.
- **Agent Windows** : apparu en 6.13 (via le réseau), passé en vsock en 6.22 (février 2026),
  exec interactif en 7.5. Il s'installe via un disque `source=agent:config`. Remontée d'IP et
  d'état *(non vérifié)*.
- Pas d'images Windows sur `images:` ; licence à la charge de l'utilisateur.
- Pour Plati, une VM Windows ne pourrait être qu'un **mode dégradé** : démarrer, arrêter,
  console, ISO. Pas de mixins, clés SSH, Tailscale, navigateur de fichiers ni liens.

## 4. Prérequis sur l'hôte Incus (à vérifier)

- QEMU ≥ 8.2, `/dev/kvm` présent, VT-x / AMD-V activé.
- Si l'hôte Incus est lui-même une VM : virtualisation imbriquée
  (`/sys/module/kvm_intel/parameters/nested` = `Y`, ou `kvm_amd` = `1`).
- `incus info` → `driver` doit contenir `qemu` (ex. `lxc | qemu`). Plati ne le verrait pas seul
  (`SkipGetServer: true`).
- Versions serveur utiles : 6.8 (capture), 6.15 (éjection à chaud), 6.16 (ISO en USB,
  plus besoin de repack), 6.22 (agent Windows en vsock).

```bash
incus version
incus info | grep -E 'driver|server_version'
ls -l /dev/kvm
```

## 5. Procédure manuelle dans Incus (voie retenue)

UI web : `incus webui`, ou `https://<hôte>:8443/ui/` — console graphique et upload d'ISO
intégrés. En CLI :

```bash
# 1. Importer les ISO (lecture seule, réutilisables)
incus storage volume import default Win11.iso win11-iso --type=iso
incus storage volume import default virtio-win.iso virtio-win --type=iso

# 2. Créer la VM vide
incus init win11 --empty --vm \
  -c limits.cpu=4 -c limits.memory=8GiB -c image.os=Windows \
  -d root,size=80GiB -d root,io.bus=nvme

# 3. TPM + ISO (VM arrêtée)
incus config device add win11 vtpm tpm
incus config device add win11 install disk pool=default source=win11-iso boot.priority=10 io.bus=usb
incus config device add win11 drivers disk pool=default source=virtio-win io.bus=usb

# 4. Démarrer avec la console graphique (appuyer sur une touche pour booter sur l'ISO)
incus start win11 --console=vga
# ou, VM déjà démarrée : incus console win11 --type=vga

# 5. Après l'installation : retirer l'ISO, ajouter l'agent Windows
incus config device remove win11 install
incus config device add win11 agent disk source=agent:config
```

Installer ensuite les pilotes réseau et le reste depuis le lecteur `virtio-win`.

Pour une image réutilisable (« golden ») : dans Windows, `sysprep /generalize /oobe /shutdown`,
puis :

```bash
incus publish win11 --alias windows-11-golden
incus launch windows-11-golden win11-bob --vm   # VM arrêtée et TPM ajouté selon besoin
```

## 6. Si on l'intègre à Plati plus tard — ordre conseillé

1. Support VM dans les templates : `type`, `image.os`, TPM, `io.bus`, image golden. C'est le
   prérequis de tout le reste.
2. Console texte + journal de console + capture d'écran (peu coûteux, réutilise l'existant).
3. Console VGA spice-html5 relayée par le backend (calquée sur `InstanceGraphicConsole.tsx`),
   patch AZERTY, bouton « Démarrer + console ».
4. Bibliothèque d'ISO : upload en flux ou import par URL, insérer / éjecter.
5. Si le bureau Windows devient un vrai usage : RDP via Tailscale ou Guacamole (le support
   SPICE de Guacamole, GUACAMOLE-261 / guacamole-server PR #688, n'est pas publié).

## 7. Sources

- API console, SPICE multi-canaux, Go client : `lxc/incus` — `client/interfaces.go`,
  `cmd/incusd/instance_console.go`, `doc/api-extensions.md`
- UI Incus : `canonical/lxd-ui` — `src/pages/instances/InstanceGraphicConsole.tsx` ;
  `zabbly/incus-ui-canonical` ; https://blog.simos.info/how-to-install-and-setup-the-incus-web-ui/
- Capture d'écran : https://github.com/lxc/incus/issues/1419 ;
  https://github.com/advisories/GHSA-q9vp-3wcg-8p4x
- ISO : https://linuxcontainers.org/incus/docs/main/howto/storage_volumes/ ;
  `client/incus_storage_volumes.go` ; https://github.com/lxc/incus/pull/2282
- Windows : https://discuss.linuxcontainers.org/t/io-bus-usb-in-incus-6-16/24701/8 ;
  https://discuss.linuxcontainers.org/t/incus-6-13-has-been-released/23899 ;
  https://linuxcontainers.org/incus/news/2026_02_27_18_28.html ;
  https://discuss.linuxcontainers.org/t/incus-7-5-has-been-released/27273 ;
  https://discussion.scottibyte.com/t/super-easy-windows-11-install-in-an-incus-vm/679
- TPM : https://linuxcontainers.org/incus/docs/main/reference/devices_tpm/
- Shell EFI : https://discuss.linuxcontainers.org/t/efi-shell-access-from-ovmf-boot-menu/23706
- spice-html5 et claviers : https://lists.freedesktop.org/archives/spice-devel/2016-March/027091.html ;
  https://access.redhat.com/solutions/877613
- Guacamole SPICE : https://github.com/apache/guacamole-server/pull/688
- Prérequis : https://linuxcontainers.org/incus/docs/main/requirements/ ;
  https://blog.simos.info/how-to-run-an-incus-vm-inside-an-incus-vm-nested-virtualization/
