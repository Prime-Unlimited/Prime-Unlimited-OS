# Neue Mod veröffentlichen

Du legst die fertige Mod ab und pushst auf `feat/prime-unlimited-mod-loader`.
GitHub prüft, lädt die ZIP hoch, schreibt den Katalog und signiert ihn.
Die einzige weitere Aktion ist die Freigabe durch einen Repository-Admin.

Development heißt: auf dem Gerät nur im Developer Mode sichtbar und installierbar.
Ready heißt: für alle kompatiblen Geräte sichtbar. Ready ist keine Hardwareprüfung.

Branch für jeden Push und für jeden Workflow: `feat/prime-unlimited-mod-loader`.

## 1. Was niemand von Hand ändert

Diese Dateien schreibt nur der Workflow:

- `modloader/catalog/jc11s.json`
- `modloader/catalog/jc11s.sig.json`
- `modloader/catalog/jp24.json`
- `modloader/catalog/jp24.sig.json`
- `mods/development/<mod-id>/<version>/release.json`
- `mods/ready/<mod-id>/<version>/promotion.json`

Nicht committen:

- den Signierschlüssel
- einen neuen Schlüssel
- einen Katalog ohne die passende `.sig.json`

Eine schon veröffentlichte Version darf keine andere ZIP bekommen.
Eine Korrektur ist eine neue Versionsnummer.

## 2. Verzeichnisse

Quellcode, wenn du die ZIP im Repository baust. Das ist die Bauform von Prime Clock.
Die Veröffentlichung liest dieses Verzeichnis nicht.

```
mods/<mod-id>/
  VERSION                         eine Zeile, zum Beispiel 0.1.0
  README.md
  scripts/package.py              baut die ZIP
  src/entrypoint.sh               wird in der ZIP zu <mod-id>/start.sh
  src/ui/<Name>.qml               wird in der ZIP zu <mod-id>/ui/<Name>.qml
  src/platform/jc11s.json         nur JC11S, falls die Geräte sich unterscheiden
  src/platform/jp24.json          nur JP24
```

Das Verzeichnis, das die Pipeline veröffentlicht. Neu, bevor ein Admin freigibt:

```
mods/development/<mod-id>/<version>/
  mod.json                        du schreibst das
  jc11s.zip                       du legst sie ab, wenn JC11S die Mod bekommt
  jp24.zip                        du legst sie ab, wenn JP24 die Mod bekommt
  release.json                    entsteht nach dem grünen Development-Lauf
```

Eine Mod darf nur JC11S, nur JP24 oder beide Geräte haben.
Die ZIP, die du nicht brauchst, lässt du weg.

Nach der Admin-Freigabe liegt dieselbe Version hier. Der Workflow verschiebt sie.
Du legst hier nichts selbst ab.

```
mods/ready/<mod-id>/<version>/
  mod.json
  jc11s.zip                       nur wenn dieses Gerät Ziel ist
  jp24.zip                        nur wenn dieses Gerät Ziel ist
  release.json
  promotion.json                  Nachweis der Freigabe
```

Kataloge, die das Gerät lädt:

```
modloader/catalog/jc11s.json
modloader/catalog/jc11s.sig.json
modloader/catalog/jp24.json
modloader/catalog/jp24.sig.json
modloader/catalog/trust/prime-unlimited-2026-1.json
```

Die Trust-Datei ist der öffentliche Schlüssel und bleibt `prime-unlimited-2026-1`.

Auf dem JC11S-Gerät:

```
/data/prime-unlimited/catalog/catalog.json
/data/prime-unlimited/catalog/catalog.sig.json
/data/prime-unlimited/auth/developer.token     einmal anlegen, nur für Development-Tests
/data/prime-unlimited/auth/developer.mode      setzt die Runtime, nicht von Hand
```

Der Modloader holt den JC11S-Katalog von:

`https://raw.githubusercontent.com/Prime-Unlimited/Prime-Unlimited-OS/feat/prime-unlimited-mod-loader/modloader/catalog/jc11s.json`

