---
markmap:
  initialExpandLevel: -1
  maxWidth: 520
  colorFreezeLevel: 2
---

# `platform/config` — la lettura di `cockpit.toml`

## Scopo

- Legge `cockpit.toml`, mette i default, legge i percorsi relativi dalla cartella del file, sostituisce la rete del file con quella della riga di comando quando c'è, valida e normalizza.
- Risolve `[sicurezza]` nelle tre capacità di scrittura (`Capacita()`) e calcola le letture derivate che il resto del server usa:
  - dove va il log;
  - per quanto resta un contenuto in cache;
  - ogni quanto gira il ricognitore del NAS;
  - se il listener parla `http` o `https`.
- Raccoglie in `Config.Avvisi` le voci del file che non hanno effetto.
- **Un errore qui ferma l'avvio:** meglio non partire che partire con un routing, una rete o una capacità sbagliati.
- **Una voce sconosciuta o senza effetto** invece non ferma niente: diventa un avviso nel log.

## Non appartiene qui

- Il seed in database (`platform/fondazioni`).
- Il certificato e il filtro del listener (`platform/rete`).
- L'applicazione delle capacità alla coda (`platform/coda`).
- La lettura di `worker.toml` (i worker Python).
- La scelta del file quando manca `-config` (`cmd/cockpit/main.go:percorsoConfig`).
- Il livello del log (`app/runtime/esegui.go:LivelloLog`).
- E l'apertura del file di log con la rotazione (`app/runtime/avvio.go:ApriLog`, `platform/logfile`).

## File

- **`config.go`** — Responsabilità:
  - i tipi `Config`, `Server`, `DB`, `NAS`, `Outlook`, `Sicurezza`, `Retention`, `Analisi`, `Staging`, `Agente`, `MotoreA`, `Utente`, `Casella`, `Postazione`, `Worker`, `Rete`;
  - `Carica` / `CaricaConRete`;
  - le `normalizza*` e `leggiRete`;
  - i percorsi (`cartellaDelFile`, `assoluto`, `conDisco`);
  - gli avvisi (`vociSenzaEffetto` → `Config.Avvisi`);
  - le letture derivate (`Capacita`, `RetentionCache`, `CacheMaxByte`, `IntervalloIntegrita`, `PercorsoLog`, `ETLS`, `Schema`, `HostPubblico`, `RadiceDiProduzione`, `SuLoopback`, `DataDal`, `RuoliAmmessi`)

## Entry point

- **`CaricaConRete(percorso, Rete)`** — Chi lo chiama:
  - `app/runtime.Esegui` (il solo caricamento in produzione);
  - il percorso lo sceglie `cmd/cockpit/main.go:percorsoConfig`:
    - quello di `-config`,
    - altrimenti il `cockpit.toml` della cartella corrente se c'è,
    - altrimenti quello accanto a `cockpit.exe` (se non c'è nessuno dei due, l'errore dice dove ha cercato)
- **`Carica(percorso)`** — Chi lo chiama: i test (è `CaricaConRete` con una `Rete` vuota)
- **`Config.Avvisi`** — Chi lo chiama: `app/runtime.Esegui`: una riga `WARN` «cockpit.toml» per avviso, subito dopo l'apertura del log
- **`Rete`** — Chi lo chiama: `cmd/cockpit/main.go` la compila da `-ascolto`, `-tls-cert`, `-tls-key`, `-url-pubblico`, `-reti`
- **`(*Config).Capacita()`** — Chi lo chiama: `app/runtime.ImpostaCapacita` (→ `coda.ImpostaCapacita`); anche `normalizzaModalita`
- **`PercorsoLog`, `RetentionCache`, `CacheMaxByte`, `IntervalloIntegrita`** — Chi lo chiama: `app/runtime` (`ApriLog`, `CostruisciServizi`)
- **`ETLS`, `Schema`, `HostPubblico`, `SuLoopback`** — Chi lo chiama: `app/runtime` (`PreparaTLS`, `Ascolta`)
- **`DataDal`** — Chi lo chiama: `normalizzaOutlook` (validazione) e `app/runtime.CostruisciServizi` (override del sync)
- **`Casella.EAttiva`** — Chi lo chiama: `platform/fondazioni.Semina`
- **`*Config` intero** — Chi lo chiama: `platform/fondazioni.Semina`; nei test anche `coda`, `web`, `workerapi`

