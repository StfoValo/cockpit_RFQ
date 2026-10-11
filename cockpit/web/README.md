---
markmap:
  initialExpandLevel: -1
  maxWidth: 520
  colorFreezeLevel: 2
---

# `web/` — i template HTML e i file statici del browser

## Scopo

- Tutto quello che il browser riceve oltre ai dati:
  - i template `html/template` delle schermate e dei loro frammenti HTMX,
  - e i file statici — il foglio di stile, htmx, il modulo del Fascicolo v3 e la copia di pdf.js.
- Viaggiano dentro `cockpit.exe` (`embed.go`):
  - per aggiornare la UI si sostituisce il binario,
  - e nessun file arriva da un CDN.

## Non appartiene qui

- **I dati e le decisioni.**
  - Li prepara `internal/transport/web` (i tipi `*Dati` e i metodi che i template chiamano) e li decide
    `internal/core`.
  - Un template legge, non calcola: che colore ha una card, quante cose restano da decidere, lo calcola Go (per
    esempio `fascicolo_bom.go:riempiComponente`).
- **Un JavaScript che decide che cosa mostrare.**
  - Il server resta la verita':
    - il JS decide solo che cosa chiedere (`layout.html:cockpitStatoInbox`)
    - e tiene lo stato di chi guarda (componente scelto, pagina, zoom) in `fascicolo.mjs`.
- **La lettura del corpo di una mail** (testo automatico, tabelle, storia citata):
  - la fa `internal/core/inbox/lettura`;
  - i template `corpo_messaggio` e `corpo_blocco` disegnano soltanto il modello che ne esce.
- **Risorse esterne:** niente CDN, niente font scaricati.

## File

### Template (`web/templates/`)

- **`layout.html`**
  - Che cosa contiene: la cornice (`layout`):
    - `<head>` con `style.css` e `htmx.min.js` presi con l'impronta (`{{statico}}`),
    - la testata con lo stato dei worker (`stato_worker`) e il form «esci» (`#esci`),
    - la rail di navigazione (`vista.Nav` → `navigazione.go:navPer`),
    - `{{template "contenuto"}}`.
  - Uno script in linea con cinque pezzi:
    - la rail chiusa (localStorage `cockpit.rail.chiusa`);
    - la guardia `htmx:beforeSwap` che non innesta risposte 2xx non HTML;
    - la pulizia delle bozze dell'editor (`cockpit.editor.*` nel sessionStorage) all'invio di «esci»;
    - lo scorrimento della lista Inbox tenuto fermo durante il poll;
    - `cockpitStatoInbox` (il poll rilegge i parametri dalla barra degli indirizzi)
  - Chi lo rende: tutte le pagine intere
- **`frammenti.html`**
  - Che cosa contiene: i frammenti condivisi:
    - `messaggio_pannello` (oggetto con `oggettoVisibile`, corpo con `corpo_messaggio`);
    - `corpo_messaggio` e `corpo_blocco` (il corpo di una mail da `lettura.Corpo`: anche nella pagina RFQ);
    - `allegati_tabella`, `allegato_riga`, `conferma_form` (gli allegati di un messaggio o di una RFQ: scarica,
      anteprima, conferma, scarta, riscarica);
    - `triage_form`, `triage_allegati`, `cartella_cliente`, `cliente_nuovo`, `anteprima_cartella`,
      `buyer_select`, `thread_risultati`, `candidati_codice` (spunta iniziale da `triageDati.Spuntato`),
      `candidati_aggancio`, `chip_controparte` (cliente, fornitore, altro, interna, ambiguo, non censito),
      `censisci_form`;
    - `stato_worker`;
    - `codici_rfq` e `codice_riga` (i codici della richiesta, B8.6: anche nel cassetto del Fascicolo)
  - Chi lo rende: `Server.frammento` (con il set di `inbox.html`), le pagine che li includono
- **`inbox.html`**
  - Che cosa contiene: l'Inbox a quadranti:
    - `inbox_stato` (linguette, direzione, filtro, casella, contatori e righe in un pezzo solo),
    - `inbox_lista` (oggetto con `oggettoVisibile`),
    - `controparte_riga`;
    - il poll di 15 s sta su un elemento fratello;
    - `sel` (sempre un UUID o vuoto) nell'`hx-get` del pannello e nei collegamenti
  - Chi lo rende: `routes_inbox.go`
- **`thread.html`**
  - Che cosa contiene: la pagina della RFQ:
    - `thread_corpo`;
    - per ogni messaggio l'oggetto con `oggettoVisibile` e il testo, chiuso in «testo», con `corpo_messaggio`
  - Chi lo rende: `thread.go`
