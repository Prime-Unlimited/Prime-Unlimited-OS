# Prime Unlimited OS — Prime 4+ (JC11S) Prozessdokumentation

**Stand:** 14.11.2026 · **Status:** Technische Bestandsaufnahme, **keine freigegebene Installationsanleitung**.

## 1. Ziel und Geltungsbereich

Dieses Dokument hält die bisherige Arbeit am Denon DJ Prime 4+ (JC11S) fest: Fastboot-Zugang, `/data`-Image, PRIMEBOMB V1–V4, Testergebnisse, späterer erfolgreicher SSH-Zugang über EngineX OS und die Voraussetzungen für einen reproduzierbaren Prime-Unlimited-OS-Installer.

**Beweisgrenze:** Die alte PRIMEBOMB-Payload hat SSH in keinem der vier dokumentierten Versuche nachweislich aktiviert. Der später erfolgreiche EngineX-Zugang ist separat zu betrachten. Die exakten Änderungen, mit denen EngineX auf JC11S installiert wurde, liegen in der vorliegenden historischen V1–V4-Dokumentation nicht vor.

## 2. Hardware und Fastboot

| Merkmal | Dokumentierter Wert |
|---|---|
| Gerät | Denon DJ Prime 4+ |
| Gerätefamilie | JC11S |
| SoC | Rockchip RK3288 |
| Architektur | ARMv7 / ARM32 |
| Ausgangs-Firmware der V1–V4-Versuche | Engine OS 5.0.4 |
| Später gemeldete Firmware | Engine OS 5.3.0; EngineX-Weboberfläche v5.3.0.2 |
| Fastboot-Seriennummer des Testgeräts | A12506258617421 |
| Slot | a |
| Fastboot-Modus | Bootloader, `is-userspace: no` |
| Partition | `data`, ext4 |
| Partitionsgröße | 2.413.280.768 Byte (`0x8fd7be00`) |
| Fastboot auf WSL2 | 34.0.5-debian |
| Test-Image | 2.306.867.200 Byte (2.200 MiB), Label `az01-data` |

Der Fastboot-Modus wurde laut Versuchsprotokoll beim Einschalten mit **SHIFT + EJECT auf der rechten Deckseite** erreicht. Das ist ein historischer Gerätebefund, keine allgemeine Zusage für alle Firmwarestände.

Die Seriennummer identifiziert **ein einzelnes Testgerät** und ist **keine Modellkennung**. Der spätere Installer muss mehrere echte Fastboot-Identifikatoren auswerten und bei Unklarheit abbrechen. Die konkrete `getvar product`-Ausgabe wurde noch nicht dokumentiert.

### Diagnostische, nicht schreibende Fastboot-Abfragen

```sh
fastboot devices
fastboot getvar all
fastboot getvar current-slot
fastboot getvar is-userspace
fastboot getvar partition-type:data
fastboot getvar partition-size:data
```

Die Ausgabe von `getvar all` kann Seriennummern und andere Geräteinformationen enthalten; vor Veröffentlichung redigieren. Die Abfragen allein sichern **keine** Partition.

## 3. Arbeitsumgebung und Ausgangsimage

Die historischen Versuche wurden unter **WSL2 Ubuntu** mit Fastboot, ext4-Werkzeugen, `debugfs` und `e2fsck` durchgeführt. Das Arbeitsimage hieß `work/data-jc11s.img` und war ein 2.200-MiB-ext4-Image. Es ist **kein Originalbackup** des Prime.

Ein `fastboot fetch data` war mit der eingesetzten Umgebung nicht verfügbar. Es liegt kein verifiziertes vollständiges Original-`/data`-Backup vor. **Ein Flash von `data` kann Einstellungen, WLAN-Konfiguration und weitere lokale Daten überschreiben.**

Vor einer späteren öffentlichen Installation sind Backup, Recovery und Dateisystemprüfung Pflicht. Die externe Musik-SSD ist kein Ersatz für ein Backup der `data`-Partition.

## 4. Boot-, Mount- und Overlay-Mechanismus

Aus der früheren Firmwareanalyse:

