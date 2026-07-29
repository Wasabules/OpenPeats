# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Langue

Le dépôt est **intégralement en français** : commentaires, messages d'erreur, noms de
tests, documentation, messages de commit. Les identifiants de code restent en anglais.
Toute contribution doit s'y conformer, accents compris.

## Commandes

### Application de bureau (`peatsaudio/`, Wails + Go + Svelte)

```bash
cd peatsaudio
wails build -tags webkit2_41   # Ubuntu 24.04 : le tag est obligatoire (plus de webkit2gtk-4.0)
wails dev -tags webkit2_41     # développement
```

Sous Windows et macOS, le tag `webkit2_41` ne s'applique pas : `wails build` suffit.

### Tests

```bash
cd peatsaudio
go test ./internal/peats -v                        # unitaires, sans matériel
go test ./internal/peats -run TestEncodeEQBand -v  # un seul test
PEATS_MAC=98:80:BB:40:D4:ED go test ./internal/peats -run TestLive -v   # nécessite le casque
```

`go vet ./...` et `go build ./...` **échouent sur un arbre propre** : `main.go` fait
`//go:embed all:frontend/dist`, et `frontend/dist/` est ignoré par git. Se limiter à
`go vet ./internal/... ./cmd/...`, ou construire le frontend d'abord
(`cd frontend && npm install && npm run build`).

### peatsctl — validation d'un transport sans interface graphique

```bash
cd peatsaudio
go build ./cmd/peatsctl
./peatsctl -addr 98:80:BB:40:D4:ED           # lecture seule
./peatsctl -addr 98:80:BB:40:D4:ED -anc 1    # écriture du mode ANC, puis relecture
```

C'est l'outil à utiliser pour valider une plateforme dont le transport n'a jamais
tourné : il exerce connexion, lecture et écriture sans dépendre de Wails.

### Android (`android/`, Kotlin + Compose)

```bash
cd android
ANDROID_HOME=$HOME/Android/Sdk ./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

### Sonde d'exploration (`probe/`, Linux uniquement)

Module Go indépendant, sans dépendances, sockets `AF_BLUETOOTH` bruts. Lecture seule
par défaut : toute commande hors liste blanche exige `-allow-write`. Sert à explorer le
protocole, pas à piloter le casque.

## Architecture

### Double implémentation délibérée

Le protocole est écrit **deux fois** — en Go (`peatsaudio/internal/peats/`) et en Kotlin
(`android/app/src/main/kotlin/com/openpeats/protocol/`) — à partir de la même
spécification. Ce n'est pas de la dette : ~350 lignes peu coûteuses à porter, et chaque
plateforme garde son transport natif sans couche de liaison.

**`PROTOCOL.md` est la référence unique.** Toute correction protocolaire doit y être
portée, puis répercutée dans les deux implémentations. Il distingue systématiquement ce
qui est *confirmé sur le fil* de ce qui reste *inféré* — préserver cette distinction :
c'est ce qui rend le document fiable.

### Couches (Go)

```
internal/peats/protocol.go     trames, opcodes, encodage EQ — pur, sans I/O
internal/peats/client.go       échanges, retry, découverte de canal — sans OS
internal/peats/transport.go    interface rfcommConn + var dialRFCOMM
internal/peats/transport_*.go  une implémentation par OS, choisie par tag de build
app.go                         état applicatif + méthodes liées à Wails
frontend/src/                  interface Svelte
```

`dialRFCOMM` est une variable globale que chaque `transport_*.go` renseigne dans son
`init()`. Ajouter une plateforme = ajouter un fichier avec le bon tag de build et
implémenter `rfcommConn` + `isTimeout` ; rien d'autre ne bouge.
`transport_stub.go` (`//go:build !linux && !windows`) renvoie `ErrUnsupported`.

### État des transports

| Système | Transport | État |
|---|---|---|
| Linux | `AF_BLUETOOTH` / `BTPROTO_RFCOMM` | validé sur matériel |
| Android | `BluetoothSocket` (Kotlin) | validé sur matériel |
| Windows | Winsock `AF_BTH` | validé sur matériel (lecture + écriture) |
| macOS | IOBluetooth | à faire — pas d'API socket, exige un pont cgo |
| iOS | — | impossible (SPP réservé au MFi) |

Les transports ne se déduisent pas les uns des autres : écrire le transport
Windows en miroir de Linux y a introduit deux bugs (structure alignée au lieu
d'être packée, `select` sans `exceptfds`). Les pièges par plateforme sont
documentés dans `peatsaudio/README.md`.

## Pièges du protocole

Ces points cassent silencieusement tout client qui les ignore. Ils sont détaillés dans
`peatsaudio/README.md` et `PROTOCOL.md` §7.

- **Ne jamais réessayer une écriture.** Plusieurs `set` prennent effet *sans* acquitter
  (mode ANC, multipoint). Renvoyer la commande rejoue l'effet de bord — trois invites
  vocales pour un seul changement voulu. Pour vérifier qu'un `set` a pris : **relire**.
  Les `get`, eux, sont réessayés (3 fois) : ils sont sans effet de bord et les réponses
  sont intermittentes.
- **L'ack d'un `set` porte l'opcode de réponse du `get`.** `03 11` répond `83 10`, pas
  `83 11`. Attendre `83 11` bloque indéfiniment.
- **L'EQ échappe aux conventions générales** : `0E 02` répond `0F 82` (et non `8E 02`),
  et utilise `vendorId 001D` au lieu de `000A`.
- **Payloads asymétriques** : les réponses encodent les scalaires sur 2 octets
  big-endian (`00 64` = 100), les écritures prennent 1 octet.
- **Délai de stabilisation de ~700 ms** après l'établissement du lien, sinon les
  premières commandes restent sans réponse.
- **Ne pas fermer puis rouvrir un canal** : les écouteurs refusent la reconnexion
  immédiate. `dialAny` conserve donc le socket de la première connexion réussie.
- **LDAC et multipoint sont exclusifs** (le firmware coupe LDAC lui-même) ; l'UI les
  présente comme un choix, pas deux bascules.

## Conventions

**Détection de fonctions plutôt que liste en dur.** `App.Refresh()` interroge chaque
fonction et renseigne `State.Supported` ; le frontend grise ce qui ne répond pas. Le H3
n'expose que 16 commandes sur 31 — d'autres modèles en exposeront d'autres. Ne jamais
coder en dur les capacités d'un modèle.

**Marquer ce qui n'est pas vérifié.** Le code signale explicitement le non-confirmé
(écriture du mapping tactile, `CUSTOMIZE_TOUCH_DEMO`), et l'UI le répercute. Conserver
ces avertissements tant que le matériel ne les a pas tranchés.

**Commandes destructives** (`CmdReset`, `CmdReboot`) : jamais émises sans confirmation
explicite de l'utilisateur.

## Contrainte légale — stricte

Le projet relève de l'interopérabilité (directive 2009/24/CE art. 6 ; 17 U.S.C.
§1201(f)). Le dépôt **ne doit jamais contenir** : l'APK officiel ou une partie de
celui-ci, du code décompilé ou du smali, des ressources ou marques du constructeur.
Ces éléments sont exclus par `.gitignore` (`*.apk`, `jadx-out/`, `smali-out/`, `*.dex`,
`*.btsnoop`) et doivent rester locaux. Tout le code publié est écrit indépendamment à
partir du comportement observé.