## Dati

- Nessuna tabella.
- Sul disco: legge il file TOML e **crea la cartella dello staging** (`os.MkdirAll` di `[nas].staging`) durante `CaricaConRete`.
- È l'unico effetto del package.

### Le chiavi

- Ogni campo della struttura si legge dal file.
- Una chiave che la struttura non conosce (un refuso come `reti_consentit`, o `passwrd` dentro `[[utenti]]`) non ferma l'avvio:
  - vale come se la riga non ci fosse,
  - e `vociSenzaEffetto` la mette in `Config.Avvisi` con il suo nome completo (`server.reti_consentit`).
- Nello stesso elenco finiscono le due voci che si leggono ma non hanno effetto:
  - `[server].segreto_sessione` (se non è vuota)
  - e `[outlook].consenti_invio` (se è scritta, qualunque valore abbia).
- Gli avvisi si scrivono a livello `warn`: con `log_livello = "error"` non compaiono.

#### I percorsi relativi

- I percorsi relativi di `[nas].radice`, `[nas].staging`, `[server].log_file`, `tls_cert`, `tls_key` (anche quelli di `-tls-cert` e `-tls-key`) e `[motore_a].regole` si leggono dalla cartella del file di configurazione, resa assoluta (`cartellaDelFile`), non dalla cartella da cui parte il processo.
- Restano come sono (`assoluto`):
  - il valore vuoto,
  - un percorso assoluto,
  - un UNC (`\\server-nas\PREVENTIVI`, anche con le barre `/`),
  - un percorso che comincia con `\` o `/`,
  - uno che comincia con la lettera di un disco (`C:`, anche letto da un server Linux),
  - e `log_file = "-"`.

#### `[server]`

- **`indirizzo`**
  - Default: `127.0.0.1:8080` — Obbligatoria: no
  - Verifica / effetto: fuori da loopback serve TLS, o `consenti_lan_in_chiaro`
- **`tls_cert`, `tls_key`**
  - Default: vuoti — Obbligatoria: in coppia
  - Verifica / effetto: relativi alla cartella del file; se i file mancano li genera `rete.Prepara`
- **`tls_nomi`**
  - Default: vuoto = `rete.NomiPredefiniti` — Obbligatoria: no
  - Verifica / effetto: nomi del certificato generato
- **`consenti_lan_in_chiaro`**
  - Default: `false` — Obbligatoria: no
  - Verifica / effetto: unica via per ascoltare in chiaro fuori da loopback
- **`url_pubblico`**
  - Default: vuoto (derivato dal bind) — Obbligatoria: no
  - Verifica / effetto: `http(s)://host[:porta]` senza percorso, schema uguale a `Schema()`
- **`reti_consentite`**
  - Default: vuoto = nessun filtro — Obbligatoria: no
  - Verifica / effetto: ogni voce passa da `leggiRete`: solo reti della LAN
- **`log_livello`**
  - Default: `info` — Obbligatoria: no
  - Verifica / effetto: `debug`, `info`, `warn` (o `warning`), `error`, senza distinguere le maiuscole; vale per tutto il log del server, a schermo e su file. Un'altra parola vale `info` e l'avvio lo scrive nel log (`app/runtime/esegui.go:LivelloLog`)
- **`log_file`**
  - Default: vuoto = `<staging>/log/cockpit.log` — Obbligatoria: no
  - Verifica / effetto: `-` = solo stdout; relativo = dalla cartella del file. Rotazione in `platform/logfile`: il file corrente più 5 copie, da 5 MB; se il file corrente non si lascia rinominare (tenuto aperto da un altro processo) si continua a scriverci oltre la soglia, le copie vecchie restano e si riprova dopo un altro decimo della soglia
- **`modalita`**
  - Default: `shadow` — Obbligatoria: no
  - Verifica / effetto: `shadow` o `produzione`, altro = errore
- **`max_upload_mb`**
  - Default: `64` — Obbligatoria: no
  - Verifica / effetto: almeno 1, altrimenti errore (anche `0`)