1. `az0x-data-mkfs.service` läuft vor dem Mount von `/data`.
2. `data.mount` stellt `/data` bereit.
3. `overlayfs-setup.service` benötigt `/data`.
4. `/etc` wird als Overlay mit Upperdir `/data/system/etc/overlay` eingebunden.
5. Neue Unit-Dateien im Overlay sind möglicherweise erst sichtbar, nachdem systemd bereits Teile seiner Abhängigkeiten geladen hat.

Ein `systemctl daemon-reload` nach dem Overlay-Mount war in der unveränderten Firmware nicht nachgewiesen. Diese Reihenfolge ist eine wesentliche Fehlerhypothese, **keine bestätigte Ursache** für die späteren WLAN-Probleme.

## 5. Ursprüngliche PRIMEBOMB-Payload

Die statische Prüfung des Arbeitsimages dokumentierte:

| Datei / Komponente | Funktion |
|---|---|
| `/ssh/freemymprime.so` | ARM32-Preload-Bibliothek, Modus 0755 |
| `/ssh/setup.sh` | Einrichtungs- und Startskript, Modus 0755 |
| `/ssh/sshd_config` | SSH-Konfiguration |
| `/ssh/authorized_keys` | Public Keys für SSH-Login |
| `/system/etc/overlay/ld.so.preload` | Verweis auf `/data/ssh/freemymprime.so` |
| `freemymprime.service` | Dienst mit Boot-Symlink |

Die Preload-Bibliothek sollte einen Marker setzen, protokollieren, forken und `/data/ssh/setup.sh` starten. **Ihre tatsächliche Ausführung wurde nie belegt.** Der Marker wurde vor erfolgreichem `exec` gesetzt und hätte allein keinen Erfolg bewiesen.

Das Setup sollte einen SSH-Host-Key erzeugen, einen Root-Passworthash setzen und SSH starten. Historisch waren außerdem ein ungeschützter Telnet-Fallback auf TCP 2323 und ein HTTP-Server auf TCP 8080 mit `/data` als Dokumentwurzel vorgesehen. **Beide Fallbacks sind für eine öffentliche Version ungeeignet und dürfen nicht übernommen werden.** Auch ein festes Root-Testpasswort darf nicht in einen öffentlichen Installer.

## 6. Vollständige Versionschronik V1–V4

### V1 — direkter SSH-Start

- Ausgangsimage mit Preload, Setup, SSH-Konfiguration und Passwortlogin.
- Setup versuchte `/usr/sbin/sshd -f /data/ssh/sshd_config` direkt zu starten.
- Image-SHA-256 (vor V2 gemessen): `0eaebfea606f0b8875de10504002d5d0df38a7c0e43ad1a4ff7fcd8c2d942a2a`.
- Fastboot: Sparse 1/1, 1.836 KB; Schreiben 71,420 s; Gesamt 72,734 s; `OKAY`.
- AVB-Warnung enthielt den unplausiblen Wert `140730698971296`.
- **Ergebnis:** Engine OS startete; WLAN-IP `192.168.178.67`; TCP 22, 2323 und 8080 wurden abgewiesen. Kein SSH-Nachweis.

### V2 — systemd-Reload und Dienst-Restart

- Ausgangspunkt: Kopie von V1; erhaltene V1-Kopie ist **kein** Originalbackup.
- Setup rief zuerst `systemctl daemon-reload`, dann `systemctl restart sshd.service` auf; direkter SSH-Start als Fallback.
- Overlay-Drop-in: `/etc/systemd/system/sshd.service.d/10-freemymprime.conf` mit `ExecStart=/usr/sbin/sshd -D -f /data/ssh/sshd_config`.
- **Kein** zusätzlicher Boot-Wants-Symlink für `sshd.service`.
- Ein fehlerhafter `debugfs`-Ersetzungsversuch hinterließ zunächst ein 75-Byte-`setup.sh`; vor dem Flash erkannt und durch Neuaufbau behoben.
- Geprüftes Setup: 1.860 Byte, Modus 0755; Drop-in: 75 Byte, Modus 0644.
- `e2fsck -fn`: ohne Fehler; Image 2.306.867.200 Byte.
- Fastboot: Sparse 1/1, 1.844 KB; Schreiben 67,420 s; Gesamt 68,075 s; `OKAY`.
- AVB-Warnwert `140733051970544`.
- **Ergebnis:** WLAN erreichbar, TCP 22/2323/8080 abgewiesen. Kein SSH-Nachweis. Nach Rückflash von V3 auf V2 war WLAN wieder bedienbar.

