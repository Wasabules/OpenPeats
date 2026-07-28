# Protocole PeatsAudio — rétro-ingénierie

Cible : SOUNDPEATS H3 (`98:80:BB:40:D4:ED`)
Source : `com.xingkeqi.peats` v1.9.39 (versionCode 9921088), extrait par ADB root
depuis un Redmi Note 10 (LineageOS, `sweet`).

> **Statut : analyse statique.** Tout ce qui suit est tracé jusqu'à un fichier
> décompilé (jadx) ou désassemblé (baksmali). Rien n'a encore été émis sur le fil.
> Les points marqués ⚠ sont des inférences, pas des certitudes.

Artefacts locaux : `peats-base.apk`, `peats-arm64.apk`, `jadx-out/`, `smali-out/`.

---

## 1. Architecture

L'app embarque **4 SDK de puces** (Qualcomm GAIA, BES, JieLi, Bluetrum) et route selon
le modèle de produit. Trois implémentations de `IDeviceInfo` coexistent :

| Implémentation | Transport | Rôle |
|---|---|---|
| `DefaultBleDeviceInfoModel` | BLE GATT | chemin générique |
| `DefaultSppDeviceModel` | RFCOMM / SPP | chemin générique |
| `QcyBleDeviceModel` | BLE | produits QCY |

**Résultat central de l'analyse : BLE et SPP produisent des trames strictement
identiques.** Les deux appellent `BtDataUtilKt.generatePeatsPacket$default` avec le
même masque de défauts `0x34`. Seul le transport diffère.

Vérifié en smali :
- `smali-out/…/ble/DefaultBleDeviceInfoModel$sendDataEvent$2.smali`
- `smali-out/…/spp/DefaultSppDeviceModel$sendDataEvent$2.smali`

→ **Sous Linux, viser RFCOMM/SPP.** C'est le même protocole, sans les contraintes
d'appairage LE, et exploitable avec un simple socket `AF_BLUETOOTH`/`BTPROTO_RFCOMM`.

### Transports

**SPP** — `SPP_DATA_UUID = 00001101-0000-1000-8000-00805F9B34FB` (RFCOMM standard).
Le H3 annonce bien ce service, plus des UUID vendeur `eb04`→`eb07` en BR/EDR.

**BLE GATT** — base vendeur `D102-11E1-9B23-00025B00A5A5` :

| Rôle | UUID |
|---|---|
| Service | `00001100-D102-11E1-9B23-00025B00A5A5` |
| Write | `00001101-D102-11E1-9B23-00025B00A5A5` |
| Notify | `00001102-D102-11E1-9B23-00025B00A5A5` |

Adresse BLE == adresse A2DP : **vérifié sur le H3**. Un `scan le` capte des annonces
sur `98:80:BB:40:D4:ED` (RSSI ~-53 dBm, ManufacturerData clé `0x000E`) pendant que
l'A2DP est actif sur la même adresse.

⚠ `getLdac`/`setLdac`/`getLeAudio`/`setLeAudio` sont des **stubs vides** dans le modèle
BLE (retournent `Unit`). Seul le modèle **SPP** les implémente réellement — argument de
plus pour cibler SPP.

---

## 2. Format de trame

`BtDataUtilKt.generatePeatsPacket()` — `jadx-out/sources/…/util/BtDataUtilKt.java:62`

```
┌──────┬─────────┬───────┬────────┬──────────┬─────────┬─────────┐
│ SOF  │ VERSION │ FLAGS │ LENGTH │ VENDORID │ COMMAND │ PAYLOAD │
│ 0xFF │  0x04   │ 0x00  │  len   │  2 oct.  │  2 oct. │  N oct. │
└──────┴─────────┴───────┴────────┴──────────┴─────────┴─────────┘
   1        1        1       1         2          2         N
```

- `LENGTH` = longueur du **payload seul**.
- **Aucun checksum, aucun CRC.**
- `VENDORID` = `00 0A` par défaut — **sauf pour l'EQ qui utilise `00 1D`** (voir §5).

Parsing des réponses (`buildPacket()`, ligne 94) : découpage identique, offsets
`0 / 1 / 2 / 3 / 4-6 / 6-8 / 8+`.

### Conventions

