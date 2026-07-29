# OpenPeats

Contrôle libre des écouteurs **SoundPEATS** — ANC, égaliseur, multipoint,
batteries — sans passer par l'application mobile du constructeur.

Développé et validé sur un **SOUNDPEATS H3** (firmware `S5_20250605_V0.4.9`)
sous Ubuntu 24.04, puis sous Windows 11 et Android.

> **Projet non officiel.** Aucun lien avec SoundPEATS. Fourni sans garantie.

## Contenu

| Dossier | Rôle |
|---|---|
| [`PROTOCOL.md`](PROTOCOL.md) | Spécification du protocole, avec le statut de vérification de chaque point |
| [`peatsaudio/`](peatsaudio/) | Application de bureau (Wails + Go + Svelte) |
| [`android/`](android/) | Application Android (Kotlin + Jetpack Compose) |
| [`probe/`](probe/) | Sonde en ligne de commande pour explorer le protocole |

Le protocole est implémenté deux fois — en Go et en Kotlin — à partir de la même
spécification. C'est délibéré : les ~350 lignes de logique sont peu coûteuses à
porter, et chaque plateforme garde ainsi son transport natif sans couche de
liaison. `PROTOCOL.md` reste la référence unique.

```bash
cd android
ANDROID_HOME=$HOME/Android/Sdk ./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Le cœur réutilisable est [`peatsaudio/internal/peats`](peatsaudio/internal/peats) :
protocole pur (trames, opcodes, encodage de l'égaliseur) et transport
RFCOMM/SPP, sans dépendance à l'interface.

## Fonctionnalités

- Réduction de bruit : arrêt / ANC / transparence, plus 4 sous-modes
  (adaptative, intérieur, extérieur, trafic) et 2 niveaux de transparence
- Égaliseur paramétrique 5/8/10 bandes, avec courbe manipulable et réglage
  du Q par bande
- LDAC et multipoint — présentés comme un choix exclusif, parce que le
  firmware coupe LDAC dès que le multipoint s'active
- Batteries gauche / droite / boîtier, avec notification de bureau sous seuil
- Mode jeu, LE Audio, désactivation du tactile, invites vocales
- Lecture du mapping des commandes tactiles
- Console de trames brutes pour l'exploration

L'application interroge chaque fonction au démarrage et grise ce qui ne répond
pas : elle s'adapte donc aux modèles qui exposent un sous-ensemble différent.

## Démarrage

```bash
cd peatsaudio
wails build -tags webkit2_41   # Ubuntu 24.04 : webkit2gtk-4.1
./build/bin/peatsaudio
```

Les écouteurs doivent être appairés et hors de l'étui.

## Plateformes

Le protocole circule sur RFCOMM (Bluetooth Classic, profil SPP). Seule la
couche transport change d'un système à l'autre ; la logique est commune.

| Système | Transport | État |
|---|---|---|
| Linux | `AF_BLUETOOTH` / `BTPROTO_RFCOMM` | ✅ lecture et écriture validées |
| Android | `BluetoothSocket` (Kotlin) | ✅ lecture et écriture validées |
| Windows | Winsock `AF_BTH` | ✅ lecture et écriture validées |
| macOS | IOBluetooth | ❌ à faire — voir ci-dessous |
| iOS | — | 🚫 impossible |

Sur Android, la validation couvre la lecture complète de l'état, le changement
de mode ANC et l'application d'une courbe d'égaliseur, vérifiés sur un H3.

Sur Windows 11, la validation a été faite avec `peatsctl` sur un H3 : connexion
sur le canal RFCOMM 11, 9 lectures sur 9 abouties, puis changement de mode ANC
confirmé par relecture et à l'oreille. Un piège s'y est révélé : `ws2bth.h`
inclut `pshpack1.h`, donc `SOCKADDR_BTH` est **packé sur 1 octet** — 30 octets,
`btAddr` à l'offset 2. Une structure alignée naturellement place `btAddr` à
l'offset 8 et fait rejeter tout `connect` par `WSAEADDRNOTAVAIL`, sur tous les
canaux.

**macOS** est réalisable mais demande un pont cgo vers IOBluetooth, un
framework Objective-C à API asynchrone par délégués : il n'y a pas d'API socket
comme sur Linux et Windows, donc le code ne peut pas être écrit en miroir des
deux autres transports.

**Android** tourne sur un noyau Linux qui gère `AF_BLUETOOTH`, mais SELinux et
le modèle de permissions bloquent les sockets Bluetooth bruts depuis le NDK. Le
chemin pris en charge est `android.bluetooth.BluetoothSocket`, côté Kotlin.

**iOS** est hors d'atteinte quel que soit le framework : le profil SPP y est
réservé aux accessoires certifiés MFi, ce que ces écouteurs ne sont pas.

## Statut de la rétro-ingénierie

Le protocole a été reconstitué par analyse de l'application Android officielle,
puis **vérifié trame par trame** sur du matériel réel. `PROTOCOL.md` distingue
systématiquement ce qui est confirmé sur le fil de ce qui reste supposé.

Deux points restent non validés et sont signalés comme tels dans l'interface :

- l'**écriture** du mapping tactile (la lecture, elle, fonctionne) ;
- le mode démo tactile de l'app Android : la commande est acquittée, mais les
  appuis ne remontent jamais — le tactile est géré en local par le firmware.

## Légalité

Ce projet relève de l'**interopérabilité** : il permet à du matériel acheté
légalement de fonctionner avec un système d'exploitation que le constructeur ne
prend pas en charge. La rétro-ingénierie à cette fin est explicitement permise
par la directive européenne 2009/24/CE (art. 6) et, aux États-Unis, par le
17 U.S.C. §1201(f).

En conséquence, ce dépôt **ne contient ni ne contiendra** :

- l'APK de l'application officielle ou une quelconque partie de celle-ci ;
- du code décompilé, du smali, ou des ressources extraites ;
- des marques, icônes ou éléments graphiques du constructeur.

Tout le code publié ici a été écrit indépendamment à partir de la description
du comportement observé. Ces éléments sont exclus par [`.gitignore`](.gitignore).

## Licence

MIT — voir [`LICENSE`](LICENSE).
