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