- réponse = `commande[0] | 0x80` → un `get` de `03 XX` répond `83 XX`
- `set` = `get + 1` sur le 2e octet (⚠ exceptions : `LE_AUDIO`, `WATER_DRAIN_FUNCTION`,
  `FIND_HEADPHONES_*`, `LEFT/RIGHT_EAR_MEDIA_VOLUME_CHANGE`, `PROMPT_VOLUME_LEVEL`)

### Exemples

```
Lire mode ANC        : FF 04 00 00 00 0A 03 10
Réponse              : FF 04 00 01 00 0A 83 10 <mode>
Écrire mode ANC=ANC  : FF 04 00 01 00 0A 03 11 01
Batterie gauche      : FF 04 00 00 00 0A 03 06
Réponse              : FF 04 00 01 00 0A 83 06 <0-100>
Activer LDAC         : FF 04 00 01 00 0A 03 39 01
```

---

## 3. Table des commandes

Source : `jadx-out/sources/…/data/enums/FunctionEnum.java`

Constantes résolues (jadx avait substitué des symboles de même valeur numérique) :
`UPGRADE_PROCEED_TO_COMMIT`=14, `UPGRADE_COMMIT_REQ`=15,
`UPGRADE_SILENT_COMMIT_SUPPORTED_CFM`=33, `UPGRADE_SILENT_COMMIT_CFM`=34,
`CMDID_TESTMODE`=13, `CMDID_RESTART`=37, `Base64.padSymbol`=61.

### Bascules et modes

| Fonction | GET | RÉPONSE | SET |
|---|---|---|---|
| `ANC_MODEL_DEFAULT` | `03 10` | `83 10` | `03 11` |
| `ANC_LEVEL` | `03 24` | `83 24` | `03 25` |
| `TRANSPARENT_LEVEL` | `03 4C` | `83 4C` | `03 4D` |
| `IN_EAR_DETECTION` | `03 0C` | `83 0C` | `03 0D` |
| `GAME_MODE` | `03 0E` | `83 0E` | `03 0F` |
| `MOVIE_MODEL` | `03 27` | `83 27` | `03 28` |
| `DOUBLE_DEVICE_CONNECT` (multipoint) | `03 14` | `83 14` | `03 15` |
| `DISABLE_ALL_TOUCHES` | `03 12` | `83 12` | `03 13` |
| `LIGHT_EFFECT_SWITCH` | `03 0A` | `83 0A` | `03 0B` |
| `PANORAMIC_SOUND_EFFECT_MODEL` | `03 29` | `83 29` | `03 30` |
| `SPACE_SOUND_EFFECT` | `03 57` | `83 57` | `03 58` |
| `DOLBY_SOUND_EFFECT` | `03 48` | `83 48` | `03 49` |
| `DYNAMIC_EQ_MODE` | `03 31` | `83 31` | `03 32` |
| `DYNAMIC_BASS` | `03 3C` | `83 3C` | `03 3D` |
| `ADAPTIVE_VOLUME` | `03 46` | `83 46` | `03 47` |
| `PERSONALIZED_VOLUME_SWITCH` | `03 40` | `83 40` | `03 41` |
| `PRIVACY_MODE` | `03 6B` | `83 6B` | `03 6C` |
| `FALL_DETECTION` | `03 55` | `83 55` | `03 56` |
| `WATER_DRAIN_FUNCTION` | `03 50` | `83 50` | `03 3E` ⚠ |
| `LDAC` | `03 38` | `83 38` | `03 39` |
| `LE_AUDIO` | `03 35` | `83 35` | `03 34` ⚠ |

### Volumes et niveaux

| Fonction | GET | RÉPONSE | SET |
|---|---|---|---|
| `PROMPT_VOLUME_LEVEL` | `03 19` | `83 19` | `03 20` |
| `LEFT_EAR_MEDIA_VOLUME_CHANGE` | `03 42` | `83 42` | `03 44` |
| `RIGHT_EAR_MEDIA_VOLUME_CHANGE` | `03 43` | `83 43` | `03 45` |

### Lecture seule

| Fonction | GET | RÉPONSE |
|---|---|---|
| `BATTERY_LEFT_GET` | `03 06` | `83 06` |
| `BATTERY_RIGHT_GET` | `03 07` | `83 07` |
| `BATTERY_BOX_GET` | `03 23` | `83 23` |
| `VERSION_GET` | `03 09` | `83 09` |
| `CONN_CLASSIC_BT` | `03 17` | `83 17` |

### Actions (commande unique)