## 3. Einmalige Einrichtung

Das ist schon eingerichtet. Nur wiederholen, wenn ein Environment oder das Secret fehlt.

1. GitHub-Environment `mod-sign`.
   Deployment branch nur `feat/prime-unlimited-mod-loader`.
   Keine Required reviewers.
   Secret `CATALOG_ED25519_PRIVATE`: der bestehende Schlüssel `prime-unlimited-2026-1` als Hex.
   Nicht als Repository-Secret. Keinen neuen Schlüssel erzeugen.
2. GitHub-Environment `mod-release`.
   Derselbe Branch.
   Required reviewers: alle Repository-Admins, derzeit `Goaloge` und `xxbluecyxx-byte`.
   Eine Freigabe von einem der beiden reicht.
   Dasselbe Secret `CATALOG_ED25519_PRIVATE`.
3. Developer-Token nur auf dem Gerät, nur wenn Development-Mods getestet werden:

```sh
umask 077
openssl rand -hex 32 > /data/prime-unlimited/auth/developer.token
```

Die Datei ist 64 Hex-Zeichen und Modus `0600`. Die API erzeugt sie nicht.
Das Mutation-Token schaltet den Developer Mode nicht ein.

## 4. ZIP bauen

Die Datei im Repository heißt `jc11s.zip` oder `jp24.zip`.
Der Name innen bei GitHub wird später `<mod-id>-v<version>-jc11s.zip` beziehungsweise `...-jp24.zip`.
Den GitHub-Namen setzt der Workflow.

In der ZIP müssen die Pfade relativ sein. Kein führendes `/`, kein `..`, keine doppelten Pfade, kein verschlüsselter Eintrag.

Pflicht in jeder ZIP:

- der Entrypoint aus `mod.json`, zum Beispiel `example/start.sh`

Üblich für eine Overlay-Mod:

- `example/start.sh`
- `example/ui/Example.qml`
- weitere Dateien, die der Entrypoint braucht

Nur für eine native Service-Mod, zusätzlich pro Gerät eine ELF-Datei:

- JC11S: 32-bit ARM, `armv7l` / `gnueabihf`
- JP24: 64-bit ARM, `aarch64` / `gnu`

Die aktuelle Geräte-Firmware ist `5.3.0`.

SHA-256 der ZIP merken. Dieselbe Summe kommt später in die Freigabe:

```sh
sha256sum jc11s.zip jp24.zip
```

## 5. mod.json

Pfad: `mods/development/<mod-id>/<version>/mod.json`.

`id` ist der Ordner `<mod-id>`. `version` ist der Ordner `<version>`.
Beide müssen exakt übereinstimmen.
`id` passt auf `^[a-z0-9][a-z0-9._-]{0,63}$`.
Die Katalog-ID wird fest `<mod-id>-jc11s` und `<mod-id>-jp24`. Nicht selbst vergeben.

Beispiel für beide Geräte:

```json
{
  "id": "example",
  "name": "Example",
  "version": "0.1.0",
  "revision": 1,
  "firmware": ["5.3.0"],
  "entrypoint": "example/start.sh",
  "overlay": "example/ui/Example.qml",
  "hardwareVerification": "not-performed",
  "approval": {
    "maintainer": "Prime Unlimited",
    "devices": ["JC11S", "JP24"]
  },
  "dependencies": [],
  "conflicts": [],
  "targets": {
    "jc11s": {
      "description": "Was die Mod auf JC11S tut."
    },
    "jp24": {
      "description": "Was die Mod auf JP24 tut."
    }
  }
}
```

Nur JC11S: `targets` enthält nur `jc11s`, `approval.devices` ist `["JC11S"]`, und es gibt nur `jc11s.zip`.

Felder:

| Feld | Inhalt |
| --- | --- |
| `id`, `version` | identisch zu den Ordnernamen |
| `name` | Anzeigename |
| `revision` | ganze Zahl, bei einer neuen Version `1`; dieselbe Version behält dieselbe Revision |
| `firmware` | exakte Engine-OS-Liste, aktuell `["5.3.0"]` |
| `entrypoint` | Pfad in der ZIP |
| `overlay` | optionaler QML-Pfad in der ZIP |
| `hardwareVerification` | `not-performed` oder `recorded-separately` |
| `approval.maintainer` | `Prime Unlimited` |
| `approval.devices` | `JC11S`, `JP24` oder beide, passend zu den ZIPs |
| `dependencies`, `conflicts` | Listen, sonst `[]` |
| `targets.<gerät>.description` | Text im Katalog für dieses Gerät |
| `targets.<gerät>.service` | nur bei einem nativen Dienst |

Service-Eintrag, SHA-256 ist die ELF-Datei in der ZIP, nicht die ZIP selbst:

```json
"service": {
  "binary": "example/bin/server-armhf",
  "sha256": "<64 Hex-Zeichen der ELF>",
  "capability": "splash-image"
}
```

`capability` ist derzeit nur `splash-image`. Mehrere Service-Mods dürfen gleichzeitig aktiv sein.
JC11S-Binary und JP24-Binary haben verschiedene Pfade und verschiedene SHA-256.
Architektur und ABI schreibt der Workflow aus der Gerätezuordnung. Nicht in `mod.json` setzen.

| Datei | Modell | Architektur | ABI | Katalog-ID |
| --- | --- | --- | --- | --- |
| `jc11s.zip` | JC11S | armv7l | gnueabihf | `<mod-id>-jc11s` |
| `jp24.zip` | JP24 | aarch64 | gnu | `<mod-id>-jp24` |

## 6. Development veröffentlichen

1. Commit nur `mods/development/<mod-id>/<version>/mod.json` und die ZIP-Dateien.
2. Push auf `feat/prime-unlimited-mod-loader`.
3. Actions-Workflow `Mod development` abwarten.
   Datei: `.github/workflows/mod-development.yml`.
   Ein roter Lauf veröffentlicht keinen Katalog. Ursache in `mod.json` oder der ZIP beheben und erneut pushen, oder den Workflow mit `workflow_dispatch` auf demselben Branch wiederholen.

Der grüne Lauf macht danach von allein:

1. Mod-ID, Version, Firmware, ZIP-Pfade, Entrypoint und gegebenenfalls den nativen Dienst prüfen.
2. SHA-256 und Dateigröße berechnen.
3. Pro Gerät ein unveränderliches Release-Asset hochladen.
   Tag: `mod-<mod-id>-v<version>`.
   Asset-Name: `<mod-id>-v<version>-jc11s.zip` oder `...-jp24.zip`.
4. `release.json` mit Tag, Asset-ID, Name, SHA-256 und Größe schreiben.
5. `modloader/catalog/jc11s.json` und `jp24.json` neu erzeugen.
   Der Eintrag bekommt `"status": "development"`.
   Andere Mods bleiben stehen.
6. Beide Kataloge mit `prime-unlimited-2026-1` signieren.
7. Katalog und Signatur in einem Commit auf denselben Branch schieben.

Die Download-URL im Katalog ist:

`https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/<asset-id>`

## 7. Auf dem Gerät testen

1. Modloader öffnen.
2. `DEV MODE` drücken und das Developer-Token aus `/data/prime-unlimited/auth/developer.token` eingeben.
3. `EINSCHALTEN`.
4. Die Mod erscheint mit dem Zusatz `DEVELOPMENT`.
5. Installieren und prüfen.
6. Wenn der Katalog noch der alte ist: im Modloader `SYNC CATALOG`.

Developer Mode aus: die Mod ist in der Liste weg und über die API nicht installierbar.
Die Meldung ist `mod is installable only in developer mode`.

Weitere Meldungen, wenn die Ursache bekannt ist:

- `download URL is not supported`
- `HTTP redirect blocked`
- `release asset unreachable`
- `package size mismatch`
- `package SHA-256 mismatch`
- `ZIP is invalid`
- `architecture is not compatible`
- `firmware is not supported`
- `entrypoint is missing`
- `native service file is invalid`
- `catalog signature verification failed`

Eine ungültige Signatur installiert nichts Neues. Der letzte gültige Katalog bleibt liegen.

## 8. Admin-Freigabe nach Ready

1. Die SHA-256-Werte der gepushten ZIPs nehmen. Es sind dieselben Werte wie in `release.json`.
2. Actions, Workflow `Mod promotion`, Branch `feat/prime-unlimited-mod-loader`, Run workflow.
   Datei: `.github/workflows/mod-promote.yml`.
3. Eingaben:
   - `mod`: die Mod-ID, zum Beispiel `example`
   - `version`: der Versionsordner, zum Beispiel `0.1.0`
   - `jc11s_sha256`: SHA-256 von `jc11s.zip`, oder leer wenn es keine JC11S-ZIP gibt
   - `jp24_sha256`: SHA-256 von `jp24.zip`, oder leer wenn es keine JP24-ZIP gibt
4. Ein Repository-Admin gibt die Environment `mod-release` frei.
   Das sind `Goaloge` und `xxbluecyxx-byte`. Eine Freigabe reicht.

Der Workflow bricht ab, wenn die Summe nicht zur Datei passt oder `release.json` noch keine Asset-ID hat.
Damit kann keine später veränderte ZIP unter derselben Version herausgehen.

Nach der Freigabe macht der Workflow von allein:

1. Verschiebt `mods/development/<mod-id>/<version>/` nach `mods/ready/<mod-id>/<version>/`.
2. Schreibt `promotion.json` mit `confirmedBy` `github-environment:mod-release`, der Workflow-Run-ID und den Asset-IDs.
3. Nimmt `status` und `release` aus dem Ready-Eintrag. Ein fehlendes `status` bedeutet ready.
   Revision 10 auf dem JP24 kann den Katalog dann laden. Ein Development-Eintrag behält beide Felder.
4. Signiert beide Kataloge neu und committet sie.
5. Die Mod-ID, Version, Revision, SHA-256 und Asset-ID bleiben gleich.
   Eine schon installierte Development-Fassung bleibt dieselbe Installation.

Danach im Modloader `SYNC CATALOG`. Die Mod erscheint ohne Developer Mode.

`hardwareVerification` bleibt, was in `mod.json` steht.
`not-performed` heißt: noch nicht auf der Hardware geprüft.
`recorded-separately` heißt: die Hardwareprüfung steht in einem anderen Dokument, nicht in diesem Katalog.

## 9. Wenn etwas schiefgeht

- Development-Lauf rot: der produktive Katalog auf dem Branch ist unverändert. Dateien korrigieren und neu pushen.
- Freigabe rot: die Mod bleibt unter `mods/development/`. Nichts liegt halb unter `mods/ready/`, solange der Lauf keinen Commit erzeugt hat.
- Dieselbe Version mit neuer ZIP: abgelehnt. Neue Version unter `mods/development/<mod-id>/<neue-version>/` anlegen.
- Direkter Commit nach `mods/ready/` ohne `promotion.json`: die Prüfung lehnt ihn ab.
- Zwei Läufe gleichzeitig: sie teilen sich die Gruppe `prime-unlimited-mod-catalog` und schreiben den Katalog nacheinander.

Quelltext und Overlay-Regeln stehen in `modloader/OVERLAY_MOD_DEVELOPMENT.md` und `modloader/SERVICE_MOD_DEVELOPMENT.md`.
Die Pipeline im Detail steht in `docs/MOD_RELEASE_PIPELINE.md`.
Vorlage für eine gebaute Mod: `mods/prime-clock/`.
Fertige veröffentlichte Beispiele: `mods/ready/prime-clock/0.1.2/` und `mods/ready/splash-manager/1.1.2/`.