- **`fascicolo.html`**
  - Che cosa contiene: la schermata del Fascicolo (B8.7, B8.7b, v3): vedi la tabella sotto
  - Chi lo rende: `fascicolo_rotte.go`, `fascicolo_documenti.go`, `fascicolo_nas.go`, `fascicolo_preparazione.go`
- **`distinta.html`**
  - Che cosa contiene: la Distinta, una RFQ in quattro passi (Richiesta, Distinta, Documenti e NAS, Fattibilita'):
    - `distinta_corpo` (il corpo che i gesti rifanno, con il JSON `#dst-dati`), `dst_richiesta`, `dst_distinta`,
      `dst_documenti`, `dst_riga_file`, `dst_fattibilita`;
    - fuori dal corpo il visore dei disegni (`#dst-visore`) e l'avviso breve (`#dst-toast`), che un gesto non chiude
  - Chi lo rende: `distinta.go`
- **`motore_a.html`**
  - Che cosa contiene: la Distinta nuova del motore A (giro 5, A1d; solo l'admin, dietro `[motore_a].anteprima`), un
    solo workspace per `vista=distinta` e `vista=documenti`, il riepilogo per `vista=rfq`:
    - `contenuto`: `#ma-avviso` (fuori da `#ma`: la rilettura non lo cancella), `#ma` con gli indirizzi di pdf.js
      (`data-ma-pdfjs`, `data-ma-pdfjs-worker`), il foglio `motore_a.css`, l'importmap del ponte e il modulo
      `motore_a.mjs` (vedi «Statici, impronte e cache»);
    - `motore_a_corpo`: il frammento che htmx rimette in `#ma`, un solo elemento di primo livello, la `.ma` con
      `data-ma-thread`, `data-ma-prodotto`, `data-ma-vista`;
    - 61 `define` con il prefisso `ma_` (testa e cartiglio, viste e prodotti, conferma della fonte, conferma
      dell'albero sopra le schede, albero a schede, anteprima persistente, conflitti, «Da smistare», riepilogo
      documentale, riepilogo della RFQ);
    - legge solo `.Dati` (`paginaMotoreA`: l'esito, il confronto, le ricerche di `Indice`); ogni valore di enum passa da
      `{{etichetta "<dominio>" …}}`; i gesti sono `data-ma-gesto` (li manda il ponte), le letture `hx-get`; con
      `Scrive` falso ogni gesto e' spento
  - Chi lo rende: `motore_a.go:rendiMotoreA`
- **`richieste.html`**
  - Che cosa contiene: la pagina Richieste:
    - `richieste_elenco`, `richiesta_card`, `richiesta_prodotti`;
    - la barra dei filtri e' un form GET vero
  - Chi lo rende: `panoramica.go`
- **`anagrafica.html`**
  - Che cosa contiene: Admin › Anagrafica, clienti (`anagrafica_corpo`);
    - nel form delle famiglie di codice la casella «Rev nel codice» porta il numero della sua riga (`fam_rev`)
  - Chi lo rende: `anagrafica.go`
- **`fornitori.html`, `importa.html`**
  - Che cosa contiene:
    - Admin › Anagrafica › Fornitori (`fornitori_corpo`)
    - e l'import del seme dei fornitori con anteprima e conferma (`importa_corpo`)
  - Chi lo rende: `fornitori_admin.go`
- **`integrita.html`**
  - Che cosa contiene: Admin › Integrita' NAS (`integrita_corpo`)
  - Chi lo rende: `integrita_admin.go`
- **`job.html`, `scarti.html`**
  - Che cosa contiene: Admin:
    - la coda dei job (`job_tabella`, poll 5 s)
    - e gli scarti dell'ingest (`scarti_tabella`, poll 10 s);
    - nel poll torna lo `stato` o l'`origine` che il server ha riconosciuto, mai il testo dell'indirizzo
  - Chi lo rende: `routes_admin.go`
- **`postazioni.html`**
  - Che cosa contiene: Admin › Postazioni (`postazioni_elenco`)
  - Chi lo rende: `postazioni_admin.go`
- **`login.html`**
  - Che cosa contiene: il form di accesso
  - Chi lo rende: `server.go:loginForm`
- **`vietato.html`**
  - Che cosa contiene: il 403:
    - `vietato` (frammento) e la pagina;
    - dice il ruolo che servirebbe (`vista.Serve`), o nessuno quando il divieto non dipende dal ruolo
  - Chi lo rende: `ruoli.go:nega`

#### `fascicolo.html`, per pezzi

- **risposte** — `define`:
  - `fasc_corpo` (la pagina),
  - `fasc_parti` (gesto o navigazione: l'avviso, e fuori banda testata, avanzamento, vista, piano, cassetto, testa
    del pannello di destra; il corpo del pannello solo se `RifaiCorpo`),
  - `fasc_avanzamento_risposta` (il poll),
  - `fasc_anteprima`,
  - `fasc_vista_frammento` (la ricerca),
  - `fasc_cassetto_frammento` (il NAS),
  - `fasc_doc_sezione_frammento`
- **testata** — `define`:
  - `fasc_testata` (briciole, linguette Documenti / Struttura BOM / Completezza, menu ••• delle viste di servizio,
    «+ Aggiungi file», «Da verificare»),
  - `fasc_bom` (versioni della BOM, Congela, Apri/Abbandona la revisione),
  - `fasc_avanzamento`,
  - `fasc_avviso`
- **area principale (`fasc_vista`)** — `define`:
  - `fasc_docv`, `fasc_doc_sezione`, `fasc_doc_file` (la vista Documenti v3, con `#doc-stage` e il JSON
    `#doc-indice`);
  - `fasc_tela`, `fasc_carta` (Struttura BOM);
  - `fasc_completezza`, `fasc_griglia`;
  - viste di servizio `fasc_documenti` (Elenco file), `fasc_schede` (Componenti), `fasc_albero`, `fasc_nodo`,
    `fasc_figlio_proposto`, `fasc_blocchi`
- **pannello di destra (tutte le viste tranne Documenti)** — `define`:
  - `fasc_anteprima_dentro`,
  - `fasc_anteprima_testa` → `fasc_file_testa` / `fasc_dettaglio_testa` (con `fasc_scheda`, i gesti sul
    componente) / `fasc_prop_testa`;
  - `fasc_anteprima_corpo` (l'iframe del PDF, il riepilogo dell'analisi di un modello, il campo `#corpo-chiave`)
- **in fondo e cassetti** — `define`:
  - `fasc_piano` («Conferma Fascicolo»),
  - `fasc_cassetto` → `fasc_verifica`, `fasc_rivedi`, `fasc_nas`, `fasc_nas_voce`, `codici_rfq`, gli avvisi;
  - `fasc_voce_gesti` (i gesti di una voce del piano, gli stessi nel cassetto e nel pannello)

### Statici (`web/static/`)

- **`style.css`** — che cosa e':
  - un foglio solo, per blocchi:
    - guscio, corpo della mail, Inbox, Anagrafica, controparte e richieste, codici (B8.6),
    - Fascicolo B8.7b (variabili `--f-*` su `.fasc`),
    - Richieste,
    - Fascicolo v3 (stage, note, editor, schermo intero).
  - Il corpo della mail:
    - `.corpo-vista` (il riquadro, piu' basso dentro `.msg-thread`),
    - `.corpo-testo`,
    - `.corpo-link` e `.corpo-img` (in grigio),
    - `.corpo-tabella` (con `td.num` allineato a destra),
    - `.corpo-rumore`,
    - `.corpo-storia`, `.corpo-originale` (i `<details>` chiusi),
    - `.corpo-sep`,
    - `.nota-corpo`.
  - Tiene `[hidden]{display:none!important}` e `.fasc.sola-lettura .gesto{display:none!important}`
- **`htmx.min.js`** — che cosa e': htmx 2.0.4, minificato
- **`fascicolo.mjs`** — che cosa e':
  - il modulo ES del Fascicolo v3:
    - il visore della vista Documenti,
    - le note sul disegno,
    - l'editor della struttura (vedi i flussi)
- **`distinta.css`** — che cosa e':
  - lo stile della Distinta, tutto sotto `.dst` e con il prefisso `dst-`: fondo bianco, grigi neutri, colore solo per
    lo stato e per il tipo di pezzo; nessun font da internet
- **`distinta.mjs`** — che cosa e':
  - il modulo ES della Distinta:
    - lo schema della distinta (stesso stato e stessa `StrutturaVoluta` dell'editor di `fascicolo.mjs`, con la regola
      «niente sotto un particolare» anche nel trascinamento),
    - le miniature dei disegni (pdf.js, la prima pagina, una volta per file),
    - il visore con le note (`/fascicolo/nota`, `/nota/{id}/modifica`, `/nota/{id}/elimina`)
- **`motore_a_gesti.mjs`** — che cosa e':
  - il ponte dei gesti della Distinta nuova (giro 5, A1d; del backend, API fissa: `esegui`, `leggi`, `confermaAlbero`,
    `aggiorna`); l'unico punto che chiama le rotte di scrittura dalla pagina `motore-a`; lo carica `motore_a.mjs`
    attraverso l'importmap di `motore_a.html`, mai un altro `<script>`
- **`motore_a.css`** — che cosa e':
  - lo stile della Distinta nuova, tutto sotto `.ma`, `#ma-avviso` e `.ma-finestra` (le finestre stanno in `body`):
    token della pagina, il workspace con le container query (tre colonne da 1180 px, due da 860 px, sotto una e
    l'anteprima come foglio dal basso), schede, albero, miniature; nessun `url(`, nessun font da internet
- **`motore_a.mjs`** — che cosa e':
  - il modulo ES della Distinta nuova, solo presentazione: schede e rami dell'albero, la casella scelta e
    l'anteprima persistente (`sessionStorage`), le miniature con pdf.js (una GET di `/allegato/{id}/anteprima?v=…`
    per disegno, al piu' due insieme), le finestre dello STEP strutturale, del tipo e di «Conferma l'albero» (con le
    letture del ponte), la tastiera, il fuoco dopo la rilettura di `#ma`. Non scrive: ogni gesto passa dal ponte
- **`pdfjs-6.3.289/`** — che cosa e':
  - pdf.js dal pacchetto npm `pdfjs-dist` 6.3.289 (Apache-2.0), senza modifiche:
    - `pdf.min.mjs`, `pdf.worker.min.mjs`,
    - `wasm/` (openjpeg, jbig2, qcms con le loro licenze e i ripieghi `*_nowasm_fallback.js`; senza quickjs),
    - `iccs/`, `cmaps/`, `standard_fonts/`, `LICENSE`,
    - e `VERSIONE.txt` con la provenienza, l'integrita' del pacchetto e lo sha256 di ogni file.
  - Niente mappe `.map`.
  - Circa 5 MB

## Entry point

- **`embed.go`:** `//go:embed … web/templates/*.html web/static/* …` → `cockpit.FS`.
  - `web/static/*` prende anche le sottocartelle (tranne i nomi che cominciano per `.` o `_`; in
    `pdfjs-6.3.289/` non ce ne sono).
- **`internal/app/runtime/ascolto.go:Ascolta`:**
  - `fs.Sub(FS, "web/templates")` e `fs.Sub(FS, "web/static")` → `web.Server.Templ` e `web.Server.Static`,
  - poi `Server.Init`.
- **`internal/transport/web/server.go:Init`** compila i template e calcola le impronte;
  - `Registra` monta `GET /static/` con `statici()`, senza autenticazione.
  - `server.go:funzioni` da' ai template, fra le altre:
    - `oggettoVisibile` (l'oggetto senza le etichette di posta esterna, `lettura.OggettoVisibile`: lista,
      pannello, pagina RFQ)
    - e, da `Init`, `statico`.
- **I template si eseguono da:**
  - `Server.rendi` (pagina o frammento),
  - `Server.frammento` (set di `inbox.html`),
  - `fascicolo_rotte.go:eseguiFascicolo`
  - e dai gestori che chiamano `pagine[…].ExecuteTemplate`.

## Flussi principali

### Come nasce una pagina

1. `Init` costruisce un set base `layout` = `layout.html` + `frammenti.html`, con le funzioni di
   `server.go:funzioni` piu' `statico`;
   - poi, per ognuna delle sedici pagine elencate in `Init`, un `Clone` del base con il file della pagina.
   - Ogni pagina ha il suo `contenuto`: un nome di `define` deve essere unico solo fra `layout.html`,
     `frammenti.html` e il file della pagina.
2. `rendi(w, r, pagina, frammento, …)`:
   - con `HX-Request: true` (e senza `HX-History-Restore-Request`) esegue il frammento;
   - altrimenti `layout`, e solo allora calcola la testata (`vista.Stato`).
3. Un file nuovo in `web/templates/` non si carica da solo: va aggiunto all'elenco di `Init`.

### Statici, impronte e cache

- **Impronte:** `Init` calcola l'impronta dei file della radice di `static/` (non delle sottocartelle):
  - sha256, primi 6 byte in esadecimale.
  - `{{statico "fascicolo.mjs"}}` → `/static/fascicolo.mjs?v=<impronta>`;
  - un file senza impronta torna senza `?v=`.
- **`statici()`:** si servono file, non cartelle:
  - la radice di `/static/` e ogni cartella (anche `pdfjs-*/`, con o senza la barra finale) rispondono 404 invece
    dell'elenco del contenuto.
  - `Cache-Control: public, max-age=31536000, immutable`
    - per cio' che sta sotto `pdfjs-*` (la versione e' nel nome della cartella)
    - e per ogni richiesta con `?v=`.
  - Il `Content-Type` lo impone il server per `.mjs`/`.js`, `.wasm`, `.css`, `.bcmap`/`.pfb`/`.icc`, `.ttf`:
    - un modulo servito come `text/plain` il browser non lo esegue.
  - Il resto lo fa `http.FileServerFS`.
- **Passano da `statico` tutti gli statici che i template caricano:**
  - `style.css` e `htmx.min.js` in `layout.html`,
  - `fascicolo.mjs` in `fascicolo.html`;
  - `motore_a.css`, `motore_a.mjs` e gli indirizzi di pdf.js in `motore_a.html`.
  - Nessun template scrive `/static/` a mano, con una sola eccezione dichiarata (P-10, RAC-18): la chiave
    dell'importmap di `motore_a.html`, `<script type="importmap">{"imports":{"/static/motore_a_gesti.mjs":
    "{{statico "motore_a_gesti.mjs"}}"}}</script>`, prima del modulo. `motore_a.mjs` importa
    `"./motore_a_gesti.mjs"`, che il browser risolve in `/static/motore_a_gesti.mjs`; la mappa lo porta
    all'indirizzo con l'impronta, cosi' anche il ponte cambia indirizzo a ogni binario nuovo
    (`motore_a_test.go:TestMotoreAIlContenutoHaLImportmap`).
  - Un binario nuovo porta impronte nuove, e il browser non tiene i file di ieri pur con la cache `immutable`.

### Il corpo di una mail

- **Dove:** `frammenti.html:corpo_messaggio`, `corpo_blocco`
- Il pannello dell'Inbox (`messaggio_pannello`) e la pagina RFQ (`thread_corpo`) disegnano il corpo con lo stesso
  template,
  - dal modello `lettura.Corpo` che Go prepara (`messaggioDati.Corpo`, `messaggioThread.Corpo`).
- La pagina non mostra mai l'HTML della mail: solo testo e tabelle estratte, tutto con l'escape di
  `html/template`.
- `corpo_messaggio` apre un `.corpo-vista`.
  - Se il corpo e' oltre il limite di `lettura` (`Ridotto`) lo mostra com'e' in un `<pre>`.
  - Altrimenti:
    - la nota «Testo ricavato dalla versione HTML…» quando il testo semplice era vuoto (`DaHTML`);
    - i `Blocchi`;
    - «Nel messaggio c'è solo testo automatico.» (`SoloRumore`);
    - «Nessun testo.» quando non c'e' niente;
    - la `Storia` chiusa in «messaggi precedenti citati» (`.corpo-storia`);
    - il «testo originale» chiuso (`.corpo-originale`, il `corpo_testo` com'e'), tranne quando il testo viene
      dall'HTML.
- `corpo_blocco` disegna un blocco per tipo:
  - `testo` (`.corpo-testo`):
    - un indirizzo e' uno `<span class="corpo-link">` in grigio con la nota nel `title`, mai un `<a>`;
    - un'immagine e' «[immagine]» in `.corpo-img`;
  - `tabella` (`.corpo-tabella`):
    - la prima riga come `<th>` se e' un'intestazione,
    - `colspan`/`rowspan`,
    - `td.num` per i numeri,
    - «…» per una cella troncata;
  - `rumore` (`.corpo-rumore`: un `<details>` chiuso con l'etichetta fuori e il testo dentro, mai tolto);
  - `separatore` (`<hr class="corpo-sep">`).

### Che cosa entra fra i pezzi della pagina (`layout.html`)

- Un ascoltatore globale `htmx:beforeSwap` guarda ogni risposta che htmx sta per innestare:
  - se e' una 2xx con un corpo e il suo `Content-Type` non comincia per `text/html`, `shouldSwap = false` e un
    avviso nella console.
  - Una risposta vuota (per esempio un 200 senza corpo) passa com'e'.
- Le rotte che non rispondono HTML non passano di qui:
  - i dati dell'editor si chiedono con `fetch`,
  - i PDF con l'iframe e con pdf.js;
  - e `GET /allegato/{id}/anteprima` risponde 400 a una richiesta htmx (lato server).

### Il Fascicolo nel browser

- **Dove:** `fascicolo.html` + `fascicolo.mjs`
- **La radice `#fascicolo`** porta `hx-target="#fasc-avviso"`, `hx-swap="innerHTML"`,
  `hx-include="#corpo-chiave"`, `hx-history="false"` e i `data-` che servono al JS (base, thread, scrive,
  bloccata, utente).
  - Ogni gesto e' un `hx-post` che eredita il bersaglio:
    - la risposta e' l'avviso,
    - il resto arriva fuori banda (`hx-swap-oob`).
- **I collegamenti** hanno l'`href` della pagina intera con lo stato (senza JavaScript funzionano) e un `hx-get` a
  `/parti`, `/anteprima` o `/vista`;
  - l'indirizzo lo rimette il server con `HX-Push-Url`.
- **`#fasc-avanzamento`** ha il poll (`every Ns`) solo finche' c'e' lavoro sui file; senza lavoro torna senza.
- **`fascicolo.mjs`**, caricato con `<script type="module">`, fa tre cose:
  1. **vista Documenti**:
     - legge `#doc-indice`, tiene lo stato di chi guarda (l'oggetto `S`),
     - disegna il PDF nello stage `#doc-stage` (con `hx-preserve`: i gesti e il poll non lo toccano).
     - pdf.js si carica quando serve (`import()` di `pdf.min.mjs`, il worker, e `wasm/`, `cmaps/`,
       `standard_fonts/`, `iccs/` dalla stessa cartella), con `isEvalSupported: false`, `enableXfa: false` e le
       annotazioni spente;
     - restano aperti gli ultimi sei documenti;
     - senza pdf.js si ripiega sull'iframe di `/allegato/{id}/anteprima`.
     - Le miniature del filmstrip usano un worker loro, due alla volta, con un `IntersectionObserver`.
     - Scegliere un altro componente non ricarica: `history.replaceState` e `GET …/fascicolo/sezione` per il
       pannello.
     - Tasti: frecce, PagSu/PagGiu', `+` `-` `0`, `N` (nota).
     - Schermo intero ricordato in localStorage (`cockpit.fascicolo.intero`);
  2. **note sul disegno**:
     - «+ Aggiungi nota» arma il cursore,
     - il clic da' il punto in coordinate 0..1 della pagina,
     - `POST …/fascicolo/nota` parte con `htmx.ajax` (funzione `gesto()`),
     - e l'esito si legge dall'avviso (`.avviso-f.no` = rifiuto: il testo resta nel riquadro);
  3. **editor della struttura** (`class Editor`):
     - `GET …/fascicolo/bom/dati` (JSON), gli archi voluti in memoria,
     - trascinare = spostare,
     - il menu ⋯ (sposta, condividi, togli, scarta, «e' il prodotto»),
     - annulla fino a 200 passi,
     - la bozza in sessionStorage (`cockpit.editor.<utente>.<thread>.<prodotto>`, con l'utente da `data-utente`:
       chi entra dopo sulla stessa scheda non trova la bozza di un altro; ripresa solo se la BOM ha la stessa
       impronta; `layout.html` cancella tutte le `cockpit.editor.*` all'invio di «esci»),
     - la conferma con `POST …/fascicolo/bom/applica` e l'esito dall'evento `bom-esito` (`HX-Trigger`).
- **Le protezioni di chi sta lavorando:**
  - `htmx:configRequest` riscrive `nodo`, `file` e `gruppo` nei GET di `/parti`, `/vista`, `/anteprima` con
    quelli dell'indirizzo di adesso;
  - `htmx:oobBeforeSwap` non rifa' `#vista` con la risposta del poll mentre si scrive o c'e' un riquadro aperto
    (compare «Ci sono novita': aggiorna»);
  - la generazione della scelta (`S.gen`) impedisce a una risposta chiesta prima di riportare indietro.

### Aggiornare pdf.js

1. Una cartella nuova `pdfjs-<versione>/` copiata dal pacchetto npm (senza `.map`),
2. `VERSIONE.txt` rifatto con gli sha256,
3. la costante `PDFJS` in `fascicolo.mjs`,
4. i percorsi in `internal/transport/web/statici_test.go`:
   - i casi di `TestIFileStaticiHannoIlTipoGiustoELaCacheGiusta`
   - e `dir` in `TestLaCopiaDiPdfjsEQuellaDichiarata`;
5. poi la cartella vecchia si toglie.

- La versione nel nome della cartella e' cio' che rende sicura la cache `immutable`.

## Invarianti

- **Niente da fuori:** nessun CDN, nessun font scaricato.
  - pdf.js e' quello di `VERSIONE.txt` byte per byte,
  - e nella cartella non c'e' niente che l'elenco non dica (`TestLaCopiaDiPdfjsEQuellaDichiarata`).
- Il tipo dei file statici lo dice il server, non il sistema operativo (`statici()`, provato).
- E' immutabile nella cache solo cio' che ha la versione nel percorso o l'impronta nell'indirizzo;
  - ogni statico caricato da un template ha l'impronta (`statici_test.go:TestIlLayoutCaricaGliStaticiConLImpronta`).
- `/static/` non elenca le cartelle (`TestLeCartelleStaticheNonSiElencano`).
- Mai HTML della mail nella pagina:
  - il corpo passa da `corpo_messaggio` come testo e tabelle,
  - l'oggetto da `oggettoVisibile`;
  - un indirizzo della mail non e' mai un collegamento (`corpo_test.go`).
- Una risposta 2xx non HTML non entra fra i pezzi della pagina (la guardia `htmx:beforeSwap` di `layout.html`).
- `[hidden]` nasconde davvero, anche sugli elementi con una classe che imposta `display` (`stile_test.go`).
- In sola lettura i gesti del Fascicolo non si mostrano (`.fasc.sola-lettura .gesto`);
  - chi li blocca pero' e' il server (`autenticato` rifiuta ogni metodo che scrive al ruolo `consultazione`), non
    il CSS.
- I dati per il JS stanno in `<script type="application/json">` scritti da `html/template` (contesto JS: i `<`
  diventano `\u003c`);
  - il JS scrive nel DOM con `textContent` e `setAttribute` (la funzione `el()`),
  - e usa `innerHTML` solo con HTML del server o con testo fisso.
- La bozza dell'editor e' di chi la scrive: chiave con l'utente, cancellata all'uscita
  (`statici_test.go:TestLaBozzaDellEditorEDiChiLaScrive`).

## Dipendenze

- Incorporati da `embed.go` (package `cockpit`);
  - letti da `internal/app/runtime` (che li passa a `web.Server`)
  - e, nei test, direttamente da `cockpit.FS`.
- I template dipendono dai tipi e dai metodi di `internal/transport/web` (`fascicoloDati`, `docVista`, `carta`,
  `rigaFile`, `statoFascicolo.Con`, …) e dalle funzioni di `server.go:funzioni`:
  - un campo rinominato in Go rompe il template solo all'esecuzione,
  - e lo scoprono i test L1 che eseguono i frammenti.
- `corpo_messaggio` e `corpo_blocco` dipendono:
  - dai tipi di `internal/core/inbox/lettura` (`Corpo`, `Blocco`, `Pezzo`, `Tabella`, `Cella`)
  - e dai valori dei loro `Tipo` (`testo`, `tabella`, `rumore`, `separatore`; `link`, `immagine`).
- `fascicolo.mjs` dipende:
  - dalle rotte `…/fascicolo/{sezione,nota,bom/dati,bom/applica,parti,vista,anteprima}` e
    `/allegato/{id}/anteprima`,
  - dagli id, dalle classi e dai `data-` di `fascicolo.html` (`#fascicolo` con `data-utente`, `#doc-vista`,
    `#doc-indice`, `#doc-stage`, `#doc-sezione`, `#fasc-avviso`, `#fasc-editor`, `.docv-*`)
  - e da htmx (`window.htmx`).
- La pulizia delle bozze in `layout.html` dipende dall'id `#esci` del form di uscita e dal prefisso
  `cockpit.editor.` della chiave.

## Test

- **`internal/transport/web/web_test.go`**
  - Livello: L1
  - Che cosa prova: `Init` compila tutti i template
- **`internal/transport/web/templates_test.go`**
  - Livello: L1
  - Che cosa prova: i frammenti principali si eseguono con dati sintetici
- **`internal/transport/web/corpo_test.go`**
  - Livello: L1
  - Che cosa prova: `corpo_messaggio` nel pannello e nella pagina RFQ:
    - tabella da Excel,
    - testo automatico chiuso,
    - storia citata,
    - «testo originale»,
    - uno script scritto nella mail resta testo,
    - nessun collegamento;
    - l'oggetto senza `[EXT]`;
    - «Nessun testo.»
- **`internal/transport/web/statici_test.go`**
  - Livello: L1
  - Che cosa prova:
    - tipo e cache di ogni statico, l'impronta di `statico`, niente uscita da `static/`;
    - le cartelle non si elencano;
    - il layout carica `style.css` e `htmx.min.js` con l'impronta e ha la guardia `htmx:beforeSwap`;
    - la chiave della bozza dell'editor con l'utente e la pulizia all'uscita;
    - la copia di pdf.js uguale a `VERSIONE.txt`
- **`internal/transport/web/ruoli_test.go`**
  - Livello: L1
  - Che cosa prova: `vietato.html` dice il ruolo che serve davvero, o nessuno
- **`internal/transport/web/triage_test.go`**
  - Livello: L1
  - Che cosa prova:
    - `candidati_codice` non spunta un codice della storia citata;
    - `chip_controparte` con `altro`
- **`internal/transport/web/stile_test.go`**
  - Livello: L1
  - Che cosa prova: la regola `[hidden]`
- **`internal/transport/web/fascicolo_test.go`, `fascicolo_b87b_test.go`, `documenti_v3_test.go`**
  - Livello: L1
  - Che cosa prova: i template del Fascicolo con dati sintetici (BOM, piano, cassetti, poll, vista Documenti)
- **`internal/transport/web/e2e/*.py` via `*_browser_test.go`** (tag `integrazione browser`, Playwright su Edge)
  - Livello: L7
  - Che cosa prova:
    - Inbox (`inbox_quadranti.py`),
    - Richieste (`richieste.py`),
    - anteprima PDF (`anteprima_pdf.py`),
    - Fascicolo (`fascicolo.py`),
    - Fascicolo v3 con pdf.js, note ed editor (`fascicolo_v3.py`)
- **`internal/transport/web/motore_a_render_test.go`, `motore_a_statici_test.go`, `motore_a_casi_test.go`**
  - Livello: L1
  - Che cosa prova: la Distinta nuova resa dal template vero per ogni caso ACME di `testdata/motore_a/`, vista,
    prodotto e `Scrive` (pagina e frammento), i testi ostili che restano testo, i controlli statici dei tre file
    (nessun HTML inserito dal JS, una sola `fetch`, nessun `htmx.ajax`, `sendBeacon` o XHR, il CSS solo dentro la
    pagina, nessun indirizzo esterno); con `MA_HTML=<cartella>` scrive le pagine per
    il kit jsdom (`testdata/motore_a/kit/prove/prove.cjs`, Node e jsdom fuori dal repository)
- **`internal/transport/web/motore_a_browser_test.go` + `e2e/motore_a.py`** (tag `integrazione browser`)
  - Livello: L4 (testata), Playwright su Edge
  - Che cosa prova: la Distinta nuova in un browser vero (apertura, schede, miniature, anteprima persistente, un
    gesto rifiutato e uno riuscito, il flag spento)
- `fascicolo.mjs` non ha test unitari JavaScript: lo prova solo L7.

## Stato dell'implementazione

- **Completo** per il Fascicolo v3 (vista Documenti con pdf.js, note, editor della struttura), B8.7b e la pagina
  Richieste.
- **Nessun visore 3D:** di uno STEP la schermata mostra quello che l'analisi ha letto.
- **Limiti noti:**
  - `style.css` nomina i font «IBM Plex Mono» e «IBM Plex Sans» che non sono inclusi (valgono i ripieghi di
    sistema).
  - `fascicolo.html` (circa 1170 righe) e `fascicolo.mjs` (circa 1600) sono un file ciascuno.
  - La guardia `htmx:beforeSwap` non ha una prova nel browser: `statici_test.go` guarda solo che ci sia.

## Dove intervenire

- **Voglio una pagina nuova** — apri:
  - il file in `web/templates/` con il suo `contenuto`,
  - l'elenco di `server.go:Init`,
  - il gestore che chiama `rendi`
- **Voglio una funzione nuova per i template** — apri: `server.go:funzioni`
- **Voglio cambiare la cornice, la rail o la testata** — apri: `layout.html`, `navigazione.go`,
  `frammenti.html:stato_worker`
- **Voglio cambiare un pannello del Fascicolo** — apri:
  - `fascicolo.html` (tabella sopra)
  - e i dati in `internal/transport/web/fascicolo_*.go`
- **Voglio cambiare il visore, le note o l'editor** — apri: `fascicolo.mjs`
- **Voglio cambiare come si vede il corpo di una mail** — apri:
  - `frammenti.html:corpo_messaggio`, `corpo_blocco`, i `.corpo-*` di `style.css`;
  - le regole in `internal/core/inbox/lettura`
- **Voglio cambiare che cosa entra fra i pezzi della pagina** — apri: `layout.html` (l'ascoltatore
  `htmx:beforeSwap`)
- **Voglio un file statico nuovo con cache lunga** — apri: il file in `web/static/`, `{{statico "nome"}}` nel
  template
- **Voglio aggiornare pdf.js** — apri: «Aggiornare pdf.js» qui sopra
- **Voglio capire perche' un modulo non si carica** — apri: `server.go:statici` (il `Content-Type`)

## Leggi anche

- `internal/transport/README.md`
- `internal/transport/web/README.md` (le rotte e i gestori)
- `internal/core/inbox/lettura/README.md` (come si legge il corpo di una mail)
- `internal/README.md`
- `README.md` (le sezioni «Il Fascicolo v3» e «Le prove nel browser (L7)»)