| Fonction | COMMANDE |
|---|---|
| `RESET` | `03 05` |
| `REBOOT` | `03 16` |
| `RESET_CUSTOMIZE_TOUCH` | `03 AC` |
| `APPLY_EQUALIZER_V1` | `0E 01` |
| `APPLY_EQUALIZER_V2` | `0E 02` |

### Divers

| Fonction | GET | RÉPONSE | SET |
|---|---|---|---|
| `CUSTOM_KEY` / `CUSTOMIZE_TOUCH_DEBUG` | `03 AA` | `83 AA` | `03 AB` |
| `SWITCH_VOICE_PACKAGE` | `03 21` | `83 21` | `03 22` |
| `FIND_HEADPHONES_LEFT` | `03 4A` | `83 4A` | `03 3A` |
| `FIND_HEADPHONES_RIGHT` | `03 4B` | `83 4B` | `03 3B` |

Les variantes ANC (`_WEAK_NOISE_REDUCTION`, `_NO_TRANSPARENCY_MODE`, `_ADAPTIVE`,
`FOCUS_MODEL`) partagent `03 10`/`03 11` ; elles ne diffèrent que par la liste de modes
exposée dans l'UI selon le produit.

---

## 4. Valeurs de payload

Source : `jadx-out/sources/…/data/enums/FunctionStatusEnum.java`

**Bascules** — `CLOSE` = `00`, `OPEN` = `01`

**Modes ANC** (payload de `03 11`)

| Mode | Valeur |
|---|---|
| `ANC_NORMAL` (arrêt) | `00` |
| `ANC_ANC` | `01` |
| `ANC_TRANSPARENT` | `02` |
| `ANC_ADAPTIVE` | `03` |
| `ANC_WEAK` | `04` |
| `ANC_CHASERS` | `01` |
| `NONE` | `FF` |

**Niveaux ANC** (payload de `03 25`)

| Niveau | Valeur |
|---|---|
| `ANC_LEVEL_1` … `ANC_LEVEL_5` | `01` … `05` |
| `ANC_LEVEL_NORMAL_MODE` | `11` |
| `ANC_LEVEL_INDOOR_NOISE_REDUCTION` | `12` |
| `ANC_LEVEL_OUTDOOR_NOISE_REDUCTION` | `13` |
| `ANC_LEVEL_OUTDOOR_TRAFFIC` | `14` |
| `ANC_LEVEL_ADAPTIVE_NOISE_REDUCTION` | `15` |
| `ANC_LEVEL_ANTI_WIND_NOISE` | `16` |

**Niveaux transparence** (payload de `03 4D`) — `VOICE_ENHANCE` = `01`,
`STANDARD_TRANSPARENT` = `02`

**Son panoramique** (payload de `03 30`) — normal = `00`, musique = `01`, film = `02`

**Packs vocaux** (payload de `03 22`) — EN=`00`, ZH=`01`, DE=`02`, JA=`03`, FR=`04`,
ES=`05`, IT=`06`

---

## 5. Égaliseur

L'EQ est **paramétrique**, pas graphique. Chaque bande est un
`EqPoint { freq: Int, gain: Double, q: Double }`
(`jadx-out/sources/…/data/model/EqPoint.java`).

⚠ **Les trames EQ utilisent `vendorId = 00 1D`**, pas `00 0A`. Constaté dans
`DefaultBleDeviceInfoModel$applyEqV3$2$1.java` : `sendDataEvent(setEqCommand,
eqPointToByteArray(...), new byte[]{0, 29}, ...)`.

**Une bande = une trame.** L'app itère sur les points et envoie une trame par bande.

### Encodage d'une bande

`EqUtilKt.eqPointToByteArray(total, masterGain, index, point, isV1)` — ligne 87.

```
v1  : [ (total & 0x0F) << 4 | (index & 0x0F) ]        1 octet   → trame de 9 o.
v2/3: [ total, index ]                                 2 octets  → trame de 10 o.

puis, tous en int16 big-endian :
  masterGain × 60     (2 o.)
  freq       × 3      (2 o.)
  gain       × 60     (2 o.)
  q          × 4096   (2 o.)
```