### V3 — Boot-Symlink, Unit-Kopie und Host-Key

- Übernahme der `sshd.service`-Unit in das Overlay.
- Boot-Symlink: `multi-user.target.wants/sshd.service -> /etc/systemd/system/sshd.service`.
- Unter WSL generierter SSH-**Host**-Key unter `/ssh/ssh_host_ed25519_key`, Modus 0600 (kein Client-Login-Key).
- V2-Drop-in und `systemctl restart` blieben.
- Unit: 356 Byte, Modus 0644; Host-Key: 411 Byte, Modus 0600.
- `e2fsck -fn`: ohne Fehler.
- SHA-256: `96fe223eab4898858da3631180328b7b722f17eaedeacd846377b5580c4c0523`.
- Fastboot: Sparse 1/1, 1.856 KB; Schreiben 67,171 s; Gesamt 68,075 s; `OKAY`.
- AVB-Warnwert `140731954370624`.
- **Ergebnis:** WLAN-Schalter nach Boot nicht bedienbar. `.67:22` „No route to host“, `.107:22` Timeout. SSH-Status mangels Netzwerk nicht feststellbar. Nach Rückflash auf V2 WLAN wieder funktionsfähig.

### V4 — Start statt Restart

- Frischer Neuaufbau aus V2 mit Unit, Boot-Symlink, Drop-in und Host-Key wie V3.
- Im Setup `systemctl restart sshd.service` durch `systemctl start sshd.service` ersetzt.
- Historische Konfiguration: `PasswordAuthentication yes`, `PermitRootLogin yes`, `AuthorizedKeysFile /data/ssh/authorized_keys`.
- `debugfs` meldete bei mehreren Ersetzungsversuchen „Ext2 file already exists“; danach frischer Aufbau in einer Python-gesteuerten `debugfs`-Sitzung.
- Geprüftes Setup: 1.858 Byte, Modus 0755; Host-Key: 411 Byte, Modus 0600.
- `e2fsck -fn`: ohne Fehler; Image 2.306.867.200 Byte.
- Fastboot: Sparse 1/1, 1.856 KB; Schreiben 66,985 s; Gesamt 68,202 s; `OKAY`.
- AVB-Warnwert `140721357274304`; keine SHA-256-Prüfsumme protokolliert.
- **Ergebnis:** WLAN erneut nicht einschaltbar, SSH auf `.107:22` Timeout. Der Wechsel von `restart` auf `start` behob die Regression nicht.

### Vergleich

| Version | WLAN | SSH | Bewertung |
|---|---|---|---|
| V1 | erreichbar | Port 22 abgewiesen | fehlgeschlagen |
| V2 | erreichbar | Port 22 abgewiesen | Netzwerk-Baseline, kein SSH |
| V3 | WLAN-Schalter defekt | nicht beurteilbar | Regression |
| V4 | WLAN-Schalter defekt | nicht beurteilbar | Regression |

**Wichtig:** Ein `fastboot flash data` mit `OKAY` belegt lediglich den gemeldeten Schreibvorgang, nicht die Funktion der Payload nach dem Boot.

## 7. Späterer erfolgreicher SSH-Zugang (getrennte Entwicklungsphase)

In einer späteren Sitzung wurde EngineX OS auf dem JC11S-Gerät erfolgreich gestartet. Beobachtungen:

- SSH-Login als `root` mit privatem Client-Schlüssel gelang.
- Beispiel der protokollierten Anmeldung (IP nur als historischer LAN-Wert):
  ```sh
  ssh -i ~/.ssh/enginex_prime4plus root@192.168.178.67
  ```
- Prompt nach Login: `root@prime4plus:~#`.
- EngineX-Weboberfläche war unter `http://192.168.178.67:8080` erreichbar und zeigte `v5.3.0.2`.
- USB-Speicher und Musikbibliothek wurden im Webinterface angezeigt.

