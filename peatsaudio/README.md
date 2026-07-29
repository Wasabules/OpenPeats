# PeatsAudio — contrôle SoundPEATS sous Linux

Application de bureau (Wails + Go + Svelte) qui pilote des écouteurs SoundPEATS
via le protocole propriétaire reconstitué dans [`../PROTOCOL.md`](../PROTOCOL.md).

Développée et validée sur un **SOUNDPEATS H3**, firmware `S5_20250605_V0.4.9`,
sous Ubuntu 24.04.

## Compilation

Ubuntu 24.04 ne fournit plus `webkit2gtk-4.0`. Le tag de build est obligatoire :

```bash
wails build -tags webkit2_41
./build/bin/peatsaudio
```

En développement : `wails dev -tags webkit2_41`.

## Tests

```bash
go test ./internal/peats -v                       # unitaires, sans matériel
PEATS_MAC=98:80:BB:40:D4:ED go test ./internal/peats -run TestLive -v
```

Les tests unitaires décodent des trames **réellement capturées** sur le H3 :
réponse batterie, sous-mode ANC, mapping tactile, encodage d'une bande d'EQ.

## Architecture

```
internal/peats/protocol.go   trames, opcodes, énumérations, encodage EQ
internal/peats/client.go     transport RFCOMM/SPP, retry, découverte de canal
app.go                       état + méthodes exposées au frontend
frontend/src/                interface Svelte
```

## Pièges du protocole (appris à la dure)

Ces points ne sont pas des détails d'implémentation : ils cassent silencieusement
tout client qui les ignore.

**Ne jamais réessayer une écriture.** Plusieurs `set` prennent effet *sans*
acquitter (mode ANC, multipoint). Interpréter l'absence de réponse comme un échec
et renvoyer la commande rejoue l'effet de bord — constaté avec trois invites
vocales pour un seul changement voulu. Pour vérifier qu'un `set` a pris :
**relire l'état**.

**L'acquittement d'un `set` porte l'opcode de réponse du `get`.** Écrire `03 11`
donne une réponse `83 10`, pas `83 11`. Attendre `83 11` bloque indéfiniment.

**L'EQ suit une convention différente.** `0E 02` répond `0F 82` — ce n'est pas la
règle « bit 7 sur le premier octet » du reste du protocole. Et l'EQ utilise
`vendorId 001D` au lieu de `000A`.

**Les payloads sont asymétriques.** Les réponses de lecture encodent les
scalaires sur 2 octets big-endian (`00 64` = 100) ; les écritures prennent
1 octet.

**Délai de stabilisation obligatoire.** Une commande émise moins de ~600 ms après
l'établissement du lien RFCOMM reste sans réponse. Sans ce délai, les lectures
semblent aléatoires.

**Ne pas fermer puis rouvrir un canal.** Les écouteurs refusent une reconnexion
immédiate sur le même canal (`EBUSY`). La découverte de canal conserve donc le
socket de la première connexion réussie au lieu de le refermer.

**LDAC et multipoint sont exclusifs.** Le firmware coupe LDAC de lui-même quand le
multipoint s'active — la bande passante ne suffit pas pour deux liens. L'UI les
présente comme un choix, pas comme deux bascules indépendantes.

**Le multipoint peut confisquer le canal de contrôle.** Une fois le multipoint
actif, le second appareil (téléphone avec l'app officielle ouverte) monopolise le
SPP et tous les canaux disparaissent du scan. L'application le détecte et
l'explique au lieu de signaler une panne.

## Pièges propres à Windows

**`SOCKADDR_BTH` est packé sur 1 octet.** `ws2bth.h` inclut `pshpack1.h` : la
structure fait 30 octets et `btAddr` commence à l'offset 2, juste après
`addressFamily`. Une struct Go équivalente serait alignée naturellement et
placerait `btAddr` à l'offset 8 — Windows lit alors l'adresse au mauvais endroit
et rejette tout `connect` par `WSAEADDRNOTAVAIL` (10049), sur tous les canaux et
quel que soit le mode d'adressage. C'est un échec muet et total, qui ressemble à
un appareil absent. Le transport sérialise donc la structure à la main.

**L'échec d'un `connect` non bloquant passe par `exceptfds`.** Sous POSIX, le
noyau rend le socket inscriptible dans les deux cas, donc surveiller `writefds`
suffit ; Winsock, lui, ne signale l'échec que dans `exceptfds`. Ne surveiller que
`writefds` fait attendre le délai complet à chaque canal fermé — 90 s pour un
balayage — avant de conclure à tort à un dépassement de délai.

**Codes d'erreur, mesurés sur un H3 joignable.** `WSAEHOSTDOWN` (10064) désigne
un canal fermé, pas un appareil absent : les canaux 1 et 12 le renvoient pendant
que le canal 11 accepte la connexion. `WSAEADDRINUSE` (10048) est l'équivalent de
l'`EBUSY` de Linux — même canal 10 occupé. `WSAEADDRNOTAVAIL` (10049) sur *tous*
les canaux signifie que l'appareil est appairé mais pas connecté : « appairé » ne
suffit pas, Windows doit avoir établi le lien.

**La notification de batterie faible ne fonctionne pas.** `notifyLowBattery`
appelle `notify-send`, absent de Windows ; l'alerte reste visible dans
l'application seulement.

## Statut de vérification

Tout est confirmé sur le fil **sauf** ce qui est marqué « non vérifié » dans
l'interface :

- **écriture** du mapping tactile (la lecture, elle, est validée) ;
- **mode debug tactile** (`CUSTOMIZE_TOUCH_DEMO`), déduit de l'app Android.

Le reste — ANC, sous-modes, transparence, batteries, firmware, LDAC, multipoint,
LE Audio, mode jeu, désactivation tactile, invites vocales, égaliseur — a été
exercé en lecture et, quand c'était applicable, en écriture avec vérification
audible.

## Fonctions absentes du H3

Détectées automatiquement au rafraîchissement et grisées dans l'interface :
détection intra-auriculaire, effet lumineux, mode cinéma, son panoramique, EQ
dynamique, basses dynamiques, Dolby, effet spatial, volume par oreille, volume
personnalisé, volume adaptatif, évacuation d'eau, détection de chute, mode privé.

D'autres modèles SoundPEATS partageant ce protocole devraient les exposer :
l'application interroge chaque fonction et n'affiche que ce qui répond.
