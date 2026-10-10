---
markmap:
  initialExpandLevel: -1
  maxWidth: 520
  colorFreezeLevel: 2
---

# `internal/app/` — il cablaggio e i processi di lungo periodo

## Scopo

- Mettere insieme i pezzi e tenerli accesi:
  - leggere la configurazione
  - aprire il log
  - applicare le migrazioni
  - seminare le fondazioni
  - fissare le capacità
  - avviare scheduler, cache, esecutore e ricognitore
  - preparare il TLS
  - poi ascoltare.
- E, nel frattempo, eseguire i job che non hanno un worker dall'altra parte (`worker_tipo = 'server'`).
- Gli altri processi di lungo periodo stanno dove sta la cosa che governano:
  - `Scheduler` in `platform/coda`
  - `Cache` in `platform/storage/staging`
  - `Ricognitore` in `core/rfq/documenti`
- Qui c'è chi li costruisce e chi li fa partire, non come funzionano.

## Non appartiene qui

- Logica di dominio, handler HTTP, query, la lettura di `cockpit.toml` (`platform/config`).
- `esecutore.go` non decide niente: riconosce il tipo del job e chiama chi sa farlo.
- `cmd/cockpit/main.go` non è di quest'area e non fa niente di tutto questo:
  - legge i flag, compila `runtime.Opzioni` e `config.Rete`
  - sceglie il file di configurazione (`percorsoConfig`)
  - chiama `runtime.Esegui`
  - e traduce l'errore in `errore: …` sullo stderr con codice di uscita 1.

## File

- Due package: `runtime`, il processo del server, in otto file; `bancoa`, il banco del motore A (giro 5, A1a e
  A1c), che ha il suo README ([`bancoa/README.md`](bancoa/README.md)).