**Nicht belegt:** vollständige Flash-Kommandofolge, exakte Ausgangsimages, EngineX-Patches für JC11S, SSH-Autostart-Mechanismus, Lizenz- und Redistributionsrechte. Deshalb ist daraus derzeit **noch kein reproduzierbarer Prime-Unlimited-Fastboot-Installer** ableitbar.

## 8. Technische Risiken und Sicherheitsentscheidungen

1. `/data` überschreiben ist potenziell destruktiv. Ohne validiertes Backup und Recovery kein unbeaufsichtigter Flash.
2. Kein eingebautes gemeinsames Root-Passwort; stattdessen individuelle SSH-Ed25519-Schlüssel.
3. Private SSH-Schlüssel bleiben auf dem Benutzerrechner; nur der öffentliche Schlüssel wird übertragen.
4. Kein unauthentifizierter Telnet- oder HTTP-Fallback.
5. Keine proprietären Firmware-Images, geheimen Schlüssel oder fremden Binärdateien ohne Rechteklärung im öffentlichen Repo.
6. Firmware-, Geräte- und Partitionsidentität vor jeder Installation verifizieren; unbekannte Geräte sperren.
7. Jede Mod mit Version, Kompatibilität, Integritätsprüfung und Deinstallations-/Rollback-Strategie dokumentieren.
8. WLAN, normaler Engine-OS-Start, Musikbibliothek und SSH müssen nach Installation separat getestet werden.

## 9. Anforderungen an den zukünftigen Windows-/macOS-Installer

Geplanter Ablauf (Design, **nicht implementiert**):

1. Fastboot-Gerät erkennen und genau eine unterstützte Gerätekennung bestätigen.
2. JC11S oder JP24 anhand verifizierter Eigenschaften auswählen; nie anhand der Seriennummer oder nur der Partitionsgröße.
3. Firmwareversion, Partitionslayout und freien Zielbereich prüfen.
4. Backup und Wiederherstellbarkeit prüfen; Risiken klar anzeigen.
5. Individuelles Ed25519-Schlüsselpaar erzeugen oder vorhandenen Public Key übernehmen.
6. Ausschließlich gerätespezifische, getestete Dateien aus einem nachvollziehbaren Build bereitstellen.
7. Image vor Flash statisch prüfen (Dateisystem, Dateirechte, Hash, erwartete Pfade).
8. Flash erst nach ausdrücklicher Bestätigung und bestandenen Sicherheitsprüfungen.
9. Nach Reboot WLAN, SSH-Key-Login und reguläre Engine-Funktionen verifizieren.
10. Installationsprotokoll und diagnostische Informationen ohne Geheimnisse speichern.

## 10. Noch zu beschaffende Daten

- [ ] Vollständige, bereinigte `fastboot getvar all`-Ausgabe des JC11S.
- [ ] Exakte Fastboot-/Build-Schritte der **erfolgreichen** EngineX-Installation.
- [ ] Rechtlich nutzbare SSH-Payload bzw. eigenständige Implementierung.
- [ ] Verifizierter Backup- und Recovery-Weg für `/data`.
- [ ] Nachweis, welche Dateien und Services auf dem funktionierenden Gerät SSH aktivieren.
- [ ] Validierte Kompatibilitätsmatrix für Engine OS 5.0.4 und 5.3.0.
- [ ] JP24-Dokumentation von Brownie als **separater Gerätebericht**.
- [ ] Tests unter Windows und macOS auf echter Hardware.

## 11. Quellen- und Änderungsstand

**Historische Primärquelle:** „PRIMEBOMB V1–V4 – technische Dokumentation“, Stand 05.11.2026. Die dort dokumentierten Messungen, Kommandos, Beobachtungen und offenen Fragen wurden übernommen und als historische Versuchsreihe gekennzeichnet.

**Zusätzlicher Projektstand aus der anschließenden Zusammenarbeit:** späterer erfolgreicher EngineX-SSH-Login und EngineX-Weboberfläche auf JC11S. Die hierfür nötigen reproduzierbaren Build- und Flashdetails sind noch nicht vollständig dokumentiert.

**Diese Datei ersetzt weder eine verifizierte Installationsanleitung noch einen Wiederherstellungsplan.**
