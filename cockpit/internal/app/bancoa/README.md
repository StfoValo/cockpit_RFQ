---
markmap:
  initialExpandLevel: -1
  maxWidth: 520
  colorFreezeLevel: 2
---

# `internal/app/bancoa` — il banco del motore A

## Scopo

- Il **runner del banco** del motore A (giro 5, R24 a, R40 e). In A1a ha due modalità, tutte e due senza DB (da A1c
  ci sono anche `dsn` ed `exports`, qui sotto):
  - **`regole`** («profili attivi validi», A1a-P1): compila le grammatiche dell'indice con i limiti dell'indice
    (R43 B), ne verifica gli esempi e scrive per cliente hash, famiglie con ruoli e categorie, forme attive e
    riservate, riserve, esempi verificati e non verificati, le **lacune** della copertura minima di P1 §5.4
    (R20 b, informative) e la **coerenza** fra gli esempi con `rif_caso` e i casi degli attesi che ripetono
    (R47 b, CP-17);
  - **`casi`** (A1a-P2; da A1b.11 A1b-P1): esegue i `casi_contratto` degli attesi e confronta l'esito con
    l'atteso, chiave per chiave. Da A1b.11 il testo del caso diventa un documento di una sola unità con
    `estrazione.DaTesto`, e `Motore.Interpreta` lo legge con l'uso sconosciuto del documento
    (`evidenze.UsoSconosciuto`, 5.4.5): nessuna selezione inventata. Le letture si dividono per funzione del
    router (R25 a): letture d'identità (`identita_file`, `struttura`, `richiesta`), menzioni, attributi.
    Esiti: passato, parziale, fallito, riservato, rimandato; un parziale, un riservato o un rimandato non è
    mai un passato.
- Da A1c (B6, P9; piano 6.4.9; T-B6-03, F0-01) le modalità **`dsn`** ed **`exports`**, con un punto d'ingresso loro
  (`EseguiBanco`) e un rapporto loro (versione 3):
  - **`dsn`**: la copia del dump in sola lettura. `migrazioni.ApriInLettura` con le migrazioni incorporate del binario
    (`Opzioni.Migrazioni`, che dà `cmd/bancoa`: F0-16) e il controllo dello schema; `migrazioni.ControllaSolaLettura`
    con le tabelle escluse del manifest; la copia del manifest (ruolo, database, schema, tabelle escluse, sentinelle:
    `controllaCopia`, R-106; le sentinelle sono una riga delegata ad A1c-L4D-01, R117 b); la prima scrittura del
    rapporto; `caricatore.Carica` (una transazione REPEATABLE READ
    READ ONLY), con i messaggi fuori RFQ dei casi senza thread (T-B6-04, D-V1-3); un messaggio dei casi che la copia
    non ha si toglie e si ricarica, ed è una differenza del controllo dei casi (`caricaSenzaIMancanti`);
  - **`exports`**: gli export del DB (`FotografiaDaExport`), la stessa fotografia con `Coerente` falso e le sezioni
    filtrate, parziali o assenti dichiarate (T-12);
  - poi lo **stesso percorso puro del prodotto**: `valutazione.Calcola` (con il file dei casi, letto con
    `valutazione.LeggiIngressi` prima della fotografia), la copia campo per campo di `passaggio.go` nei DTO di
    `confronto` (R53 B), `confronto.Confronta`; per i thread con un caso, una seconda valutazione senza il caso
    (`senza_caso`, R48 A);
  - sempre, il file dei casi contro la fotografia (`casi_e_fotografia`, R-103) e, con gli export, i clienti dei thread
    (`clienti_degli_export`, T-B6-104);
  - con **`-attesi`**: le sezioni degli attesi lette con `LeggiSezioni` e tradotte con i nomi neutri (M-21, P-10), i
    **controlli del runner 1–5**, la baseline, la C5, le letture e le chiavi booleane dei file
    (`letture_e_invarianti_dei_file`, R-101), gli esiti contro l'atteso, i casi di contratto, il **gate**; con
    **`-gate`** il gate decide l'uscita (0 superato, 1 non superato, 3 incompleto: R44).
- Legge tutto **da fuori il repository**, attraverso il manifest del dataset privato (`platform/dataset`):
  attesi, indice delle regole, grammatiche, file dei casi, export. Ogni file del manifest passa dal controllo di
  sha256 e byte; l'indice controlla gli sha256 delle grammatiche. Con `dsn` legge il DB solo in sola lettura, e scrive
  solo i file del rapporto.
- Scrive il **rapporto** con l'esito in testa (R44): «ESITO: ESEGUITO — conforme», «ESITO: ESEGUITO — con
  differenze (n)» o «ESITO: NON ESEGUITO — <motivo>», più l'elenco dei controlli, ciascuno eseguito o non
  eseguito con il motivo. In JSON canonico e in testo, in modo atomico.
- È **l'unico importatore della libreria YAML** del modulo (G3): gli attesi entrano solo qui, tradotti nei DTO
  del runner. La libreria è `gopkg.in/yaml.v3 v3.0.1`, il ripiego offline di R9 (`go.yaml.in/yaml/v3` non è
  nella cache dei moduli; le due hanno la stessa API).

## Non appartiene qui

- **Riconoscere e interpretare i codici**: lo fa il motore del prodotto (`motorea.CompilaInsieme`,
  `Motore.Riconosci`, `Motore.Interpreta`). Il runner è il giudice, non un secondo motore (R25): legge
  l'interpretazione e la confronta; la funzione di una lettura è quella che Interpreta ha scritto.
- **Costruire documenti**: il documento del caso lo fa `estrazione.DaTesto`, l'unica funzione di
  `core/estrazione` che il banco usa (F19, R52 A). Nessuna entità condivisa, nessuna selezione dei segmenti
  costruita dal banco.
- **Nomi di clienti, profili, percorsi o codici reali**: nel codice ci sono solo i nomi neutri delle chiavi
  degli attesi (R47 a, M-21). I profili si legano ai clienti nel manifest (D-09); i valori attesi vengono
  dagli attesi a runtime e compaiono solo nei rapporti privati.
