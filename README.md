# OpenPeats

Contrôle libre des écouteurs **SoundPEATS** — ANC, égaliseur, multipoint,
batteries — sans passer par l'application mobile du constructeur.

Développé et validé sur un **SOUNDPEATS H3** (firmware `S5_20250605_V0.4.9`)
sous Ubuntu 24.04.

> **Projet non officiel.** Aucun lien avec SoundPEATS. Fourni sans garantie.

## Contenu

| Dossier | Rôle |
|---|---|
| [`PROTOCOL.md`](PROTOCOL.md) | Spécification du protocole, avec le statut de vérification de chaque point |
| [`peatsaudio/`](peatsaudio/) | Application de bureau (Wails + Go + Svelte) |
| [`probe/`](probe/) | Sonde en ligne de commande pour explorer le protocole |

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