- `bancoa` non è un processo di lungo periodo e non sa niente del server: legge il manifest del dataset
  privato, gli attesi (è l'unico importatore della libreria YAML), l'indice e le grammatiche, fa girare le
  modalità `regole` e `casi` sul motore A e scrive il rapporto con l'esito in testa. Da A1c ha anche le modalità
  `dsn` (la copia del dump in sola lettura, attraverso `platform/migrazioni` e il caricatore) ed `exports` (gli
  export del DB), con lo stesso percorso puro del prodotto (`valutazione`, poi `confronto`) e il gate.
- File e che cosa tiene:
  - **`esegui.go`**
    - `Opzioni` (con `SoloLettura`) e `Esegui(cfgPath, rete config.Rete, Opzioni)`: la sequenza dell'avvio, un passo per riga, e niente altro
    - `EseguiInLettura` (i comandi che leggono soltanto)
    - `LivelloLog` (`[server].log_livello`)
  - **`avvio.go`**: i passi che vengono prima che esista un servizio
    - `ApriLog`
    - `ApriDatabase`
    - `ApriDatabaseInLettura` (niente migrazioni, transazioni in sola lettura, schema uguale a quello del binario):
      da A1c (P-02) è un involucro di `migrazioni.ApriInLettura`, con l'errore di schema tradotto da `spiegaSchema`
    - `ApriDatabaseSenzaMigrare` e `apriSenzaMigrare` (il pool scrivibile del comando U5, che non migra): restano qui,
      e per lo schema usano `migrazioni.UltimaApplicata` e `migrazioni.SchemaDiverso`
    - `ultimaApplicata` (involucro di `migrazioni.UltimaApplicata`), `spiegaSchema`, `stessoSchema`, `schemaDelBinario`
      (i testi del rifiuto: «backup, poi `-migra`» o «serve il cockpit.exe aggiornato»)
    - `Semina` (utenti, fondazioni, analizzatore corrente, una sola casella attiva prima della 0004)
    - `ImpostaCapacita`
  - **`comandi.go`**: i lavori della riga di comando, che fanno il loro e poi escono
    - `SeminaAnagrafica`
    - `SemeFornitori`
    - `ContaAnagrafiche`
    - `DestinazioneDelDSN` (da A1c un involucro di `migrazioni.Destinazione`, con il testo d'errore del Cockpit)
    - e `ricalcola` (il ritriage delle sole chiavi appena scritte)
  - **`servizi.go`**
    - `Servizi`
    - `CostruisciServizi` (scrittore NAS, scheduler e cache avviati subito, agente, esecutore, ricognitore, ingest, opzioni del sync, anteprima del motore A)
    - `Servizi.Avvia` (esecutore e ricognitore)
  - **`motore_a.go`** (giro 5, A1d)
    - `preparaMotoreA` (l'anteprima del motore A all'avvio: indice, grammatiche, file dei casi, limite, la riga del log)
    - `leggiRegoleMotoreA` e `regoleLette` (il disco: i file solo dentro la cartella dell'indice)
  - **`ascolto.go`**
    - `PreparaTLS` (certificato, impronta, avvisi su chiaro e `token_worker`)
    - `Ascolta` (`web.Server`, `workerapi.Server`, il mux dietro `web.ProtezioneCSRF`, il filtro `rete.SoloDalleReti`, il listener)
    - `logga`
  - **`esecutore.go`**
    - Commento `// Package`.
    - `EsecutoreServer` (`Avvia`, `esegui`)
    - l'interfaccia `Estrattore`: il fascicolo a `core/rfq/documenti`, gli archivi a `transport/workerapi`, l'analisi del messaggio a `ai/agente`
    - `definitivo` (i fallimenti che non si riprovano)
    - `contaScartati`
  - **`vigilanza_nas.go`**
    - `ScrivePerNas` (quali tipi vogliono il NAS)
    - `rinviaSeNasAssente` (rinvio senza consumare il tentativo)
    - `vigilaNas` (il guardiano)
    - `RiaccodaAlRitornoDelNas`

## Entry point

- Simbolo e chi lo chiama:
  - **`Esegui`, `Opzioni`**: `cmd/cockpit/main.go`
  - **`EsecutoreServer`, `RiaccodaAlRitornoDelNas`, `ScrivePerNas`**: l'avvio (`CostruisciServizi`, `Servizi.Avvia`) e le prove L4 del package
  - **`ApriLog`, `ApriDatabase`, `ApriDatabaseInLettura`, `Semina`, `ImpostaCapacita`, `CostruisciServizi`, `PreparaTLS`, `Ascolta`, `SeminaAnagrafica`, `SemeFornitori`, `ContaAnagrafiche`, `EseguiInLettura`, `LivelloLog`**
    - solo `Esegui` (sono esportati per essere letti e provati uno per uno)
  - **`ApriDatabaseSenzaMigrare`, `DestinazioneDelDSN`**: il comando U5 (`RiapriAgganci`)
  - L'apertura in sola lettura e il controllo dello schema che le serve stanno in `platform/migrazioni`
    (`ApriInLettura`, `UltimaApplicata`, `SchemaDiverso`, `Destinazione`, `ControllaSolaLettura`: A1c, P-02),
    dove li usano anche il banco del motore A e gli aiuti delle prove senza importare `runtime`.
- Nessun altro package importa `runtime`.
- **`bancoa.Esegui`, `bancoa.Opzioni`**: `cmd/bancoa/main.go`, che legge i flag `-modalita regole|casi`,
  `-dataset <manifest del dataset privato>`, `-uscita <cartella dei rapporti>` e traduce l'esito nei codici
  d'uscita 0 (conforme), 1 (con differenze), 2 (uso o configurazione, nessun rapporto), 3 (NON ESEGUITO).
  Non legge `cockpit.toml`. Dettaglio in `bancoa/README.md`.
- **`bancoa.EseguiBanco`** (A1c): `cmd/bancoa/main.go`, con `-dsn <DSN della copia, senza password>` o
  `-exports <cartella degli export>`, `-thread`/`-tutti`, `-attesi`, `-gate`; gli stessi codici d'uscita, con il gate
  che decide l'uscita con `-gate`.
  `bancoa.FlagThread` dà a `cmd/bancoa` il flag `-thread`, ripetibile, un UUID per volta.

### La riga di comando (`cmd/cockpit/main.go`)

- Flag ed effetto:
  - **`-config`**: il file di configurazione.
    - Senza il flag: `cockpit.toml` della cartella corrente se c'è, altrimenti quello **accanto a `cockpit.exe`** (un'attività pianificata o un servizio partono da un'altra cartella); se non c'è in nessuno dei due posti il messaggio dice dove ha cercato.
    - Con il flag: quel file e basta
  - **`-migra`**: migrazioni, fondazioni, capacità, ricalcolo delle controparti; poi esce
  - **`-semina-anagrafica FILE`**: il seme dei clienti (poi il ritriage); poi esce
  - **`-importa-fornitori FILE`**: il seme dei fornitori, applicato (poi il ritriage); poi esce
  - **`-anteprima-fornitori FILE`**: il seme dei fornitori, solo guardato, **in sola lettura**
    - insieme a `-importa-fornitori` è un errore
    - poi esce
  - **`-conta-anagrafiche`**: stampa `nome=numero`, una riga per voce, **in sola lettura**; poi esce
  - **`-ascolto`, `-tls-cert`, `-tls-key`, `-url-pubblico`, `-reti`**: la rete al posto di quella di `[server]` (`config.Rete`, gli avviatori di `scripts/avvio-rete`)
    - senza `-ascolto` gli altri sono un errore
- I due comandi in sola lettura (`Opzioni.SoloLettura`) aprono il database con `ApriDatabaseInLettura`:
  - niente migrazioni, niente semi, niente allineamento della coda
  - ogni transazione in sola lettura (`default_transaction_read_only`).
- Se lo schema del database non è quello del binario si fermano:
  - più vecchio → «fare un backup, poi `cockpit.exe -migra`, poi rilanciare»
  - più nuovo → «serve il cockpit.exe aggiornato».
- Se si danno più lavori amministrativi insieme ne parte uno solo, in quest'ordine:
  1. conteggio
  2. anteprima dei fornitori
  3. anagrafica
  4. importazione dei fornitori
  5. `-migra`
- I percorsi dei file di seme sono relativi alla cartella da cui si lancia.

## Dati

- L'area non ha query sue: scrive attraverso i package che chiama.
- **All'avvio e con `-migra`, `-semina-anagrafica`, `-importa-fornitori`** (non con `-conta-anagrafiche` e `-anteprima-fornitori`, che non scrivono):
  - lo schema (`migrazioni.Applica`)
  - `utente`, `casella`, `postazione`, `worker_credenziale` (`fondazioni`)
  - `analizzatore_corrente` (schema ≥ 20)
  - `job` (`coda.AllineaCoda`: marca o annulla)
  - la controparte dei messaggi che non l'hanno (`ingest.RicalcolaControparti`).
- **Su disco:**
  - la cartella dello staging (la crea `config`)
  - il log `<staging>/log/cockpit.log` salvo `[server].log_file`
    - un percorso relativo si legge dalla cartella del toml
    - la rotazione è di `platform/logfile`: se il file è bloccato si continua a scriverci e le copie vecchie restano
  - il certificato e la chiave al primo avvio con TLS.
- **L'esecutore:**
  - `job` (claim, completa, fallisce, rinvia, riaccoda le scritture NAS esaurite)
  - più tutto ciò che scrivono `documenti.CopiaSulNas`, `documenti.CreaCartellaThread`, l'estrattore e `agente.Analizza`.

## Flussi principali

### L'avvio

- Nell'ordine in cui `Esegui` lo scrive:
  1. `config.CaricaConRete`
     - legge il file (percorsi relativi dalla cartella del file)
     - applica la rete della riga di comando
     - valida
     - crea lo staging
     - raccoglie gli avvisi sulle voci sconosciute o senza effetto
  2. `ApriLog`
     - stdout sempre, più il file se si apre (se non si apre è un avviso, non un fermo)
     - poi nel log gli avvisi della configurazione e un `log_livello` non riconosciuto (vale `info`)
  3. il contesto, che si chiude con Ctrl+C o con SIGTERM
  4. **`-conta-anagrafiche` o `-anteprima-fornitori`: `EseguiInLettura`, e qui si esce** (senza migrare)
  5. `ApriDatabase` — pool, `migrazioni.Applica` sotto `pg_advisory_lock`, versione dello schema
  6. `Semina`
     - utenti, poi caselle, postazioni e worker
     - l'analizzatore corrente
     - la regola di una sola casella attiva (vale solo sotto la 0004)
  7. `ImpostaCapacita` — `coda.ImpostaCapacita`, `coda.AllineaCoda`, una riga di log per capacità
  8. `ingest.RicalcolaControparti`
  9. **eventuale lavoro amministrativo che scrive (`-semina-anagrafica`, `-importa-fornitori`, `-migra`), e qui si esce**
  10. `CostruisciServizi` — scheduler e cache partono qui; con `[motore_a].anteprima = true` l'anteprima del motore A legge qui le sue regole (`preparaMotoreA`)
  11. `PreparaTLS`
  12. `Ascolta`
      - nascono `web.Server` e `workerapi.Server`
      - il Fascicolo riceve la pipeline dei worker (`ws.Pipeline = wa`, B8.7)
      - l'esecutore riceve l'estrattore
      - partono esecutore e ricognitore
      - poi `net.Listen`, il filtro delle reti, `Serve` o `ServeTLS`.
- Alla chiusura del contesto (Ctrl+C, SIGTERM): `Shutdown` con cinque secondi di margine.

### Che cosa ferma l'avvio

- Ogni errore torna a `main`, che lo scrive sullo **stderr**.
- Un errore che arriva dopo l'apertura del log si scrive anche nel log («cockpit si ferma»):
  - chi avvia il server da un'attività pianificata lo ritrova nel file.
- Gli errori della configurazione (prima del log) restano solo sullo stderr.
- Passo → si ferma se…
- **config**
  - senza `-config`, nessun `cockpit.toml` né nella cartella corrente né accanto a `cockpit.exe`
  - con `-config`, il file dato non c'è
  - TOML non valido
  - `[db].dsn` mancante
  - `-tls-cert`/`-tls-key`/`-url-pubblico`/`-reti` senza `-ascolto`
  - uno solo fra `tls_cert` e `tls_key`
  - indirizzo non di loopback senza TLS e senza `consenti_lan_in_chiaro`
  - `url_pubblico` malformato o con uno schema diverso da quello del listener
  - una voce di `reti_consentite` che non è della LAN
  - `max_upload_mb < 1`
  - `modalita` diversa da `shadow`/`produzione`
  - radice di produzione in shadow, o in produzione con `nas_scrittura` senza `consenti_nas_produzione`
  - lo staging non si può creare
  - `giorni_sync_iniziale < 0` o `dal` non `AAAA-MM-GG`
  - utenti, caselle, postazioni o worker incoerenti
    - sigla doppia
    - ruolo sconosciuto
    - nessun `admin`
    - casella o postazione non dichiarata
    - token uguale per due worker
    - più caselle senza `casella_default`
  - `[motore_a].anteprima = true` senza `regole` (un indice del motore A rotto invece non ferma: vedi sotto)
- **database**
  - PostgreSQL non raggiungibile o credenziali sbagliate
  - il ruolo non può creare tabelle o l'estensione `pgcrypto` (migrazione 0001)
  - lo schema è più recente del binario
  - `schema_versione` ha dei buchi
  - una migrazione fallisce
- **semi**
  - un utente NUOVO di `[[utenti]]` senza password o con il segnaposto dell'esempio `INSERISCI_PASSWORD_INIZIALE` (il messaggio dice la sigla)
  - un ruolo sconosciuto
  - un vincolo del database rifiuta una casella, una postazione o una credenziale
- **comandi in sola lettura**
  - lo schema del database non è quello del binario (il messaggio dice che cosa fare)
- **TLS**
  - c'è solo uno dei due file
  - i file non si leggono o non sono una coppia valida
  - la cartella non è scrivibile quando vanno generati
- **ascolto**
  - i template non si compilano
  - la porta è occupata
  - l'indirizzo non appartiene a questa macchina
- Non fermano l'avvio, e vanno letti nel log:
  - il NAS non raggiungibile (le copie restano in coda)
  - il log su file che non si apre
  - una voce del toml che non esiste (un refuso)
  - `[server].segreto_sessione` e `[outlook].consenti_invio` (lette ma senza effetto)
  - un `log_livello` non riconosciuto
  - un utente attivo in database che non è più in `[[utenti]]` (il seed non disattiva nessuno)
  - il segnaposto della password su un utente che esiste già
  - credenziali di worker da generare
  - il chiaro sulla LAN dichiarato
  - un `token_worker` rimasto nel file
  - il certificato che non nomina l'host di `url_pubblico`
  - l'agente acceso senza chiave
  - l'indice del motore A che non si legge, è rifiutato o non attiva nessuna grammatica, e il suo file dei casi non letto (`[motore_a]` accesa: la pagina mostra l'errore, il log il motivo)
  - `[retention].giorni_staging` deprecata.

### Il job del server

- `EsecutoreServer.Avvia`, una goroutine, un job alla volta:
  1. `coda.Claim` con `worker_tipo='server'`, id `server` e long-poll di 20 secondi
  2. se il job scrive sul NAS (`ScrivePerNas`: `copia_nas`, `crea_cartella_thread`) e il NAS non c'è, `coda.Rinvia` di un minuto senza consumare il tentativo
  3. `esegui` riconosce il tipo (`estrai_archivio`, `analizza_messaggio_ai`, `crea_cartella_thread`, `copia_nas`)
  4. `coda.Completa`, oppure `coda.Fallisci`.
- Il fallimento è definitivo (niente altri tentativi) quando riprovare non cambia niente (`definitivo`):
  - un conflitto sul NAS (`nas.ErrConflitto`)
  - un tipo che `esegui` non conosce («tipo job non gestito dal server», oggi `sposta_nas` e `backup_db`)
  - l'analisi semantica spenta (`agente.ErrSpento`)
  - l'estrazione degli archivi non configurata.

### Il NAS che torna

- `vigilaNas`, solo con `nas_scrittura` accesa.
- Un controllo all'avvio e poi ogni minuto.
- Quando il NAS c'è (all'avvio) o torna, `RiaccodaAlRitornoDelNas` rimette `pronto` le scritture NAS `fallito` con i tentativi esauriti.

## Invarianti

- Le capacità si fissano **prima** che scheduler ed esecutore partano: nessun job parte con una capacità non ancora letta.
- Il server non semina mai l'anagrafica da solo: seminare è un comando esplicito che si ferma prima del listener.
  - Le fondazioni di `cockpit.toml` invece si seminano a ogni avvio.
- I comandi amministrativi che scrivono (`-semina-anagrafica`, `-importa-fornitori`) chiudono con il ritriage delle sole chiavi appena scritte.
- Un NAS irraggiungibile non è un errore del job: `copia_nas` e `crea_cartella_thread` si rinviano senza consumare il tentativo.
- L'esecutore passa dal tentativo come un worker: `Completa` e `Fallisci` esibiscono il token del claim.
- Senza TLS non si ascolta fuori dal loopback, salvo `consenti_lan_in_chiaro` dichiarato (la regola sta in `config`; qui l'avviso a ogni avvio).
- Il certificato servito è quello di cui si è scritta l'impronta: `ServeTLS` riceve il materiale già caricato, non i percorsi.

## Dipendenze

- `runtime` importa:
  - `core` (`inbox/ingest`, `registro/anagrafica`, `registro/fornitori`, `rfq/documenti`; da A1d `inbox/classificazione/motorea`, `registro/regole/grammatica`, `valutazione`, per l'anteprima del motore A)
  - `ai/agente`
  - `platform` (`coda`, `config`, `contratti/worker`, `db`, `fondazioni`, `jsoncanonico`, `logfile`, `migrazioni`, `rete`, `storage/nas`, `storage/staging`)
  - `transport` (`web`, `workerapi`)
  - e il package radice del modulo (`embed.go`: l'`embed.FS` con migrazioni, template, statici e file dei worker).
- È l'unica area che può conoscerle tutte insieme.
- Lo importa solo `cmd/cockpit`.
- `bancoa` importa:
  - `core` (`inbox/classificazione/motorea`, `registro/regole/grammatica`, `estrazione/evidenze`; da A1b.11
    `estrazione`, solo `DaTesto`, per il modo `casi`; da A1c `fotorfq`, `fotorfq/caricatore`, `valutazione`,
    `confronto`)
  - `platform` (`dataset`, `jsoncanonico`; da A1c `migrazioni`)
  - la libreria YAML, di cui è l'unico importatore (G3)
  - mai `runtime`, `transport`, `ai`, `platform/config`, `platform/db`, `core/ancoraggio`.
- Lo importa solo `cmd/bancoa`.

## Test

- File, livello e che cosa copre:
  - **`analizzatore_db_test.go`** — L4
    - `Semina` scrive l'analizzatore corrente e lo riscrive solo se la chiave cambia
  - **`copianas_db_test.go`** — L4
    - una copia rimessa in coda diventa `scritto`
    - hash diverso sulla destinazione = conflitto, il file non si tocca
  - **`contenutomancante_db_test.go`** — L4
    - il contenuto c'è e si copia
    - contenuto sparito, cartella al posto del file, nessun allegato: il motivo giusto, scritto sul documento
  - **`ripresa_db_test.go`** — L4
    - la copia non consuma la cache
    - contenuto sparito → ripresa da Outlook o riestrazione dall'archivio
    - download già fallito non si insiste
  - **`nas_db_test.go`** — L4
    - cinquanta tentativi per chi scrive sul NAS
    - NAS assente → rinvio senza consumare
    - al ritorno le copie esaurite tornano in coda
    - nessun doppione per chiave
  - **`estrazione_db_test.go`** — L4
    - l'archivio va all'estrattore
    - senza estrattore lo si dice
    - l'estrazione non dipende dal NAS
  - **`lettura_db_test.go`** — L4
    - i comandi in sola lettura non migrano un database vecchio e non scrivono
  - **`motore_a_test.go`** — L1
    - spenta senza file aperti, le regole ACME (`testdata/motore_a/`), indice rotto, grammatica cambiata, casi cambiati, file fuori dalla cartella
  - **`esegui_test.go`** — L1
    - `LivelloLog` (debug, info, warn, error, parola sconosciuta)
    - `SoloLettura`
    - lo schema diverso che ferma i comandi in lettura
    - `definitivo`
    - `contaScartati`
  - **`comune_db_test.go`** — livello: —
    - impalcatura condivisa (DB pulito, accoda, claim, capacità)
  - **`cmd/cockpit/config_test.go`** — L1
    - `percorsoConfig`: con `-config` quel file; senza, la cartella corrente e poi accanto all'eseguibile
  - **`cmd/cockpit/zip_browser_test.go`** — L7 (`integrazione browser`)
    - compila `cockpit.exe` e lo avvia su uno schema vuoto (loopback, senza TLS, `produzione` con la sola `nas_scrittura`), con i worker Python veri e il browser
    - l'unica prova dell'avvio intero
- Le prove chiamano `esegui`, `rinviaSeNasAssente` e `RiaccodaAlRitornoDelNas` direttamente:
  - il ciclo di `EsecutoreServer.Avvia` e `vigilaNas` non ha una prova sua.
- Non hanno prove automatiche i lavori amministrativi che scrivono, `PreparaTLS` e i rifiuti dell'avvio:
  - dopo ogni commit che tocca l'avvio si verifica a mano sul database di prova, `-migra` e poi avvio completo con `GET /healthz`, confrontando il log riga per riga con quello di prima.

## Stato dell'implementazione

- **Avvio e comandi:** completi
  - B10a/B10b
  - rete dalla riga di comando con «Avvio in rete»
  - comandi in sola lettura
  - errore d'avvio anche nel log
  - SIGTERM
- **Esecutore:** `copia_nas`, `crea_cartella_thread`, `estrai_archivio`, `analizza_messaggio_ai`.
  - `sposta_nas` (B8.A4a) è instradato al server ma non eseguito:
    - arriva con B8.8
    - oggi nessuna rotta lo accoda
    - e se lo si accodasse fallirebbe subito, in modo definitivo.
  - `backup_db` non lo accoda nessuno.
- L'esecutore non rinnova il lease:
  - un lavoro più lungo del lease del suo tipo perde il tentativo e viene rifatto.

## Dove intervenire

- Voglio… → apri:
  - **capire in che ordine nasce il server**
    - `runtime/esegui.go:Esegui`, che si legge dall'alto in basso
  - **capire perché il server non parte**
    - il messaggio sullo stderr (o nel log: «cockpit si ferma»), poi la tabella «Che cosa ferma l'avvio»
    - `platform/config/config.go:CaricaConRete`
  - **aggiungere un servizio da avviare**
    - `runtime/servizi.go:CostruisciServizi`, e `Servizi.Avvia` se deve partire dopo le rotte
  - **aggiungere un tipo di job eseguito dal server**
    - `runtime/esecutore.go:esegui` e chi sa farlo
    - `platform/coda` per le tabelle per tipo
  - **dire che un tipo di job ha bisogno del NAS**
    - `runtime/vigilanza_nas.go:ScrivePerNas` (e `RiaccodaScrittureNasEsaurite` in `platform/db/queries/job.sql`)
  - **aggiungere un flag della riga di comando**
    - `cmd/cockpit/main.go` (il flag)
    - `runtime.Opzioni` (il campo)
    - `runtime/comandi.go` (che cosa fa)
    - `runtime/esegui.go` (dove esce)
  - **cambiare una rotta o il suo montaggio**
    - `runtime/ascolto.go:Ascolta`, poi `transport/web`
  - **capire perché un job non parte all'avvio**
    - `platform/coda/capacita.go:AllineaCoda`

## Leggi anche

- `internal/README.md`
- `platform/README.md`
- `platform/coda/README.md`
- `transport/README.md`
- `core/rfq/documenti/README.md`