- **Leggere il DB per conto suo**: la fotografia la legge il caricatore, l'unico che parla con il DB (in sola
  lettura); l'apertura e i controlli del collegamento sono di `platform/migrazioni`. Questo pacchetto non nomina pgx:
  il pool passa per inferenza di tipo. Nessuna query e nessuna scrittura (R92); le sentinelle e le impronte di
  contenuto della copia non si contano né si calcolano qui (nessun SQL nuovo, T-B0-36): le controlla `testutil.PoolDump`
  nella L4 sul dump (A1c-L4D-01), per l'ambiente della copia intatta, e il rapporto lo scrive come riga delegata
  (R117 b, ratificata e ampliata dall'utente il 07/10, domande-a1c.md; T-B6-219). Gli ambienti della copia per PO-30 e
  della copia rianalizzata (la parte (c) di R117) non sono qui.
- **Valutare, proporre, confrontare**: lo fanno `valutazione` e `confronto`, gli stessi dell'anteprima. Il banco
  traduce gli attesi nei DTO di atteso di `confronto` e li confronta solo lui (R2, P-11): il motore non li vede mai.
- **`ancoraggio`**: il banco non lo importa (la freccia c'è solo nella prova di parità, `passaggio_test.go`). Le
  costanti del motore che `confronto` ricopia come stringhe le controlla quella prova (T-B6-50).
- **Il gate del prodotto**: gli assi del prodotto, lo stato e il fascicolo sono solo informazione nel rapporto
  (sezione `prodotti`; T-B0-16, R69 A), mai una voce del gate né una NON ESEGUITA. `cockpit.toml` non si legge.
- **I valori degli attesi, del file dei casi, degli export**: nel codice ci sono solo i nomi neutri delle sezioni e
  delle chiavi; i nomi dei file degli export stanno nel manifest privato.

## File

- **`bancoa.go`** — responsabilità:
  - commento `// Package`; `VersioneRapporto` (2 da A1b.11: funzione delle letture e attributi nei casi); le
    modalità; i nomi delle voci del manifest (`attesi`,
    `regole.indice`);
  - `Opzioni`, `Esito` con `CodiceUscita` (0, 1, 3), `ErroreUso` (uscita 2), `Esegui`: manifest, profili,
    indice, attesi, la modalità, l'esito dai controlli (una differenza prevale su un non eseguito), il rapporto.
- **`attesi.go`** — responsabilità:
  - `LeggiAttesi`: la testata e i `casi_contratto`, con la decodifica stretta (chiavi sconosciute, ripetute,
    secondo documento: errori); le altre sezioni accettate e non lette;
  - `Attesi`, `Testata`, `CasoContratto`, `Precondizioni`, `ChiaveAttesa`, `ValoreAtteso` (il testo come è
    scritto: una revisione «00» resta «00»). Unico file con l'import della libreria YAML.
- **`traduzione.go`** — responsabilità:
  - `TraduciContesto` (`figlio_step.<campo>` → `nodo_step.<campo>`, R19 a; la notazione puntata di
    `evidenze.LeggiSelettore`, R19 b);
  - la tabella «chiave degli attesi → campo o predicato» del par.4.7.5, con le sessioni (A1a, A1b, decaduta):
    `ChiaveNota`, `SessioneChiave`, `ChiaviNote`; i predicati sulle letture e sugli attributi;
  - la divisione dell'interpretazione per funzione (`nuovaScena`, `dividiLetture`, `funzioneDIdentita`) e
    `funzioneNelBanco`, la funzione del router con l'uso del modo casi, che usa la coerenza.
- **`regole.go`** — responsabilità:
  - `VerificaRegole`, `RapportoRegole` e le sue parti: clienti, famiglie, riserve, esempi, lacune, coerenza.
- **`casi.go`** — responsabilità:
  - `EseguiCasiContratto` (con `DaTesto` più `Interpreta` e l'uso sconosciuto), la verifica delle precondizioni,
    `EsitoCaso`, `EsitoChiave`, `LetturaRapporto`, `ConteggiCasi`, `Conta`.
- **`rapporto.go`** — responsabilità:
  - `Rapporto`, `Versioni`, `Controllo`, `RapportoCasi`; `PrimaRiga`, `Testo`; `ScriviRapporto` (`.tmp`, poi
    `Rename`; rifiuta una cartella dentro il modulo). Il rapporto di A1a: non cambia. Da A1c `Controllo` ha anche
    `Classe` ed `Esito`, con `omitempty`: li riempie solo il rapporto versione 3, e il JSON di A1a resta lo stesso, byte
    per byte (R116 B).
- **`banco.go`** (A1c) — responsabilità:
  - `EseguiBanco`, `validaBanco` (le cinque condizioni di F0-01, il DSN senza password, `PGPASSWORD`, le regole dei
    flag), `FlagThread` (il flag `-thread` del comando, R-114), le sequenze `-dsn` (con `caricaSenzaIMancanti`) ed
    `-exports`, il percorso comune dalla fotografia al rapporto (`valuta`, con il contesto della corsa), i casi di
    contratto, `senza_caso`, il censimento, le correzioni manuali per cliente, il profilo dei limiti.
- **`export.go`** (A1c) — responsabilità:
  - `FotografiaDaExport`, `VociExport`, `ErrExportNonValido`: gli export elencati dal manifest (`export.<sezione>`),
    con sha256 e byte, nella forma `{query: righe}`, nella fotografia, con le sezioni dichiarate.
- **`passaggio.go`** (A1c) — responsabilità:
  - `inFile`, `inProdotti`: la copia campo per campo dai record piatti di `valutazione` ai DTO di `confronto`.
- **`sezioni.go`** (A1c) — responsabilità:
  - i DTO delle sezioni lette (`SezioniAttesi`, `ScenarioAtteso`, `VoceAttesa`…) e la traduzione in
    `confronto.Atteso`, thread per thread (`traduci`), con gli errori di traduzione (D8, R-51).
- **`controlli.go`** (A1c) — responsabilità:
  - i controlli del runner 1–5, la traduzione, i controlli (1) e (2) della baseline, gli invarianti della C5, le
    letture attese dei file (`controlloLetture`, con le regole della tabella dei casi), il file dei casi contro la
    fotografia, i clienti degli export, la copia del manifest e la sola lettura (`controllaCopia`, `solaLetturaDi`),
    le impronte del confronto; il contesto della corsa (`contestoCorsa`); da B6c le righe decise con le loro fonti
    (`fontiRighe`: la sigla di chi ha deciso o confermato, la provenienza del documento), gli ambiti della sezione dei
    reali (`ambitiDellaSezione`, D-R116 O) e le righe della «Conferma l'albero» per la sezione informativa (D-R116 I).
- **`gate.go`** (A1c) — responsabilità:
  - `Gate`, `VoceGate`, `calcolaGate` (le sette voci, una per una, con la classe e l'esito), l'uscita con `-gate`;
    `RevisioniDellaBaseline`, le differenze di revisione diagnosticate accanto alle decisioni preservate (R113 B).
- **`classi.go`** (A1c) — responsabilità:
  - la classe e l'esito tri-stato di ogni controllo e di ogni voce del gate (R116 B, precisata dall'utente il 07/10):
    `ClasseControllo`, `EsitoTriStato`, `ControlloDelegato` (R117 b), la tabella statica delle classi con la
    motivazione, la fonte e, per gli esterni, la prova; il riepilogo `Chiusura`; da B6c le decisioni dell'utente
    dell'08/10 (D-R116 I, O, F; D-R115 A; D-R114 B) nelle righe della tabella, e la riga fuori perimetro della parte del
    n.1 con gli export (`ParteFinitiConGliExport`).
- **`prodotti.go`** (A1c) — responsabilità:
  - la sezione informativa `prodotti` (i sette assi, lo stato, il fascicolo, i conflitti; «non calcolato» dove manca
    una sezione: T-12) e la fonte contro l'atteso dei prodotti attesi dello scenario (`fontiDelloScenario`, PO-29, con
    la regola `RegolaFonteAttesa`, la derivazione e i livelli). R109, precisata dall'utente il 07/10, senza scegliere
    una lettera. Lettura [T]: la regola del runner (la A del testo della domanda) resta valida, con la derivazione dagli
    attesi esplicita e indipendente dal motore (E2 §2.3).
- **`rapporto_banco.go`** (A1c) — responsabilità:
  - `RapportoBanco` (versione 3) e le sue sezioni, con i dettagli dei controlli (`DettaglioControllo`: le differenze,
    le parti non verificate, non applicabili e fuori perimetro una per una) e la sezione informativa delle righe della
    «Conferma l'albero» (`RigheInformative`, D-R116 I), `PrimaRiga`, `Testo`, `scriviRapportoBanco`
    (`rapporto-dsn.json` e `.txt`, `rapporto-exports.json` e `.txt`).
- **`attesi.go`**, in più da A1c: `LeggiSezioni`, la lettura stretta delle sezioni (unico file con la libreria YAML).

## Entry point

- **`Esegui`, `Opzioni`, `Esito.CodiceUscita`, `ErroreUso`** — chi li chiama: `cmd/bancoa/main.go`.
- **`EseguiBanco`, `RapportoBanco`** (A1c, modalità `dsn` ed `exports`) — chi li chiama: `cmd/bancoa/main.go`; da Q10
  le L4 sul dump, attraverso il runner (R54).
- **`FotografiaDaExport`, `LeggiSezioni`** — chi li chiama: `EseguiBanco`.
- **`FlagThread`** — chi lo chiama: `cmd/bancoa/main.go`, per il flag `-thread` (così il comando non importa la
  libreria degli UUID: I.2).
- **`LeggiAttesi`, `VerificaRegole`, `EseguiCasiContratto`, `ScriviRapporto`** — chi li chiama: `Esegui`; da A1c
  le prove private sul dump, che passano dal runner (R54).

### La riga di comando (`cmd/bancoa/main.go`)

- `bancoa -modalita regole|casi -dataset <manifest del dataset privato> -uscita <cartella dei rapporti>`:
  - `-dataset` è lo stesso file di `COCKPIT_DATASET_A`; `-uscita` la cartella dei rapporti, che si crea se
    manca. Tutti e due fuori dal modulo.
- `bancoa -dsn <DSN della copia, senza password> -dataset <manifest> (-thread <uuid> ... | -tutti) [-attesi [-gate]] -uscita <cartella>`
- `bancoa -exports <cartella degli export> -dataset <manifest> [-thread <uuid> ...] [-attesi [-gate]] -uscita <cartella>`
  - la modalità la dice `-dsn` o `-exports` (o `-modalita dsn|exports`); `-dsn` ed `-exports` si escludono; `-tutti`
    solo con `-dsn`; `-manifest` è un sinonimo di `-dataset`; `-gate` vuole `-attesi`;
  - il DSN non porta la password (pgx la legge da `pgpass.conf`), e con `PGPASSWORD` impostata il banco non parte
    (R32 c); gli errori non ripetono il DSN;
  - le righe a video: «sorgente: …», poi «collegato in sola lettura: ruolo …, scrittura possibile: no, tabelle escluse
    non leggibili: n» (con `-dsn`) o «nessun database aperto; fatti dagli export (bypass di Outlook, download e parser
    Python)» (con `-exports`), poi «rapporto: <percorso>», con la prima scrittura del rapporto («ESITO: NON ESEGUITO —
    lettura non cominciata»); alla fine il riepilogo, con l'ultima riga «scritture: …».
- Codici d'uscita, gli stessi del riepilogo delle prove e del controllo prima del push (R44):
  - **0** eseguito e conforme;
  - **1** eseguito, con differenze: un errore di grammatica, un caso fallito, un controllo del runner fallito;
  - **2** uso o configurazione (flag sbagliati o mancanti, uscita o dataset dentro il modulo): nessun rapporto,
    la causa su stderr;
  - **3** NON ESEGUITO: il manifest manca o non si legge, una sua voce manca o ha un'impronta diversa; con `-dsn` la
    copia non si raggiunge, ha uno schema diverso dal binario o non è quella del manifest; con `-gate`, un gate
    incompleto (una voce non eseguita e nessuna non superata). Le sentinelle della copia, delegate ad A1c-L4D-01, non
    danno il 3 (R117 b); le classi e la chiusura non cambiano mai l'uscita, e il 3 non diventa 0 (R116 B).
- Il riepilogo va anche su stdout. Contiene dati privati: non si incolla in commit, PR o note pubbliche.

## Invarianti

- **Mai «conforme» se un ingresso non è stato letto**: senza dataset non esiste un rapporto verde. Una voce che
  manca o è cambiata dà NON ESEGUITO; un file presente ma non valido dà una differenza (par.3.6.5).
- **1 prevale su 3**: una differenza trovata si vede sempre, anche se qualcosa non è stato eseguito.
- **Una chiave degli attesi che la tabella non conosce rende gli attesi illeggibili**: una chiave nuova non si
  ignora. Una chiave che il runner non sa controllare è «rimandata» con il motivo, mai passata: da A1b.11 lo
  sono solo le chiavi decadute e le precondizioni che `DaTesto` non sa costruire.
- **Il modo casi non inventa selezioni** (5.4.5): l'uso è sempre `evidenze.UsoSconosciuto` del documento di
  `DaTesto`. Su oggetto e corpo la riga 2 del router dà «richiesta», che conta fra le letture d'identità.
- **Letture d'identità** (R25 a): le letture con funzione `identita_file`, `struttura` o `richiesta`, per
  occorrenza. Le leggono base, basi, `letture_identita` (su ogni selettore) e le chiavi dei campi; menzioni e
  attributi stanno fuori, e li leggono le loro chiavi (`base_menzionata`, `identita_file_da_nota`,
  `nessuna_fusione`; `revisione`, `stato`, `originale_conservato`, `cifre`). Sui selettori di A1a le letture
  d'identità sono tutte le letture di forma: le chiavi di A1a non cambiano significato.
- **Confronto** (R25 c, d): esatto per base, basi, marcatore, affisso e revisione; per le altre chiavi il valore
  atteso sta fra quelli letti, e una lista vuota vuole zero valori letti (mai un passato per vuoto); una
  chiave che l'atteso non nomina non si controlla.
- **La revisione in campo separato** viene dall'attributo di Interpreta (5.4.6 punto 12): con `DaTesto`
  l'entità non ha letture di codice, quindi vale l'unica regola attiva sul selettore; un valore che non la
  rispetta è `non_interpretabile`, con l'originale. Le precondizioni (entità condivisa con un codice) si
  verificano: il codice della precondizione, letto da solo sul campo del codice della stessa entità, deve
  essere di una famiglia la cui regola ha dato la revisione; se non si legge da solo, la precondizione è
  rimandata, mai passata. Le precondizioni da sole non fanno passare un caso senza chiavi dell'atteso.
- **Un'interpretazione parziale o non disponibile non si giudica** (5.4.6 punto 16; A1b-22): con un limite
  superato, o con il testo del caso vuoto (E5 b = A), il caso è fallito, con il motivo, anche se le chiavi
  tornano. Un testo senza codici invece è un'interpretazione completa con zero letture, e si giudica. Vale per
  il caso del banco: un messaggio senza testo leggibile dentro un thread non è senza significato, e in A1c
  fotografia e proposte usano le altre interpretazioni del thread.
- **La coerenza esempio/caso legge ogni chiave come la legge il runner** (`traduzione.go` è l'unica fonte della
  semantica di una chiave; par.4.10 n.3): con la funzione del router sul selettore dell'esempio
  (`funzioneNelBanco`), le letture attese su un selettore d'identità valgono per base, basi, marcatore,
  revisione e contraddicono `letture_identita = 0`; su un selettore di menzione non valgono per nessuna di
  queste; su un campo della revisione valgono solo per revisione. Per `basi` un esempio contraddice il caso solo
  se attende una base che il caso non elenca (non sa scrivere le ripetizioni). In un conflitto vincono gli
  attesi, e si corregge l'esempio.
- **Un caso definito con `dipende_da` si valuta**, e il rapporto lo annota; un caso con `stato_atteso:
  riservato` non si valuta.
- **Nessun ordine dipende da una mappa**: casi nell'ordine degli attesi, chiavi in ordine alfabetico, clienti
  nell'ordine dell'indice. Niente orologio nel rapporto.

### Le modalità `dsn` ed `exports` (A1c)

- **A1a non cambia** (F0-01): `Esegui`, il rapporto di A1a e `ScriviRapporto` restano com'erano; `Esegui` rifiuta
  le modalità di A1c e i loro flag con regole e casi (errore d'uso); un'opzione `dsn` senza DSN, o `exports` senza la
  cartella, è un errore d'uso.
- **Il file dei casi prima della fotografia** (T-B6-04): `caricatore.Richiesta.Messaggi` ha solo i messaggi dei casi
  senza thread (D-V1-3).
- **I controlli del runner** girano prima degli esiti; un fallimento è una differenza (uscita 1), mai un «mancante»;
  un ingresso che manca o è cambiato è NON ESEGUITO (uscita 3). Con i soli thread scelti (`-thread`) gli ID degli
  attesi di altri thread non sono differenze ma parti non verificabili, e così lo scenario se il suo thread non è
  scelto (il n.1 e i conteggi del gate: R-104); uno sha256 sbagliato resta una differenza.
- **La regola delle chiavi accettate** (T-B6-102, precisata dopo la revisione di P9): una chiave degli attesi che il
  runner accetta è verificata, oppure è dichiarata non verificata fra le parti non eseguite del controllo che la
  riguarda, con il percorso e senza valori; il controllo senza differenze è allora NON ESEGUITO. Mai accettata e
  buttata. I dettagli dei controlli nel rapporto le elencano una per una. Così, per esempio: `deciso_da` con gli
  export (R-102); le letture senza ambito di un file con l'interpretazione parziale. Un campo che una riga della
  fotografia non ha (per esempio `confermato_il` in una proposta) è un errore di traduzione. Le eccezioni, che non
  cambiano l'uscita: la riga della C5 dell'altra modalità (la decisione con gli export, la riga dell'export con `-dsn`)
  non si applica alla corsa, perché il piano la assegna all'altra modalità, e sta fra le parti «non applicabili» dei
  dettagli (R-117); nelle sezioni a forma libera le chiavi che non sono ID, e da B6c le chiavi descrittive delle sezioni
  e delle voci che il piano non fa confrontare, vanno in un elenco informativo, «chiavi libere non verificate», e il
  valore si riporta; le righe della «Conferma l'albero» stanno nella sezione informativa `righe_conferma_albero` (D-R116
  I); le parti fuori perimetro per una decisione dell'utente (D-R116 F) stanno fra le parti «fuori perimetro» dei
  dettagli. `scritture_consentite` dello scenario, un invariante del prodotto, la dichiara la voce del gate «zero
  scritture di dominio» (R-118). Accanto al percorso il rapporto porta l'`id` della voce: negli esiti, nella baseline e
  nell'indice `attesi.id_voci` (R-119).
- **Le chiavi della prima corsa vera** (B6c, 08/10: 37 chiavi non tradotte, ognuna con il suo trattamento, con le
  sigle T-B6c-nn dove il piano non lo scrive; nessuna ignorata):
  - nella voce: `thread_id` (baseline, C5), che deve essere il thread dell'allegato nella fotografia (n.2, T-B6c-14);
    `codice` dei file dello scenario, descrittiva: si riporta ed è fra le chiavi libere (il piano non lo fa confrontare,
    e il significato di un codice del cliente il runner non lo decide: T-B6c-04); `atteso` della C5 scritto come valore,
    l'esito del file, che deve essere `da_rivedere` (P-10), oppure come mappa di letture, giudicata come quelle dei
    reali (T-B6c-07, da ratificare);
  - dentro `atteso` (T-B6c-01): della baseline `componente_id_proposta_vuoto_preservato` (il (5) della baseline),
    `decisioni_invariate` (il controllo (1): vero lo verificano le righe, e senza righe resta non verificato; falso è un
    errore di traduzione) e `revisione_confrontata_separatamente` (la riga di confronto ha l'indicatore di revisione
    accanto al badge: E-19, R31; falso è una differenza); dei reali `target_presente` (R70 A) ed `esito_con_target`
    (null vuol dire «non ancora scritto»; scritto, la voce resta riservata e l'esito si riporta: T-B6c-03);
    `token_00_conservato` è `token_conservato` (6.4.9, «Traduzione degli attesi»; C-30), solo nelle sezioni di A1c
    (T-B6c-13);
  - nelle righe decise, i campi che la riga della fotografia porta (la regola delle chiavi accettate): `proposta_id`;
    del documento `thread_id`, `estensione`, `confermato_da` (la sigla, qui sotto) e la provenienza (T-B6c-02):
    `allegato_id` fra gli allegati della provenienza del documento (una differenza dove la fotografia l'avrebbe: con le
    provenienze complete, o con un allegato del thread del documento; altrimenti non si decide), `messaggio_id` come il
    messaggio, nella fotografia, dell'allegato della stessa riga (il prodotto scrive la provenienza con l'allegato e il
    suo messaggio; la fotografia non porta `documento_provenienza.messaggio_id`, e non c'è SQL nuovo); un null, o ciò che
    la fotografia non sa dire, è una parte non verificata;
  - nella C5 (T-B6c-08): `thread_id` (anche vuoto) e `cliente` con l'UUID vanno al n.2 (il thread si risolve, il
    cliente è il suo); `deciso_da` (anche vuoto) va al n.2, che lo cerca fra gli utenti della fotografia (C-05,
    T-B6c-15, proposta tecnica; il piano 6.5: `ListSigleUtenti` serve al `deciso_da` degli attesi C5); `anomalie`
    riportate (6.4.9, la C5 nel runner); `decisione`, `fonte`, `intervallo`, `regole`, `stato` e un `cliente` scritto con
    il nome (o vuoto) descrivono la sezione: riportati, fra le chiavi libere;
  - nello scenario: `fonte_quantita` della sezione vale per i prodotti che non la scrivono, e uno che ne scrive una
    diversa è un errore (T-B6c-05); `origine_atteso` riportata, fra le chiavi libere (T-B6c-06); i conteggi (T-B6c-09;
    6.4.9: «8 radice, 28 figlio (22 con due radici), 13 fuori richiesta») `candidati_radice` e `candidati_figlio` (i
    file attesi radice e figlio), `file_condivisi` (i figli con più di una radice), `allegati` (le voci, una per
    allegato) contro le voci (n.5) e, i primi tre, contro gli esiti corretti (voce 2 del gate); `hash_distinti` contro
    gli sha256 distinti degli allegati delle voci nella fotografia (n.2).
- **La sigla nei campi `deciso_da` e `confermato_da`**: la C5 li scrive con la sigla dell'utente (il piano: 6.5,
  `ListSigleUtenti`), la fotografia con l'UUID. Un valore atteso che è un UUID (o null) si confronta come prima. Con
  `-dsn` una sigla si confronta, alla lettera, con quella dell'utente della fotografia con quell'UUID (`fotorfq.Utente`):
  un campo assente, un utente che la fotografia non ha (anche se non ne ha nessuno: la copia li ha tutti) o una sigla
  diversa sono differenze. Con gli export `deciso_da` resta non confrontabile (R-102), e una sigla di `confermato_da`
  non si confronta: gli export non hanno gli utenti, ed è una parte non verificata. La regola è la stessa per ogni riga
  decisa confrontata, C5 e baseline (T-B6c-10). Nessun SQL nuovo.
- **Chi ha deciso la sezione C5** (`decisioni_successive_da_rivedere.deciso_da`; C-05, T-B6c-15): con `-dsn` una sigla
  deve essere, alla lettera, la sigla di un utente della fotografia, e un UUID l'ID di un utente; altrimenti è una
  differenza (anche con la fotografia senza utenti: con `-dsn` ci sono tutti). Un valore vuoto non nomina un utente ed è
  una parte non verificata. Con gli export gli utenti non ci sono: la parte è NON VERIFICATA (R-102), mai un passato. La
  sezione non dice quale riga abbia deciso: si verifica che sia un utente; le righe le verificano le voci.
- **`nell_export_proposte`**: il nome neutro della riga dell'export della C5 (il nome di prima portava un frammento del
  nome di un file dell'export; come M-21, solo lessicale: valori e significato invariati). La chiave di prima è una
  chiave non tradotta come le altre: nessun alias, e nel codice nessun altro nome `nell_export_…`.
- **Le letture attese dei file** della baseline (oltre a `base`, che è il controllo (2)) e dei file reali si
  confrontano con l'interpretazione del file, con le regole della tabella dei casi (R-101); `target_presente` con il
  predicato di R70 A; `nome_file` contro il nome dell'allegato (n.2); `mostrata_con_badge_di_confronto` contro le righe
  del confronto (C5). Una chiave con un ambito (`revisione_dal_nome`, `decorazione_nome_file`, `revisione_dal_token`,
  `revisione_da_token_base` sul nome del file; `identita_file_da_nota` sul testo del PDF) si giudica solo sulle letture
  di quell'unità, mai sul file intero (R-116): il nome si legge sempre per intero, quindi le chiavi del nome si
  giudicano anche con il documento parziale; senza letture di quell'unità, o con l'ambito «questo campo», la chiave è
  fra le parti non verificate. Le chiavi dei token (`token_revisione`, `token_conservato`, `identita_include_token`)
  non nominano un'unità: si giudicano sul solo ambito che la voce indica con le sue chiavi che ne hanno uno, se è uno
  solo e dice un'unità; altrimenti sono fra le parti non verificate, con il motivo, e mai sul file intero (T-B6-200).
  L'ambito è dedotto, non dichiarato (T-B6-220, precisata con R-140): un'asserzione negativa (false, null, la lista
  vuota) è fra le parti non verificate, «ambito dedotto, asserzione negativa», perché su un'unità più stretta del file
  passerebbe a vuoto; un'asserzione positiva si giudica, e i dettagli del controllo dicono su quale unità
  (`ambiti_dedotti`, nel riepilogo «ambito dedotto: …»), solo come informazione.
  Le chiavi senza ambito vogliono l'interpretazione completa. Le letture parziali restano obbligatorie (D-R116 O,
  decisa dall'utente l'08/10): si giudicano i valori disponibili, e il resto si dichiara parziale. La verifica minima:
  la base dei reali è quella del nome dell'archivio (il piano: 4.6.10, la forma `nome-token` su `nome_file`; 6.4.9, la
  tabella delle sezioni; A1c-L4D-10, gli archivi senza fatti), quindi prende l'ambito del nome (T-B6c-12), **solo se il
  nome è tutto il file** (C-04, che restringe T-B6c-12): ogni unità del documento è del nome, nessuna è troncata,
  nessun limite è superato (né nei limiti della fonte né fra le diagnostiche dell'interpretazione). Allora giudicare sul
  nome equivale a giudicare sul file intero, ed è il caso degli archivi senza contenuto. La sola presenza di una base nel
  nome non basta: con un contenuto letto in parte la chiave torna alla regola del file intero e resta NON VERIFICATA,
  con il motivo di A1b-22 e quello di C-04 accanto; lo stesso per `token_00_conservato`, che dalla base prendeva
  l'ambito. Le chiavi di unicità del file intero restano sul file intero, e con il documento parziale non si giudicano.
- **Il file dei casi contro la fotografia**, sempre: un thread o un messaggio di un caso che la fotografia non ha è
  una differenza (con `-dsn`: il file dei casi contraddice i dati); con gli export i messaggi che mancano per
  costruzione (solo quelli in entrata) sono parti non verificabili (T-B6-74, T-B6-112).
- **Gli attesi si leggono con le chiavi del piano** (6.4.6, 6.4.9), con i nomi neutri: il codice non ha mai letto gli
  attesi veri (P-11). Una chiave che il runner non conosce non si ignora: è una «chiave non tradotta», con il suo
  percorso senza valori, e il controllo n.5 la conta; la prima corsa sugli attesi veri le dice tutte. Le sezioni
  descrittive (`gate`, `riservati_non_bloccanti`, `prerequisiti_gate_l4`) si riportano riga per riga; da
  `casi_integrazione` e dalla conferma dell'albero si raccolgono solo gli ID, per il n.2.
- **Le contraddizioni di una voce** non arrivano a `confronto`: una voce `radice` o `figlio` senza la base del target
  (D8), un `figlio` senza radici (in `confronto` «vuoto» vuol dire «nessuna radice»: R-51), una radice attesa vuota,
  un valore fuori elenco sono errori di traduzione, cioè differenze.
- **Un file in due sezioni** è una differenza del n.2, e il gate lo conta una volta sola (D7).
- **L'impronta**: un'impronta vuota di `confronto` vuol dire «non calcolata», ed è una differenza (R-48); l'impronta
  dell'esito di `valutazione` coincide con quella dell'anteprima solo con un thread solo (F0-14).
- **Il gate** ha sette voci; quelle che il runner non verifica da sé (gli esiti di A1c-L4D-12, A1c-L4S-08 e -09,
  A1c-L4T-01, G1, G5, che stanno nel registro; le forme dei profili ancora senza etichette, R34 c) restano NON
  ESEGUITE: con `-gate` l'uscita è 3 finché mancano (R58 B). La C5, i non coperti e i thread non valutati stanno a
  parte.
- **La classe e l'esito di ogni controllo** (R116 B, precisata dall'utente il 07/10, domande-a1c.md; `classi.go`):
  ogni controllo e ogni voce del gate del rapporto versione 3 dice se è `obbligatorio`, `obbligatorio_esterno`
  (verificato fuori dal runner, con la prova che lo chiude), `informativo` o `fuori_perimetro` (con la motivazione), e
  se è passato, fallito o non eseguito (l'esito si deriva dallo stato). La classificazione segue la revisione
  d'impatto delle risposte (R116 §3):
  - obbligatori del runner: il manifest e le sue voci, le grammatiche, la copia, la sola lettura, la fotografia o gli
    export, la valutazione, i casi e la fotografia, i controlli del runner 1–5, la traduzione, la baseline, la C5, le
    letture, il confronto, i casi di contratto; le voci del gate 1, 2, 3 e 5;
  - obbligatori esterni: le sentinelle (A1c-L4D-01), le voci del gate «zero scritture» (A1c-L4D-12, A1c-L4S-09),
    «motore senza LLM» (A1c-L4S-08, A1c-L4T-01, G1, G5) e «forme dei profili» (le etichette, M-22, R58 B);
  - informativi: le sezioni prodotti, correzioni manuali, profilo dei limiti, censimento, parti non applicabili,
    chiavi libere, righe della «Conferma l'albero» (`righe_conferma_albero`);
  - fuori dal perimetro: PO-30 (E1 §11; R110, precisata dall'utente il 07/10); `clienti_degli_export` (D-R115 A); con
    gli export, la parte del n.1 senza i finiti (`runner_1_finiti_con_gli_export`, D-R116 F), elencata nella chiusura
    solo quando la corsa ne ha;
  - le decisioni dell'utente dell'08/10 sulle parti che erano «da classificare», ognuna un'esclusione specifica e
    motivata, ratificata, come le sentinelle: D-R116 I, le righe legacy della «Conferma l'albero» sono informative, e
    gli invarianti della C5 restano obbligatori per i file confermati; D-R116 O, le letture parziali restano
    obbligatorie (si giudicano i valori disponibili, il resto si dichiara parziale); D-R116 F, con gli export il n.1
    senza i finiti è fuori perimetro, coerente con D-R115 A (la parte sta fra le parti «fuori perimetro» dei dettagli,
    e il motivo del n.1 le conta: un n.1 passato con una parte esclusa non si legge come passato per intero, T-B6c-11);
    D-R115 A, `clienti_degli_export` è fuori perimetro, con la non coerenza degli identificativi nel motivo, lo stato NON
    ESEGUITO e l'uscita 3;
  - una voce che la tabella non conosce è obbligatoria per difetto: nessuna esenzione generica.
- **La chiusura** (`Chiusura`, R116 B) distingue la conclusione della parte obbligatoria del runner dall'incompletezza
  del rapporto: gli obbligatori del runner, conclusi o no; gli obbligatori esterni, ognuno con la sua prova, da
  chiudere nel registro; gli informativi incompleti; i fuori perimetro con la motivazione e, per un controllo, l'esito
  della corsa. Né le classi né la chiusura cambiano l'uscita: una differenza resta 1, un controllo non eseguito resta 3,
  e il 3 non diventa mai 0. Il rapporto è completo solo senza nessun controllo NON ESEGUITO, di nessuna classe: un
  controllo fuori perimetro e non eseguito (`clienti_degli_export`, D-R115 A) esce dagli obbligatori del runner, ma il
  rapporto resta incompleto (T-B6c-11). Le righe informative della «Conferma l'albero» (D-R116 I) e le chiavi libere
  restano fra gli informativi incompleti: sui dati veri `rapporto_completo` resta falso anche con tutte le parti
  obbligatorie verificabili concluse. È la scelta prudente decisa per B6c (D-1): una parte informativa non verificata
  non diventa «superata», e la chiusura non ha un predicato diverso. L'unica riga
  che non decide l'uscita è quella delle sentinelle, «delegato», un'esclusione specifica e motivata (R117 b): vale solo
  per l'ambiente della copia intatta (il database della sezione copia del manifest); senza quel database dichiarato
  resta NON ESEGUITA.
- **Le correzioni manuali per cliente** sono sui thread valutati; i thread non valutati stanno a parte (D5). «Deciso»
  vuol dire che un documento confermato porta il file (T-B6-52). La misura è quella di `confronto` (R114, precisata
  dall'utente il 07/10), sommata campo per campo: un indicatore ricostruito delle correzioni necessarie, non il tempo
  risparmiato, con il denominatore (i valutabili), la copertura (valutabili su decisi) e gli esclusi per motivo (che
  hanno preso il posto del vecchio «prima per stringa»); il titolo «prima» del riepilogo è base più marcatore
  (`prima + prima_marcatore`, con le due parti accanto), e il marcatore da un solo lato resta a parte (D-R114 B, decisa
  dall'utente l'08/10: solo il testo; nel JSON i due conteggi restano separati); «dopo» ha tre parti: false
  associazioni, ambiguità, astensioni. Fuori dalla misura il
  rapporto conta la stessa base su un altro target (su tutti i decisi) e la revisione vecchia solo nella colonna rev
  (R-65), divisa per lo stato della lettura della colonna con la regola della famiglia (R113 B ratificata;
  `RevisioneInColonna`): letta, nessuna regola, non interpretabile, ambigua, e a parte i file non valutati il cui stato
  non è nel record piatto.
- **La revisione del vecchio motore** (R113 B ratificata; E2 §2.6): valutazione la interpreta (dal codice o dalla colonna
  rev, con `motorea.LeggiRevisioneRegistrata`) e ne dice la provenienza nel gemello `RevisioneDa`, che `passaggio.go`
  copia in `confronto.Vecchio` (19 campi, A1c-L1-31); l'indicatore di revisione la porta in `provenienza_vecchia`. Il
  banco non interpreta niente: copia e conta. La versione della lettura della colonna sta fra le versioni del rapporto.
- **Il motivo di un esito** contro l'atteso si divide sulle virgole (T-B6-53: `ambiguo` può portarne due).
- **Le diagnostiche** stanno in quattro sedi, tutte nel rapporto: la fotografia (il caricatore o il lettore degli
  export: non entrano nell'esito, D-V1-2), l'esito, il thread, gli ancoraggi; il conteggio per codice le somma.
- **La sezione `prodotti`** è solo informazione: con `-exports` un asse è «non calcolato» dove manca una sezione della
  fotografia da cui dipende (T-12). Con `-attesi`, la fonte contro l'atteso dei prodotti attesi dello scenario (PO-29,
  `prodotti.fonti_scenario`). R109, precisata dall'utente il 07/10, senza scegliere una lettera; E2 §2.3. Lettura [T]:
  la regola del runner (la A del testo della domanda) resta valida, con la derivazione dagli attesi esplicita e
  indipendente dal motore:
  - si scorrono i prodotti attesi (le basi dei prodotti attesi e le basi del target delle voci radice), non quelli del
    motore; un prodotto atteso senza calcolato è una differenza, e i prodotti del motore senza atteso sono elencati;
  - la regola (`RegolaFonteAttesa`, `po29-radici-step-1`): uno STEP fra le voci radice risolte del target dà
    `in_attesa_di_conferma` con `documento_candidato` e quegli STEP come candidati; nessuno STEP dà `assente`, e il
    motivo, che gli attesi non fissano, il rapporto lo dice («motivo non fissato dagli attesi») invece di confrontarlo;
    mai confermata, nessuna BOM di lavoro;
  - la derivazione nel rapporto: la regola, i prodotti attesi e le sole voci radice del target; una voce radice non
    risolta rende la derivazione incompleta, e lo dice;
  - i livelli distinti: presente (attesi e fotografia), estrazione riuscita (i fatti della fotografia: dati d'ingresso),
    struttura (l'asse della gerarchia del prodotto calcolato, che misura la «struttura corretta» della risposta:
    «calcolato, non confrontato», R-144), associazione (i candidati del motore), autorizzazione come fonte (lo stato
    della fonte), verifica operativa (mai in A1c);
  - le parti non verificate (il motivo non fissato, le radici non risolte, lo scenario che la corsa non verifica) la
    chiusura le mette fra gli informativi incompleti; le differenze sono informazione, fuori dal gate.
- **Con `-exports` la ragione sociale del cliente** viene dall'export dei clienti, per UUID: senza, la grammatica non si
  può controllare e il thread non si valuta (`ragione_sociale_discorde`). È un limite degli ingressi, non del motore:
  il controllo `clienti_degli_export` è NON ESEGUITO, e per quei thread ciò che dipende dalla valutazione (la base
  della baseline, il predicato del n.1, le letture, gli esiti nel gate) è fra le parti non verificate, mai fra le
  differenze: l'uscita è 3 (T-B6-104). R115 A, ratificata dall'utente il 07/10 con un vincolo: un export nuovo non si
  mescola al campione del 02/10, che resta con questo limite dichiarato. D-R115 A, decisa dall'utente l'08/10: il limite
  è accettato (l'export dei clienti è di prima della ricreazione del DB, con UUID diversi; non è un difetto del motore
  né delle migrazioni); il motivo del controllo registra la non coerenza degli identificativi, con il numero dei
  thread; la classe è `fuori_perimetro`, lo stato resta NON ESEGUITO e l'uscita 3. Gli export non si toccano, nessun
  collegamento fra UUID si crea, gli attesi non cambiano.
- **Il gate non dà mai per superato ciò che non ha visto**: una voce dello scenario o della baseline non risolta o non
  verificabile rende non eseguite le false associazioni e le decisioni preservate (una decisione con una parte non
  verificata non è preservata, R-102); un prodotto in più del motore non supera i conteggi (R-108); una voce della C5
  riservata non è un riservato «passato» (R-110). Con `-attesi` e senza `-gate` un gate non superato non decide
  l'uscita, ma la prima riga lo dice.
- **Le decisioni preservate, con le differenze di revisione diagnosticate** (R113 B ratificata; E2 §3.4): ogni voce
  della baseline porta l'indicatore di revisione del suo file (`attesi.baseline[].revisione`, con la provenienza della
  revisione vecchia), e la voce 3 del gate conta le differenze diagnosticate (le revisioni diverse e le revisioni
  vecchie discordi), con le uguali, le altre non determinabili e le voci senza riga (`gate.revisioni_baseline`). Non
  tolgono la preservazione e non cambiano lo stato della voce: l'indicatore di revisione resta separato dalla
  correttezza dell'associazione, e la cautela sul vecchio motore non indebolisce le decisioni confermate.

## Dipendenze

- **Importa:** `platform/dataset`, `core/registro/regole/grammatica`, `core/inbox/classificazione/motorea`,
  `core/estrazione/evidenze`, `core/estrazione` (da A1b.11, solo `DaTesto`: F19, R52 A), `platform/jsoncanonico`,
  `github.com/google/uuid`, `gopkg.in/yaml.v3` (unico importatore), la libreria standard. Da A1c anche
  `core/fotorfq`, `core/fotorfq/caricatore`, `platform/migrazioni`, `core/valutazione`, `core/confronto`.
- **Non importa:** `app/runtime`, `transport/*`, `ai/*`, `platform/config`, `platform/db`, `core/ancoraggio`, pgx per
  nome, la radice del modulo (le migrazioni incorporate le dà `cmd/bancoa`).
- **Solo nelle prove:** `core/ancoraggio` (la prova di parità dei valori del motore, T-B6-50).
- **È importato da:** `cmd/bancoa`.

## Test

- Nel ramo `-qa` (piano A, par.4.3, Q5), tutti L1 e sintetici, con il cliente inventato ACME e i file di
  `testdata/` dai nomi `_acme` (`attesi_acme.yaml`, `regole/indice_acme.v1.json`, `regole/acme.v1.json`,
  `manifest_acme.json`), copiati in `t.TempDir()` con gli sha256 calcolati sui byte copiati (A1a-BA):
  - **`attesi_test.go`** — la testata e i casi; chiavi sconosciute, ripetute o fuori posto; il testo dei valori
    come è scritto; le sezioni non lette;
  - **`traduzione_test.go`** — i contesti (R19); la tabella delle chiavi, completa e neutra; ogni chiave di A1a
    con il valore giusto e con uno sbagliato; la lista attesa vuota («nessuno»); le chiavi decadute; da A1b.11
    le chiavi del router e degli attributi (A1b-24), la funzione del banco uguale a quella di Interpreta;
  - **`casi_test.go`** — gli esiti dei casi ACME; il caso fallito con atteso, ottenuto e regola; profilo
    senza cliente, cliente scartato, contesto illeggibile; da A1b.11 le precondizioni (anche da sole, mai un
    passato), il testo che DaTesto rifiuta e l'interpretazione parziale o non disponibile, che non si
    giudica; il determinismo;
  - **`regole_test.go`** — il rapporto delle regole; la coerenza con `rif_caso`, con la regola del runner per
    `letture_identita`, `basi` e la funzione del selettore; senza attesi; cliente scartato
    e indice non valido;
  - **`rapporto_test.go`** — esito e codice d'uscita, prima riga, scrittura atomica fuori dal modulo, gli esiti
    di `Esegui` (conforme, con differenze, non eseguito, 1 su 3), gli errori d'uso senza rapporto.
- Da A1c (Q9), sulla scena ACME di `scena_banco_test.go` (export sintetici con la forma di quelli veri, file dei casi
  `testdata/regole/casi_acme.v1.json`, indice con il puntatore ai casi, attesi con tutte le sezioni):
  - **`export_test.go`** — A1c-L1-22: `FotografiaDaExport`, le sezioni dichiarate, l'HTML «non esportato», le
    proposte senza dettagli, i fatti a un'altra terna, gli sha256 sbagliati, i file mancanti;
  - **`sezioni_test.go`** — A1c-L1-23: `LeggiSezioni`, le chiavi non tradotte, i valori non letti, la traduzione e
    i suoi errori (D8, R-51);
  - **`runner_test.go`** — A1c-L1-24: i controlli del runner 1–5, da soli e nella corsa;
  - **`gate_test.go`** — A1c-L1-25: le voci del gate e le uscite con `-gate`;
  - **`rapporto_banco_test.go`** — A1c-L1-26: il rapporto (versione 3), la prima scrittura, nessun testo di mail a
    video, la stabilità;
  - **`passaggio_test.go`** — A1c-L1-31 (19 campi nel vecchio, con `CodiceLettoMarcatore` e `RevisioneDa`; il
    marcatore arriva alla misura) e la parità dei valori del motore (T-B6-50), anche per la misura di R114;
  - **`banco_test.go`** — le cinque condizioni di F0-01 e le regole d'uso, la sequenza `-exports`, la sequenza `-dsn`
    fino al collegamento;
  - **`sezioni_rapporto_test.go`** — le sezioni del rapporto una per una (messaggi fuori RFQ, motivo diviso,
    correzioni manuali e il testo della misura, `senza_caso`, censimento, profilo dei limiti, R109, 1 su 3);
  - **`risposte_test.go`** — il banco dopo le risposte dell'utente del 07/10: la tabella delle classi e la sua
    completezza sul sorgente, la classe e l'esito di ogni controllo e voce del gate, la chiusura, le uscite che non
    cambiano, il JSON di A1a identico (R116); le sentinelle delegate, con le impronte nel testo della riga (R117,
    T-B6-219); la misura sugli export (R114);
  - **`revisione_test.go`** — le correzioni della revisione di P9 (R-101…R-115) e i pareri che le accompagnano (gli
    export con i clienti di un altro DB, i due ingressi incoerenti, il n.3 nelle due direzioni), una prova per
    correzione; da B6b le chiavi dei token sull'ambito della voce (T-B6-200) e, con l'ambito dedotto, le asserzioni
    negative fra le parti non verificate e l'ambito nel rapporto (R-140);
  - **`fonti_scenario_test.go`** (B6b) — R109: i prodotti attesi scorsi al posto di quelli del motore, il prodotto
    atteso senza calcolato, l'«assente» senza motivo fissato, la derivazione, i livelli, lo scenario non verificabile,
    la scena;
  - **`revisione_colonna_test.go`** (B6b) — R113, la parte del banco: la colonna per stato, le differenze di revisione
    della baseline nel gate, la provenienza da valutazione alla baseline, la scena;
  - **`prima_corsa_test.go`** (B6c) — ciò che la prima corsa vera chiede al banco: le 37 chiavi della corsa tutte nella
    forma della prova, lette e verificate sulla scena, ognuna con la sua differenza quando cambia; i conteggi della
    corsa nel gate; `nell_export_proposte` senza alias; la sigla nei campi `deciso_da` e `confermato_da`, con `-dsn` e
    con gli export; la provenienza del documento (T-B6c-02); le righe della «Conferma l'albero» informative (D-R116 I);
    la base dei reali dal nome e `token_00_conservato` (D-R116 O), solo se il nome è tutto il file (C-04); chi ha deciso
    la sezione C5 fra gli utenti (C-05); i campi nuovi del rapporto facoltativi, con la versione 3 (D-6); il n.1 dei
    finiti fuori perimetro con gli export
    (D-R116 F); i clienti degli export fuori perimetro con l'uscita 3, e il rapporto mai completo con un controllo non
    eseguito (D-R115 A, T-B6c-11). Il titolo della misura (D-R114 B) è in `sezioni_rapporto_test.go`, la tabella delle
    classi in `risposte_test.go`;
  - **`banco_db_test.go`** (tag `integrazione`, L4) — la sequenza `-dsn` sul DB di prova, fermata dal ruolo che
    scrive, con le tabelle escluse «non controllate».
- `cmd/bancoa/main_test.go` prova flag e codici d'uscita del comando (0, 1, 2, 3); da A1c anche A1c-L1-27 (i flag
  dei modi nuovi, il DSN senza password, l'aiuto con i soli segnaposti).
- Le corse sul dump e sulla copia `_run` (A1c-L4D, A1c-L4T) sono di Q10.
- Il controllo che la libreria YAML stia solo qui (G3) e quello che di `core/estrazione` il banco usi solo
  `DaTesto` (F19) sono in `core/estrazione/evidenze/dipendenze_test.go`.

## Leggi anche

- `internal/app/README.md`
- `internal/platform/dataset/README.md`
- `internal/core/inbox/classificazione/motorea/README.md`
- `internal/core/estrazione/README.md`
- `internal/core/registro/regole/grammatica/README.md`
- `internal/README.md`