`index` est **1-based** (l'appelant passe `index + 1`).

### Choix de version

`EqUtilKt.needInParallel(firmwareCode)` — ligne 140. Renvoie `true` (→ v1, empilé sur
l'EQ classique) si la puce est Qualcomm, ou si le firmware est dans :
`CAPSULE3_PROV2`, `CAPSULE3_PRO`, `WINGS2`, `SPACE_PRO`, `AIR5_LLITE`,
`PearClipPro_Ldac`, `"S15"`, `"S22"`. Sinon v2/v3, où la courbe personnalisée est
concaténée à la courbe classique.

⚠ Le firmware du H3 n'est pas dans cette liste → **v2/v3 attendu**, à confirmer en
lisant `VERSION_GET` (`03 09`).

---

## 6. Commandes tactiles

Payload de `03 AB`. Deux énumérations, toutes deux 1-based.

**Geste** (`CustomKeyOptionEnum`)

| Geste | Code |
|---|---|
| Clic gauche / droit | `01` / `02` |
| Double clic gauche / droit | `03` / `04` |
| Triple clic gauche / droit | `05` / `06` |
| Appui long 1,5 s gauche / droit | `07` / `08` |

**Action** (`CustomKeyFunctionEnum`)

| Action | Code |
|---|---|
| Volume + / − | `01` / `02` |
| Lecture / pause | `03` |
| Mode jeu | `04` |
| Piste précédente / suivante | `05` / `06` |
| Mode ANC | `07` |
| Assistant vocal | `08` |

⚠ `setCustomKey(byte[])` reçoit le tableau déjà construit par l'appelant.
`SET_CUSTOMIZE_TOUCH_DEMO` vaut `{0x00, 0x00}`, ce qui suggère fortement un payload
`[geste, action]` de 2 octets — **inféré, non confirmé**.

---

## 7. Confirmé sur le fil ✅

Validé le 2026-07-29 avec `probe/` sur le H3 (firmware `S5_20250605_V0.4.9`),
via **RFCOMM/SPP**. Le transport SPP est donc le bon choix : il répond.

### Transport

`sdptool browse` ne renvoie rien d'exploitable ; la découverte par balayage de canaux
fonctionne. Sur le H3 : **canaux 11 et 12 ouverts**, canal 10 occupé (`EBUSY`).
Le canal **11** répond au protocole.

⚠ **Les réponses sont intermittentes.** Une même commande peut ne rien renvoyer puis
répondre à l'essai suivant. Tout client doit implémenter un **retry** — ne pas conclure
« fonction non supportée » sur un seul échec.

### Corrections apportées à l'analyse statique

| Point | Analyse statique | Réalité mesurée |
|---|---|---|
| `version` en réponse | supposé `0x04` | **`0x03`** (la requête reste `0x04`) |
| Payloads scalaires | supposés 1 octet | **2 octets, big-endian** |

Une réponse batterie est `00 64`, pas `64`. Un mode ANC est `00 02`, pas `02`.
La valeur utile est dans le **dernier** octet.

### Relevés

| Commande | Réponse brute | Décodage |
|---|---|---|
| `03 06` batterie G | `FF 03 00 02 00 0A 83 06 00 64` | 100 % |
| `03 07` batterie D | `… 83 07 00 64` | 100 % |
| `03 23` batterie étui | `… 83 23 00 5F` | 95 % |
| `03 09` firmware | `… 83 09 00` + ASCII | `S5_20250605_V0.4.9` |
| `03 10` mode ANC | `… 83 10 00 02` | `02` = transparence |
| `03 38` LDAC | `… 83 38 00 01` | activé |
| `03 35` LE Audio | `… 83 35 00 00` | désactivé |
| `03 0E` mode jeu | `… 83 0E 00 00` | désactivé |
| `03 14` multipoint | `… 83 14 00 00` | désactivé |

**Validation croisée** : `03 38` renvoie « LDAC activé », ce qui concorde avec le codec
LDAC effectivement négocié en A2DP (`api.bluez5.codec = "ldac"`, transport BlueZ
`Configuration` = Sony `0x012D` / codec `0x00AA`). Deux voies indépendantes, même
conclusion → le décodage est fiable.

`03 0C` (détection intra-auriculaire) n'a jamais répondu en 3 essais — seule fonction
qui semble réellement absente du H3.

### Réponses composées

**`83 AA` commandes tactiles** — `00 | 0102 0201 0303 0403 0504 0608 0707 0806`

8 paires `[geste, action]` après un octet de tête. **Ceci confirme le format
`[geste, action]` qui n'était qu'une inférence en §6.**

| Geste | Code | Action | Code |
|---|---|---|---|
| Clic gauche | `01` | Volume − | `02` |
| Clic droit | `02` | Volume + | `01` |
| Double clic gauche | `03` | Lecture/pause | `03` |
| Double clic droit | `04` | Lecture/pause | `03` |
| Triple clic gauche | `05` | Mode jeu | `04` |
| Triple clic droit | `06` | Assistant vocal | `08` |
| Appui long gauche | `07` | Mode ANC | `07` |
| Appui long droit | `08` | Piste suivante | `06` |

**`83 24` niveau ANC** — `0011 0012 0113 0014 00`

Paires `[actif, code]` : `01 13` → mode actif = `ANC_LEVEL_OUTDOOR_NOISE_REDUCTION`.
Les codes `11`,`12`,`13`,`14` correspondent bien aux modes de scène de §4.

**`83 4C` niveau transparence** — `00 01 00 02 01` (structure non élucidée).

### Écriture ✅

Premier `set` validé sur le fil (mode ANC, `02` → `01`) :

```
→ FF 04 00 01 00 0A 03 11 01      (set, payload 1 octet)
← FF 03 00 02 00 0A 83 10 00 01   (ack, payload 2 octets)
```

Deux asymétries à retenir pour l'implémentation :

1. **Le payload d'écriture fait 1 octet**, alors que les réponses en renvoient 2.
   L'app envoie bien `FunctionStatusEnum.getStatus()` = `new byte[]{1}`.
2. **L'acquittement utilise l'opcode de réponse du `get`** (`83 10`), pas `83 11`.
   Un `set` est donc confirmé par une notification d'état, pas par un écho de la
   commande d'écriture. Un client doit attendre `83 <get>`, pas `83 <set>`.

Relecture après écriture : `83 10 00 01` — cohérent. Validation humaine : invite
vocale « ANC MODE ON » et réduction de bruit effectivement audible.

### ⚠ Ne JAMAIS réessayer une écriture

Plusieurs `set` **prennent effet sans acquitter** : mode ANC, sous-mode ANC,
multipoint. Un client qui interprète l'absence de réponse comme un échec et renvoie
la commande **rejoue l'effet de bord**.

Observé en conditions réelles : une écriture `03 11 02` réémise 3 fois par la logique
de retry a déclenché **trois invites vocales « passthrough »** pour un seul changement
voulu.

**Règle** : réessayer librement les `get`, jamais les `set`. Pour vérifier qu'une
écriture a pris, **relire l'état** — ne pas renvoyer la commande.

### Réponses en liste : `en-tête | paires (code, actif)`

Structure commune à `83 24`, `83 4C` et `83 AA` : un octet de tête, puis des paires.

Confirmé par écriture : `03 25 = 0x14` déplace le drapeau `01` sur la paire du code
`14`, et `03 25 = 0x12` le ramène sur `12`.

```
avant : 00 | 11 00 | 12 01 | 13 00 | 14 00   → actif = 12 (intérieur)
après : 00 | 11 00 | 12 00 | 13 00 | 14 01   → actif = 14 (trafic)
```

Les 4 sous-modes ANC `11`/`12`/`13`/`14` correspondent exactement aux 4 entrées de
l'app Android : *annulation adaptative*, *à l'intérieur*, *bruits extérieurs*,
*mode trafic* (confirmé par l'utilisateur sur l'UI officielle).

`83 4C` se décode pareil : `00 | 01 00 | 02 01` → 2 niveaux de transparence,
actif = `02` (transparence standard).

### Égaliseur ✅

**Validé sur le fil, audible.** Une bande +6 dB à 100 Hz produit un renforcement des
basses nettement perceptible ; le retour à 0 dB le supprime.

```
→ FF 04 00 0A 00 1D 0E 02 01 01 0000 012C 0168 1000
                    └vendorId EQ  └cmd  └total,index, mg×60, f×3, g×60, q×4096
← FF 03 00 01 00 1D 0F 82 01
```

⚠ **La famille EQ ne suit pas la convention `réponse = requête | 0x80`.**
Requête `0E 02` → réponse **`0F 82`** (1er octet +1, 2e octet avec bit 7 armé).
Payload `01` = succès. Un client qui attendrait `8E 02` resterait bloqué.

L'encodage de §5 est donc correct tel quel, y compris les facteurs d'échelle
`freq × 3`, `gain × 60`, `q × 4096`.

### Multipoint : effet de bord sur le canal de contrôle

Activer le multipoint (`03 15 = 01`) fonctionne, mais provoque une reconfiguration des
liens :

1. l'écriture n'acquitte pas ;
2. le canal RFCOMM devient brièvement `EBUSY` ;
3. si un second appareil se connecte, **il monopolise le canal de contrôle SPP** et
   tous les canaux disparaissent du scan.

Constaté : une fois le multipoint actif, le téléphone (app PeatsAudio ouverte) a repris
le lien et les canaux 11/12 ont disparu ; ils sont revenus après extinction du
Bluetooth du téléphone. Un client desktop doit prévoir ce cas et le signaler à
l'utilisateur plutôt que de conclure à une panne.

### ⚠ LDAC et multipoint sont mutuellement exclusifs

Confirmé sur le fil. État initial : `03 38 = 01` (LDAC), `03 14 = 00` (multipoint off),
codec A2DP négocié = **LDAC**.

Après `03 15 = 01` (activation multipoint), **sans aucune commande LDAC de ma part** :

```
03 38  →  00 00   LDAC désactivé automatiquement par le firmware
03 14  →  00 01   multipoint activé
```

Et le codec A2DP renégocié par PipeWire retombe sur **aptX**
(`api.bluez5.codec = "aptx"`).

Le firmware arbitre seul : la bande passante ne permet pas du LDAC 990 kbps sur deux
liens simultanés. Une UI honnête doit présenter ces deux réglages comme un choix
exclusif et expliquer la conséquence audio, plutôt que comme deux bascules
indépendantes.

### Canal RFCOMM 12

Second point d'entrée SPP, **protocole identique** — `03 06`, `03 10`, `03 09` y
répondent exactement comme sur le canal 11. Utilisable en repli.

### Matrice de support du H3

16 commandes sur 31 répondent de façon stable. Les non-réponses sont reproductibles
sur 3 essais avec délai de stabilisation : ce sont de vraies fonctions absentes.

**Supportées** — batteries L/R/étui, firmware, mode ANC, sous-mode ANC, niveau
transparence, mode jeu, multipoint, LDAC, LE Audio, commandes tactiles, désactivation
tactile, volume des invites (`00 32` = 50), pack vocal (`00 00` = anglais), connexion
BT classique, EQ.

**Absentes du H3** — détection intra-auriculaire, effet lumineux, mode cinéma, son
panoramique, EQ dynamique, basses dynamiques, Dolby, effet spatial, volume par oreille,
volume personnalisé, volume adaptatif, évacuation d'eau, détection de chute, mode privé.

### Intermittence : résolue

Cause identifiée : les commandes émises trop tôt après l'établissement du lien RFCOMM
restent sans réponse. **Un délai de ~600 ms après connexion** avant la première
émission, plus une réutilisation de la connexion pour les commandes suivantes, rend les
lectures fiables (16/16 des fonctions supportées lors du sweep complet).

### Conséquence pour l'EQ

Firmware `S5` : absent de la liste de `needInParallel()` (qui contient `S15`, `S22`…)
→ **EQ v2/v3**, comme inféré en §5. Reste à valider en écriture.

---

## 8. Ce qui reste incertain

Tous les points majeurs sont clos. Reste :

1. ~~Trames validées sur le fil.~~ ✅ lecture **et** écriture.
2. `setCustomKey` : format `[geste, action]` confirmé **en lecture** ;
   **écriture jamais testée** — seul point fonctionnel encore ouvert.
3. ~~Le H3 répond-il sur SPP ?~~ ✅ canaux 11 et 12.
4. ~~Version d'EQ acceptée.~~ ✅ v2/v3, validé et audible.
5. ~~Canal RFCOMM 12.~~ ✅ identique au 11.
6. ~~Structure de `83 4C`.~~ ✅ paires (code, actif).
7. ~~Cause de l'intermittence.~~ ✅ délai de stabilisation après connexion.
8. Sémantique de `flags` (toujours `00` dans tous les appels observés).
9. EQ multi-bandes : une seule bande testée. L'app itère sur N bandes ; le
   comportement avec `total > 1` et l'ordre des `index` n'est pas vérifié.
10. `masterGain` : toujours envoyé à 0 dans les tests ; effet non observé.
11. `handleData()` route vers `sceneLevelHandle()` pour certains payloads — non analysé.
12. Aucun test de `RESET` (`03 05`) ni `REBOOT` (`03 16`) — volontairement évités.