- **`token_worker`**
  - Default: vuoto — Obbligatoria: no
  - Verifica / effetto: deprecata: non autentica, l'avvio lo scrive nel log
- **`segreto_sessione`**
  - Default: vuoto — Obbligatoria: no
  - Verifica / effetto: senza effetto (le sessioni stanno nel database); scritta, dà un avviso all'avvio

#### `[db]`

- `dsn`, **obbligatoria** (vuota = errore).

#### `[nas]`

- **`radice`**
  - Default: vuota
  - Verifica / effetto: relativa = dalla cartella del file; nessuna verifica; confrontata con `radici_produzione`
- **`staging`**
  - Default: `<cartella del file>/staging`
  - Verifica / effetto: relativa = dalla cartella del file; creata al caricamento
- **`radici_produzione`**
  - Default: vuoto
  - Verifica / effetto: confronto senza maiuscole, separatori e barra finale (`normalizzaPercorso`)
- **`intervallo_integrita_s`**
  - Default: assente = 900
  - Verifica / effetto: `0` = nessun giro automatico del ricognitore
- **`dry_run`**
  - Default: `false`
  - Verifica / effetto: deprecata: `true` spegne `nas_scrittura` e lo dice fra gli avvisi

#### `[sicurezza]`

- `outlook_scrittura`, `bozze`, `nas_scrittura` (assenti = spente),
- `consenti_nas_produzione` (serve quando `nas_scrittura` è accesa e la radice sta sotto una radice di produzione).
- Sono puntatori: assente e `false` restano distinguibili.

#### `[outlook]`

- **`cartelle`**
  - Default: `["Inbox", "Sent Items"]`
  - Verifica / effetto: —
- **`intervallo_sync_s`**
  - Default: `60`
  - Verifica / effetto: `0` = nessun sync periodico
- **`giorni_sync_iniziale`**
  - Default: `0` = `coda.GiorniSyncInizialeDefault` (7)
  - Verifica / effetto: negativo = errore
- **`sync_apertura_inbox`**
  - Default: assente = `true`
  - Verifica / effetto: —
- **`dal`**
  - Default: vuoto
  - Verifica / effetto: `AAAA-MM-GG` nel fuso del server (`DataDal`), altrimenti errore
- **`lotto`**
  - Default: `50`
  - Verifica / effetto: nessuna verifica
