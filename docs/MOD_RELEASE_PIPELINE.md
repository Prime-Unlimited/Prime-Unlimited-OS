# Mod-Release-Pipeline

Neue Mods werden nur unter `mods/development/<mod-id>/<version>/` abgelegt. Danach prüft, veröffentlicht und signiert GitHub Actions den Katalog. Die einzige weitere manuelle Aktion ist die Administratorbestätigung, die eine Version nach `mods/ready/` übernimmt.

Development heißt testbar im Developer Mode. Ready heißt administrativ freigegeben. Beides ist keine Hardwareprüfung. Die Hardwareprüfung steht separat in `hardwareVerification` (`not-performed` oder `recorded-separately`).

## 1. Development

1. Verzeichnis anlegen: `mods/development/<mod-id>/<version>/`.
2. `mod.json` und mindestens eine ZIP ablegen: `jc11s.zip`, `jp24.zip` oder beide.
3. Den Push auf `feat/prime-unlimited-mod-loader` machen.
4. Der Workflow `.github/workflows/mod-development.yml` startet automatisch. Ein fehlgeschlagener Lauf kann mit `workflow_dispatch` wiederholt werden.

Der Workflow erkennt die Version, prüft `mod.json`, Version, Mod-ID, Geräte, Firmware, ZIP-Pfade, Architektur, ABI, Entrypoint und native Dienste, berechnet SHA-256 und Größe, lädt pro Gerät ein unveränderliches Release-Asset hoch und schreibt die Asset-ID in `release.json`. Die Download-URL hat diese Form:

`https://api.github.com/repos/Prime-Unlimited/Prime-Unlimited-OS/releases/assets/<id>`

Dieselbe Version mit anderem ZIP-Inhalt wird abgelehnt. Ein bereits vorhandenes Asset mit demselben Digest wird wiederverwendet.

Kataloge:

- `modloader/catalog/jc11s.json`
- `modloader/catalog/jc11s.sig.json`
- `modloader/catalog/jp24.json`
- `modloader/catalog/jp24.sig.json`

Einträge, die nicht zur Änderung gehören, bleiben erhalten. Der Status im Katalog ist `development`. Die Signatur wird geprüft, bevor eine Katalogdatei ersetzt wird. Schlägt die Prüfung fehl, bleibt der bisherige Katalog stehen und es gibt keinen Commit.

Pull Requests validieren nur. Sie bekommen keinen Signierschlüssel und schreiben keine Releases.

## 2. mod.json

Pflichtfelder:

- `id`: muss dem Verzeichnisnamen entsprechen, Muster `^[a-z0-9][a-z0-9._-]{0,63}$`
- `name`, `version`, `revision`
- `firmware`: Liste exakter Engine-OS-Versionen, zum Beispiel `["5.3.0"]`
- `entrypoint`: relativer Pfad in der ZIP, ohne `..`
- `approval.maintainer` und `approval.devices`
- `targets`: Objekt mit `jc11s` und/oder `jp24`

Die Katalog-ID eines Ziels ist fest `<mod-id>-<gerät>`, also `prime-clock-jc11s`. Architektur und ABI kommen aus der Gerätezuordnung, nicht aus der ZIP:

| Gerät | Modell | Architektur | ABI |
| --- | --- | --- | --- |
| jc11s | JC11S | armv7l | gnueabihf |
| jp24 | JP24 | aarch64 | gnu |

Optionale Felder: `description`, `overlay`, `dependencies`, `conflicts`, `hardwareVerification`, und pro Ziel `service` mit `binary`, `sha256`, `capability`. `capability` ist derzeit `splash-image`.

`release.json` und `promotion.json` schreibt die Pipeline. Sie werden nicht von Hand gepflegt, außer bei der einmaligen Übernahme der bereits signierten Versionen Prime Clock 0.1.2 und Splash Manager 1.1.2.

## 3. Developer Mode

Der Modloader filtert serverseitig.

- Aus: `/api/v1/mods` enthält keine Development-Mods. `cache`, `stage`, `inspect`, `install` und `update` antworten mit `mod is installable only in developer mode`.
- An: Development-Mods werden zusätzlich geliefert und in der Oberfläche mit `DEVELOPMENT` gekennzeichnet. Installationsfehler nennen die konkrete Ursache.

Einschalten kann nur ein lokales Token. Die Datei `/data/prime-unlimited/auth/developer.token` wird nicht von der API erzeugt. Einmal auf dem Gerät:

```sh
umask 077
openssl rand -hex 32 > /data/prime-unlimited/auth/developer.token
```

Die Oberfläche fragt dieses Token ab und sendet es als `X-Prime-Developer-Token` zusammen mit dem bestehenden Mutation-Token. Ein Aufruf nur mit dem Mutation-Token oder ganz ohne Token ändert den Modus nicht. Der Zustand liegt in `/data/prime-unlimited/auth/developer.mode` und wird bei jedem Request gelesen.

## 4. Freigabe nach Ready

1. Im Development-Lauf die SHA-256-Werte der ZIPs notieren.
2. Workflow `Mod promotion` manuell starten.
3. Mod-ID, Version und die SHA-256 der gemeinten ZIPs eintragen.
4. Die GitHub-Environment `mod-release` muss die Ausführung freigeben.

Die Environment ist die Administratorbestätigung. Vor dem ersten Development-Lauf und vor der ersten Freigabe einrichten. Den Signierschlüssel nicht als Repository-Secret ablegen. Ein Repository-Secret wäre für jeden Workflow im Repository lesbar.

1. Environment `mod-sign` anlegen. Deployment branch nur `feat/prime-unlimited-mod-loader`. Keine Required reviewers. Secret `CATALOG_ED25519_PRIVATE`: der bestehende Schlüssel `prime-unlimited-2026-1`, Hex der 32-Byte-Seed oder der 64-Byte-Go-Schlüssel. Keinen neuen Schlüssel erzeugen.
2. Environment `mod-release` anlegen. Deployment branch nur `feat/prime-unlimited-mod-loader`. Required reviewers setzen. Dasselbe Secret `CATALOG_ED25519_PRIVATE` auch hier hinterlegen.
3. Der Workflow liest die Environment-Regeln und bricht ab, wenn der Branch nicht exakt gesperrt ist oder `mod-release` keine Reviewer hat. Er umgeht die Prüfung nicht.

Der Signierlauf bricht zusätzlich ab, wenn der öffentliche Schlüssel nicht zu `modloader/catalog/trust/prime-unlimited-2026-1.json` passt.

Nach der Bestätigung prüft der Workflow die SHA-256 gegen die Dateien, verschiebt genau diese Version nach `mods/ready/`, schreibt `promotion.json` mit `confirmedBy: github-environment:mod-release` und der Workflow-Run-ID, erzeugt beide Kataloge neu, signiert sie und committet Katalog und Signatur zusammen. Die Mod-ID, Version, Revision und der ZIP-Digest bleiben gleich. Eine bereits installierte Development-Fassung bleibt damit dieselbe Installation.

Ein direkter Commit nach `mods/ready/` ohne gültige `promotion.json` fällt in der Validierung durch.

## 5. Signierung

Die Vertrauenskette bleibt `prime-unlimited-2026-1`. Private Schlüssel liegen nicht im Repository. Signieren dürfen nur die beiden Workflows auf `Prime-Unlimited/Prime-Unlimited-OS`, und nur außerhalb von Pull Requests. Development signiert über die Environment `mod-sign`. Die Freigabe signiert über `mod-release` nach der Reviewer-Bestätigung. Der Modloader prüft die abgetrennte Signatur, bevor ein Paket installiert wird. Eine ungültige Signatur blockiert die Installation. Der letzte gültige Katalog bleibt als `.last-good` erhalten. Katalog und Signatur werden erst aktiv, nachdem beide Bytes gemeinsam geprüft wurden. Auf dem Gerät schreibt die Runtime die neue Signatur erst nach erfolgreicher Prüfung und behält das vorherige Paar, falls der Prozess zwischen den beiden Dateien unterbrochen wird.

## 6. Download

Der Laufzeit-Client akzeptiert nur:

- die Asset-API-URL oben
- eine bestehende URL `https://github.com/Prime-Unlimited/Prime-Unlimited-OS/releases/download/<tag>/<datei>`

Er folgt genau einer Weiterleitung von diesen beiden Adressen nach `release-assets.githubusercontent.com`. Andere Hosts und weitere Weiterleitungen werden mit `HTTP redirect blocked` oder `download URL is not supported` abgelehnt. Die Pipeline schreibt nur noch Asset-API-URLs. Splash Manager 1.1.2 behält denselben ZIP-Digest und zeigt jetzt auf die Asset-IDs `773325277` (JC11S) und `773325282` (JP24).

Weitere Fehlermeldungen der Installation: `release asset unreachable`, `package size mismatch`, `package SHA-256 mismatch`, `ZIP is invalid`, `architecture is not compatible`, `firmware is not supported`, `entrypoint is missing`, `native service file is invalid`, `catalog signature verification failed`.

## 7. Wiederherstellung

- Workflow rot: es wurde kein Katalog-Commit erzeugt. Den Lauf erneut starten, nachdem die Ursache in `mod.json` oder der ZIP beseitigt ist.
- Signatur auf dem Gerät ungültig: der Modloader behält den letzten gültigen Katalog und installiert nichts Neues.
- Falsche Version freigegeben: eine bereits veröffentlichte Version wird nicht überschrieben. Eine Korrektur ist eine neue Version unter `mods/development/`.
- Secret oder Environment fehlt: der Workflow endet mit dem Namen des fehlenden Schritts. Er umgeht die Signierung nicht.

Die Runtime, die das Feld `status` versteht, muss auf dem Gerät installiert sein, bevor ein Katalog mit diesem Feld dort ausgewertet wird. Bis dahin bleibt der letzte gültige Katalog aktiv. Diese Änderung wird nicht von hier aus auf ein Gerät ausgerollt.