- **`consenti_invio`**
  - Default: `false`
  - Verifica / effetto: senza effetto sul server, che prepara bozze e non invia (l'invio lo decide il `consenti_invio` del `worker.toml`); scritta, dà un avviso all'avvio
- **`casella_default`**
  - Default: con una sola `[[casella]]` è quella
  - Verifica / effetto: con più caselle è obbligatoria; deve essere fra le `[[casella]]`

#### `[retention]`

- `giorni_job` (30; 0 = mai),
- `cache_gg` (assente = 30; 0 = mai per età; negativo = 0),
- `giorni_staging` (deprecata: vale come `cache_gg` se `cache_gg` manca),
- `cache_max_mb` (0 = nessun limite).

#### `[analisi]`

- `versione` (1),
- `parametri` (tabella libera; il suo hash entra nella chiave dei fatti).

#### `[staging]`

- `automatico` (`false`),
- `bootstrap` (`false`),
- `max_mb` (20; `0` lascia la soglia all'ingest, che usa la propria).

#### `[agente]`

- `attivo` (`false`), `modello`, `url`, `chiave_env` (il NOME della variabile), `caselle`.

#### `[motore_a]`

- `anteprima` (`false`), `regole` (vuota; relativa = dalla cartella del file).
- `anteprima = true` senza `regole` ferma l'avvio (`normalizzaMotoreA`, l'unico controllo, che non apre file).
- L'indice, le grammatiche e il file dei casi li legge l'avvio del server (`app/runtime/motore_a.go`), che con un indice rotto parte lo stesso (giro 5, A1d).

#### Le tabelle ripetute

- **`[[utenti]]`**
  - Campi: `sigla`, `nome`, `ufficio`, `ruolo`, `password`
  - Regole: sigla obbligatoria, unica, in maiuscolo; ruolo obbligatorio e dell'enum `ruolo_utente`; almeno un `admin` se c'è almeno un utente
- **`[[casella]]`**
  - Campi: `indirizzo`, `nome`, `canale`, `condivisa`, `utente`, `attiva`
  - Regole: indirizzo obbligatorio, unico, in minuscolo; `nome` = parte locale se manca; `canale` = `outlook` se manca (validato solo al seed); condivisa senza `utente`; `attiva` assente = vera
- **`[[postazione]]`**
  - Campi: `nome_host`, `descrizione`, `utente`
  - Regole: nome host obbligatorio, unico, in maiuscolo
- **`[[worker]]`**
  - Campi: `nome`, `tipo`, `token`, `postazione`, `caselle`
  - Regole: nome obbligatorio e unico; tipo `outlook` o `analisi`; token facoltativo ma mai uguale a quello di un altro worker; postazione e caselle devono essere dichiarate
- La password di `[[utenti]]` qui non si controlla:
  - il rifiuto della password vuota o del segnaposto `INSERISCI_PASSWORD_INIZIALE` per un utente nuovo sta nel seed (`platform/fondazioni/utenti.go:SeedUtenti`).

#### `cockpit.toml.example`

- `cockpit.toml.example` è la procedura per un server nuovo, in ordine, con:
  - percorsi segnaposto sotto `C:\Cockpit\` (radice di prova `C:\Cockpit\nas_prova\PREVENTIVI DA FARE`, staging `C:\Cockpit\server\staging`),
  - un NAS di produzione fittizio (`\\server-nas\preventivi`) fra le `radici_produzione`,
  - indirizzi `@azienda.example`,
  - sigle di `[[casella]]` e `[[postazione]]` dichiarate in `[[utenti]]`.
- Parte così com'è cambiando solo `[db].dsn` e le password (`esempio_test.go`).
- Tre voci dell'esempio sono diverse dal default, e togliere la riga cambia il comportamento:
  - `intervallo_sync_s = 30` (default 60),
  - `[analisi].versione = 3` (default 1, e la versione entra nella chiave dei fatti),
  - `[staging].automatico = true` (default `false`).

## Flussi principali

### Il caricamento (`CaricaConRete`)

1. default nei campi (`indirizzo`, `log_livello`, `max_upload_mb`, `modalita`, `cartelle`, `intervallo_sync_s`, `lotto`, `giorni_job`, `versione`, `max_mb`);
2. `toml.DecodeFile`:
   - ogni chiave presente sovrascrive il default;
   - `vociSenzaEffetto` riempie `Config.Avvisi` (chiavi sconosciute, `segreto_sessione`, `[outlook].consenti_invio`);
3. `dsn` vuota → errore;
4. `[nas].radice`, `[nas].staging`, `[server].log_file` (tranne `-`) e `[motore_a].regole` resi assoluti rispetto alla cartella del file (`assoluto`);
5. `Rete.applica`:
   - con `-ascolto` la rete della riga di comando sostituisce **per intero** indirizzo, certificato, `tls_nomi` (azzerati), `url_pubblico`, `reti_consentite` e `consenti_lan_in_chiaro` (forzato a falso), e segna `DaRigaDiComando`;
   - senza `-ascolto` gli altri flag di rete sono un errore;
6. `normalizzaRete`:
   - percorsi TLS assoluti rispetto alla cartella del file,
   - TLS in coppia,
   - rifiuto del chiaro fuori da loopback,
   - `url_pubblico` coerente con lo schema,
   - `reti_consentite` lette in `Server.Reti`;
7. `max_upload_mb ≥ 1`;
8. `normalizzaModalita`: vedi sotto;
9. staging di default (`<cartella del file>/staging`) e sua creazione;
10. `normalizzaOutlook`, `normalizzaUtenti`, `normalizzaFondazioni` (forma canonica e riferimenti incrociati; `casella_default` dedotta con una casella sola);
11. `normalizzaMotoreA`: `[motore_a].anteprima = true` senza `regole` ferma l'avvio; nessun file aperto.

### Le capacità (`Capacita`)

- In quest'ordine:
  1. `shadow` spegne tutto e lo dice;
  2. nessuna delle tre voci di `[sicurezza]` scritta = tutto spento, con l'avviso;
  3. poi le tre voci come sono;
  4. `dry_run = true` spegne `nas_scrittura`;
  5. se `nas_scrittura` resta accesa su una radice di produzione, un avviso lo dice.

### La modalità (`normalizzaModalita`)

- In `shadow` una `[nas].radice` sotto una radice di produzione ferma l'avvio;
- in `produzione` lo ferma solo se `nas_scrittura` è accesa e manca `consenti_nas_produzione`.

### Le reti (`leggiRete`)

- Una rete o un indirizzo solo (/32, /128), senza zona;
- un IPv4 in forma 4in6 torna IPv4;
- il prefisso si maschera;
- passa se sta dentro uno dei `blocchiLAN` (IPv4 privati, link-local, loopback, IPv6 ULA e link-local, `::1`)
- o se è un IPv6 globale di `/64` o più stretto.

## Invarianti

- `[db].dsn` c'è.
- Mezzo TLS non esiste: `tls_cert` senza `tls_key`, o il contrario, ferma l'avvio.
- Senza TLS, fuori da loopback si parte solo con `consenti_lan_in_chiaro = true`; un indirizzo senza host (`:8080`) conta come LAN.
- `url_pubblico` ha lo schema che il listener parla davvero.
- `reti_consentite` non lascia entrare internet.
- `modalita` vale `shadow` o `produzione`; assente vale `shadow`.
- Il silenzio vale «non scrivere»: una capacità assente è spenta; `produzione` da sola non accende niente.
- Scrivere sul NAS di produzione richiede due dichiarazioni (`nas_scrittura` e `consenti_nas_produzione`).
- Un ruolo sconosciuto ferma l'avvio; con utenti dichiarati c'è almeno un `admin`.
- Nessun riferimento incrociato verso una casella o una postazione non dichiarata; nessun token in comune fra due worker.
- La rete della riga di comando passa dalle stesse verifiche del file.
- Un percorso relativo del file vale dalla cartella del file, qualunque sia la cartella corrente del processo (un'attività pianificata o un servizio partono da un'altra cartella).
- Una voce sconosciuta o senza effetto non ferma l'avvio e non passa in silenzio: è un avviso.
- La sezione `[motore_a]` non apre file al caricamento.

## Dipendenze

- Importa `platform/db` (per l'enum dei ruoli) e librerie (`BurntSushi/toml`).
- È importato da `cmd/cockpit`, `app/runtime` e `platform/fondazioni`;
  - nei test anche da `platform/coda`, `transport/web`, `transport/workerapi`.
- Nessuna violazione della tabella di `internal/README.md`.

## Test

- Tutti L1 (nessun tag, nessun database), su file TOML scritti in una cartella temporanea:
  - **`config_test.go`** — Che cosa prova:
    - forma canonica di caselle e postazioni,
    - `casella_default` dedotta,
    - i rifiuti dei riferimenti incrociati e dei token duplicati,
    - `attiva` assente = vera,
    - `[analisi]`
  - **`esempio_test.go`** — Che cosa prova:
    - `cockpit.toml.example` parte cambiando solo le password (lo staging spostato in una cartella temporanea):
      - nessun avviso,
      - ogni sigla di `[[casella]]` e `[[postazione]]` dichiarata in `[[utenti]]`,
      - indirizzi `.example`,
      - radice fuori dalle radici di produzione
  - **`finestra_test.go`** — Che cosa prova: `giorni_sync_iniziale` (assente, scritta, negativa) e `dal` (malformata, fuso locale)
  - **`modalita_test.go`** — Che cosa prova: assente = shadow, valore sbagliato rifiutato, shadow su radice di produzione, produzione su radice di produzione
  - **`motore_a_test.go`** — Che cosa prova:
    - sezione assente = spenta;
    - accesa senza `regole` = errore d'avvio;
    - `regole` relativa letta dalla cartella del file, assoluta, UNC e con il disco invariate;
    - spenta con `regole` che non c'è = nessun errore;
    - le due voci sono conosciute (nessun avviso), un refuso nella sezione è un avviso
  - **`percorsi_test.go`** — Che cosa prova:
    - `radice`, `staging` e `log_file` relativi letti dalla cartella del file anche da un'altra cartella corrente (e con `-config` relativo);
    - assoluti, UNC, «dalla radice» e con disco invariati;
    - `log_file = "-"`;
    - chiavi sconosciute e senza effetto negli avvisi senza fermare l'avvio;
    - `max_upload_mb = 0` rifiutato
  - **`radiceprod_test.go`** — Che cosa prova: il secondo consenso per il NAS vero e il suo avviso; in shadow il consenso non sblocca niente
  - **`retention_test.go`** — Che cosa prova: `cache_gg`, `giorni_staging` deprecata, capienza
  - **`rete_test.go`** — Che cosa prova: `SuLoopback`, rifiuto del chiaro in LAN, mezzo TLS, percorsi relativi al file, radici di produzione POSIX
  - **`reti_test.go`** — Che cosa prova: `leggiRete` (ammesse e rifiutate), `reti_consentite` dal file, la rete della riga di comando che vale per intero
  - **`ruoli_test.go`** — Che cosa prova: ruolo inesistente, i quattro ruoli, sigla doppia, nessun admin, senza utenti
  - **`sicurezza_test.go`** — Che cosa prova: le regole di `Capacita()`, `dry_run` deprecata
  - **`urlpubblico_test.go`** — Che cosa prova: forma e schema di `url_pubblico`, `HostPubblico` anche IPv6
- Fuori dal package:
  - `app/runtime/esegui_test.go` prova `LivelloLog` (le quattro parole, `warning`, una parola sconosciuta);
  - `cmd/cockpit/config_test.go` prova la scelta del file senza `-config`.
- Non provato: `IntervalloIntegrita`.

## Stato dell'implementazione

- Completo per:
  - il blocco 4 (capacità),
  - la voce 2.4 (TLS, credenziali individuali),
  - 7C.1 (`url_pubblico`),
  - l'avvio in rete (`reti_consentite`, `Rete`),
  - i percorsi relativi alla cartella del file
  - e gli avvisi sulle voci senza effetto.
- Limiti noti:
  - `[nas].radice` vuota non è un errore nemmeno con `nas_scrittura` accesa (le scritture restano in coda perché la radice non risulta raggiungibile);
  - `segreto_sessione` e `[outlook].consenti_invio` restano lette (per dirle nel log) invece di essere rifiutate: un file scritto per un binario di prima deve continuare a partire.

## Dove intervenire

- **Voglio… una chiave nuova** — Apro:
  - il tipo della sezione in `config.go`,
  - il default in `CaricaConRete` (se è un percorso, anche `assoluto` lì),
  - la verifica in una `normalizza*`,
  - poi i due `*.example` (`esempio_test.go` controlla che l'esempio del server non dia avvisi)
- **Voglio… rendere «senza effetto» una chiave che c'era** — Apro: lasciarla nel tipo e aggiungere l'avviso in `vociSenzaEffetto` (toglierla dal tipo la farebbe diventare «sconosciuta»)
- **Voglio… capire perché una riga del file non conta** — Apro: le righe «cockpit.toml» del log di avvio (`Config.Avvisi`)
- **Voglio… cambiare che cosa ferma l'avvio** — Apro: `normalizzaRete`, `normalizzaModalita`, `normalizzaUtenti`, `normalizzaFondazioni`, `normalizzaOutlook`, `normalizzaMotoreA`
- **Voglio… capire perché una capacità è spenta** — Apro: `Capacita()` e i suoi `Avvisi` nel log di avvio
- **Voglio… ammettere un altro tipo di rete** — Apro: `blocchiLAN` e `leggiRete`
- **Voglio… un flag di rete nuovo** — Apro: `Rete`, `Rete.applica`, `cmd/cockpit/main.go`

## Leggi anche

- `internal/platform/README.md`
- `internal/platform/rete/README.md`
- `internal/platform/fondazioni/README.md`
- `internal/app/README.md`
- `cockpit.toml.example`
- il README principale («Configurazione del server», «Mettere il Cockpit in rete»).
