// Package web serve HTML (mai JSON) al browser. Ogni rotta esiste in due forme decise dall'header
// HX-Request: pagina intera (layout + contenuto) o solo il frammento.
//
// I file sono per area. server.go e' l'infrastruttura condivisa: il tipo Server, i template, il
// montaggio delle rotte, la sessione con il suo cookie, il rendering e i due avvisi.
// routes_inbox.go, routes_rfq.go e routes_admin.go tengono le tre aree, e ognuna monta le sue rotte
// nella propria `registra*`. Gli altri file erano gia' per tema e restano dove sono.
package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"promatec/cockpit/internal/ai/agente"
	"promatec/cockpit/internal/core/inbox/ingest"
	"promatec/cockpit/internal/core/inbox/lettura"
	"promatec/cockpit/internal/core/rfq/documenti"
	"promatec/cockpit/internal/core/rfq/fascicolo"
	"promatec/cockpit/internal/platform/coda"
	"promatec/cockpit/internal/platform/contratti/worker"
	"promatec/cockpit/internal/platform/db"
	"promatec/cockpit/internal/platform/rete"
	"promatec/cockpit/internal/platform/storage/nas"
)

type Server struct {
	Pool   *pgxpool.Pool
	Log    *slog.Logger
	NAS    *nas.Scrittore
	Ingest *ingest.Servizio // riprova degli scarti dell'ingest
	Templ  fs.FS            // web/templates
	Static fs.FS            // web/static
	pagine map[string]*template.Template
	// versioni: per i file statici che una pagina carica con «statico», l'impronta del contenuto, che va
	// nell'indirizzo (?v=): un cockpit.exe nuovo porta file nuovi, e il browser non tiene i vecchi.
	versioni map[string]string
	// IntervalloSync: ogni quanto lo scheduler accoda il sync (0 = mai). Serve solo a dirlo
	// all'operatore nella schermata, con parole che corrispondono alla configurazione.
	IntervalloSync time.Duration
	// Sync sono cartelle, data minima e lotto del sync ordinario: le stesse dello scheduler, perché
	// «Aggiorna ora» accoda esattamente il job che accoderebbe lui (voce 2.16).
	Sync coda.SyncOpzioni
	// SyncAperturaInbox: alla prima apertura dell'Inbox di una sessione si accoda un aggiornamento
	// ([outlook].sync_apertura_inbox, predefinito true). È indipendente da IntervalloSync: quello
	// governa il sync periodico, questo è una richiesta implicita di chi sta aprendo la schermata.
	SyncAperturaInbox bool
	// PollRichieste: ogni quanto la pagina Richieste chiede se l'elenco e' cambiato (0 = 60 s). Le
	// prove nel browser lo accorciano per vedere un giro di poll senza aspettare un minuto.
	PollRichieste time.Duration
	// Modalita è "shadow" o "produzione" (§2.7): la testata lo dice sempre, perché in shadow metà
	// dei pulsanti non fa quello che c'è scritto sopra, e va saputo prima di premerli.
	Modalita string
	// IndirizzoClient: da quale IP arriva il browser (voce 2.7, abbinamento sessione → postazione).
	// Nil = RemoteAddr. X-Forwarded-For non si legge in produzione: lo scriverebbe chiunque.
	IndirizzoClient func(*http.Request) string
	// TLS è il certificato del listener, o nil se il server è in chiaro (voce 2.4). Serve a due cose:
	// mettere `Secure` sul cookie di sessione, e scrivere l'impronta nel worker.toml che la pagina
	// *Postazioni* genera — così l'impronta non va copiata a mano da nessuna parte.
	TLS *rete.Materiale
	// Indirizzo è [server].indirizzo: da qui si ricava l'URL da scrivere nel worker.toml del pacchetto.
	Indirizzo string
	// URLPubblico e' [server].url_pubblico (7C.1, P1): l'indirizzo che finisce nel pacchetto della
	// postazione. Vuoto = si deriva dal bind (URLServer).
	URLPubblico string
	// Agente è l'analisi semantica (checkpoint 3R §9). Nil o spenta: la schermata non offre il
	// pulsante, e la rotta risponde che l'analisi non è attiva. Nascondere non è autorizzare, quindi
	// il controllo sta in tutti e due i posti.
	Agente *agente.Servizio
	// Workers è il filesystem che contiene `workers/` (il pacchetto del worker, D22). Nil = la pagina
	// *Postazioni* genera solo il worker.toml, senza i file del worker.
	Workers fs.FS
	// Ricognitore confronta i documenti del database con i file veri sul NAS (blocco 5B). È lo stesso
	// oggetto che gira a tempo: «Controlla ora» in Admin chiama la sua stessa funzione, perché un
	// controllo che in produzione e a richiesta passa da due strade diverse è un controllo che in una
	// delle due prima o poi si comporta in un altro modo. Nil = la schermata lo dice.
	Ricognitore *documenti.Ricognitore
	// Staging è la radice dello staging locale ([nas].staging, assoluta). Serve a due cose. La prima non
	// si vede: `allegato.path_staging` è un percorso assoluto letto dal database, e l'anteprima lo apre —
	// quindi prima verifica che stia qui sotto, come già fa per il NAS. La seconda è il caricamento interno
	// del Fascicolo (B8.7): il file caricato a mano va fra i contenuti, con il suo sha256 per nome, come
	// quelli che portano i worker. Vuota = l'anteprima non serve dallo staging e il caricamento si
	// rifiuta, perché ciò che non si può verificare non si serve e non si scrive.
	Staging string
	// Pipeline è la strada di un file appena arrivato in staging (proposta dal nome, analisi), quella del
	// workerapi: il caricamento interno del Fascicolo la percorre tale e quale (B8.7). Nil = il
	// caricamento non è disponibile, e la schermata lo dice.
	Pipeline interface {
		DopoCaricamento(ctx context.Context, q *db.Queries, allegato uuid.UUID) error
	}
	// MaxCaricamento: la dimensione massima di un file caricato a mano, la stessa degli upload dei worker
	// ([server].max_upload_mb). 0 = 64 MB.
	MaxCaricamento int64
	// Analizzatore: la versione e la configurazione correnti dell'analisi ([analisi], voce 1.12), le
	// stesse del workerapi. Servono alla rianalisi degli STEP di una RFQ (B8.5): si rileggono quelli
	// che hanno i fatti con questa chiave, si accodano gli altri. Versione 0 = nessuna rianalisi.
	Analizzatore coda.Analizzatore
	// MotoreA è la Distinta nuova del motore A (giro 5, A1d; contratto A1d §2.1; piano 7.4.2). Spenta (il valore zero):
	// le sue rotte rispondono 404 a tutti prima della sessione e il ramo di threadFrammento non scatta. Accesa: la vede
	// solo l'admin. Si legge a ogni richiesta e mai al montaggio delle rotte, perché le prove la cambiano a server
	// acceso, come Analizzatore.
	MotoreA AnteprimaMotoreA
}

type chiaveCtx int

const ctxUtente chiaveCtx = 1

const cookieSessione = "cockpit_sess"

var funzioni = template.FuncMap{
	"list": func(a ...string) []string { return a },
	// oggettoVisibile è l'oggetto senza le etichette «[EXTERNAL]», «[EXT]» che il server di posta
	// aggiunge davanti: nella lista e nel pannello si legge l'oggetto vero. Quello memorizzato resta
	// com'è (lettura.OggettoVisibile).
	"oggettoVisibile": func(t pgtype.Text) string { v, _ := lettura.OggettoVisibile(t.String); return v },
	"data":            func(t time.Time) string { return t.Local().Format("02/01 15:04") },
	"dataLunga":       func(t time.Time) string { return t.Local().Format("02/01/2006 15:04") },
	// oraBreve/oraLunga accettano un istante che può non esserci (l'ultimo sync di una casella che
	// non ha mai sincronizzato): il template deve poterle chiamare senza sapere se il valore c'è.
	"oraBreve": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Local().Format("15:04")
	},
	"oraLunga": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Local().Format("02/01/2006 15:04")
	},
	"txt": func(t pgtype.Text) string { return t.String },
	"kb": func(b pgtype.Int8) string {
		if !b.Valid {
			return ""
		}
		if b.Int64 < 1024 {
			return fmt.Sprintf("%d B", b.Int64)
		}
		return fmt.Sprintf("%.0f KB", float64(b.Int64)/1024)
	},
	// Smistamento F3 (A5.14.5): la lettura di una proposta per dimensione, una dimensione col suo nome per il
	// frammento «dimensione», uno score di regola («score N», mai una percentuale), una evidenza in parole.
	"valutazione": valutazioneDi,
	"dim":         dimVista,
	"score":       scoreHTML,
	"parole":      paroleEvidenza,
	"dipendenza":  dipendenzaEvidenza,
	"motivi": func(m *json.RawMessage) []string {
		if m == nil {
			return nil
		}
		var out []string
		_ = json.Unmarshal(*m, &out)
		return out
	},
	"uuidBreve": func(u uuid.UUID) string { return u.String()[:8] },
	// cache C1: l'indirizzo dell'anteprima con l'impronta del contenuto, l'unico che il browser puo' tenere
	// senza richiederlo (anteprima.go)
	"indirizzoAnteprima": func(id uuid.UUID, sha pgtype.Text) string { return indirizzoAnteprima(id, sha.String) },
	"tipiDocumento": func() []string {
		out := make([]string, 0, len(db.AllTipoDocumentoValues()))
		for _, t := range db.AllTipoDocumentoValues() {
			out = append(out, string(t))
		}
		return out
	},
	"vistaAllegati": func(a []AllegatoUI, messaggioID uuid.UUID, agganciato bool, ritornaThread string, gesti bool) allegatiVista {
		v := allegatiVista{Allegati: a, MessaggioID: messaggioID, Agganciato: agganciato, RitornaThread: ritornaThread, Gesti: gesti}
		for _, x := range a {
			if x.Scaricabile() {
				v.NScaricabili++
			}
		}
		return v
	},
	"rigaAllegato": func(a AllegatoUI, agganciato bool, ritornaThread string, figlio, gesti bool) rigaAllegato {
		return rigaAllegato{A: a, Agganciato: agganciato, RitornaThread: ritornaThread, Figlio: figlio, Gesti: gesti}
	},
	// B8.6, il pannello dei codici: il tipo di componente con le parole della schermata (prodotto,
	// assieme, particolare), i tipi fra cui si sceglie per un nodo proposto, una riga del pannello.
	"nomeTipo":       func(t db.TipoComponente) string { return fascicolo.NomeTipo(t) },
	"tipiComponente": func() []db.TipoComponente { return fascicolo.TipiDaCodice },
	"rigaCodice":     nuovaRigaCodice,
	// Smistamento P17: il codice di un candidato visto solo nel nome di un allegato.
	"soloNelNome": SoloNelNome,
	// B8.7, la schermata del Fascicolo: un nodo dell'albero e una riga tratteggiata con la schermata (il
	// template e' ricorsivo), i nomi brevi dei tipi, gli esiti dello STEP del prodotto finito.
	"nodoVista": func(d *fascicoloDati, n *fascicolo.Nodo) nodoVista { return nodoVista{D: d, N: n} },
	// B8.7b: una card della BOM visuale e una voce del NAS, con la schermata; una dimensione in byte.
	"cartaVista": func(d *fascicoloDati, c *carta) cartaVista { return cartaVista{D: d, C: c} },
	"voceVista":  voceVistaDi,
	"fileVista":  func(d *fascicoloDati, s *sezioneDoc, f fileDoc) fileVista { return fileVista{D: d, S: s, F: f} },
	"nasRiga":    func(d *fascicoloDati, v nasVoce) nasRigaVista { return nasRigaVista{D: d, V: v} },
	"kbInt": func(b int64) string {
		switch {
		case b < 1024:
			return fmt.Sprintf("%d B", b)
		case b < 1024*1024:
			return fmt.Sprintf("%.0f KB", float64(b)/1024)
		}
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	},
	"propostaVista":    func(d *fascicoloDati, p figlioProposto) propostaVista { return propostaVista{D: d, P: p} },
	"etichettaTipoDoc": etichettaTipoDoc,
	"etichettaStep":    fascicolo.EtichettaStep,
	"etichettaNodo":    etichettaNodo,
	"classeStep":       classeStep,
	"simboloStep":      simboloStep,
	"mittente":         mittente,
	"mul":              func(a, b any) int { return intero(a) * intero(b) },
	"add":              func(a, b any) int { return intero(a) + intero(b) },
	"sub":              func(a, b any) int { return intero(a) - intero(b) },
	"sel": func(si bool, a, b string) string {
		if si {
			return a
		}
		return b
	},
	// Smistamento M1: la proposta del triage senza percentuali («RFQ: forte», «nuova RFQ? score 60»)
	"chipTriage": chipTriage,
	"join":       strings.Join,
	// arr mette piu' valori in un elenco, per passarli insieme a un template: {{template "x" (arr $v .)}}
	"arr":       func(x ...any) []any { return x },
	"hasPrefix": strings.HasPrefix,
	// il colore della riga dell'Inbox, coerente col chip del triage (Smistamento M1)
	"colore": coloreTriage,
	// Smistamento M2: il chip dell'evento di una riga (nil se il triage non ha scritto un atto) e i
	// motivi della proposta senza le righe dell'evento, che si mostrano a parte
	"eventoRiga":     eventoDellaRiga,
	"motiviProposta": motiviProposta,
	// giro 5, A1d (contratto §2.3): il testo di un valore di enum, per tutte le pagine
	"etichetta": etichetta,
}

// intero legge un numero intero di qualunque larghezza, per le poche somme dei template.
func intero(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int16:
		return int(x)
	case int32:
		return int(x)
	case int64:
		return int(x)
	}
	return 0
}

func (s *Server) Init() error {
	s.versioni = map[string]string{}
	if s.Static != nil {
		// le impronte si calcolano qui, una volta: le pagine le leggono in parallelo
		voci, _ := fs.ReadDir(s.Static, ".")
		for _, v := range voci {
			if v.IsDir() {
				continue
			}
			if b, err := fs.ReadFile(s.Static, v.Name()); err == nil {
				h := sha256.Sum256(b)
				s.versioni[v.Name()] = hex.EncodeToString(h[:6])
			}
		}
	}
	base, err := template.New("layout").Funcs(funzioni).Funcs(template.FuncMap{"statico": s.statico}).
		ParseFS(s.Templ, "layout.html", "frammenti.html")
	if err != nil {
		return err
	}
	s.pagine = map[string]*template.Template{}
	for _, p := range []string{"inbox.html", "login.html", "job.html", "scarti.html", "thread.html", "fascicolo.html", "postazioni.html", "vietato.html", "anagrafica.html", "richieste.html", "integrita.html", "fornitori.html", "importa.html", "distinta.html", "forme.html", "motore_a.html"} {
		t, err := template.Must(base.Clone()).ParseFS(s.Templ, p)
		if err != nil {
			return fmt.Errorf("template %s: %w", p, err)
		}
		s.pagine[p] = t
	}
	return nil
}

// ProtezioneCSRF avvolge il server con `http.CrossOriginProtection` (voce 2.5).
//
// Il cookie di sessione viaggia da solo: se una pagina qualsiasi, aperta dall'operatore mentre è
// collegato, manda un POST al Cockpit, il browser ci attacca il cookie e il server non ha modo di
// distinguerlo da un clic sulla schermata vera. Con «Conferma», «Apri in Outlook» e «Bozza» dietro a
// dei POST, questo basta a far succedere cose a nome suo.
//
// `CrossOriginProtection` guarda `Sec-Fetch-Site` (e, per i browser che non lo mandano, `Origin`
// contro `Host`) e blocca i metodi non sicuri dichiarati cross-site. Non è un token da mettere in
// ogni form: non c'è niente da ricordarsi di aggiungere, ed è il motivo per cui regge anche sulle
// pagine che verranno.
//
// I worker non sono browser: non mandano né `Sec-Fetch-Site` né `Origin`, e passano. La loro
// autenticazione è il token individuale, che una pagina esterna non ha (voce 2.4).
//
// Sta qui, e non in main, perché il test deve poter provare ESATTAMENTE ciò che gira in produzione:
// una protezione montata in due punti diversi è una protezione che in un punto prima o poi manca.
func ProtezioneCSRF(h http.Handler) http.Handler {
	return http.NewCrossOriginProtection().Handler(h)
}

// Registra monta tutte le rotte. Le quattro `registra*` stanno nel file della loro area: chi
// aggiunge una schermata all'Inbox non apre questo file, e chi apre questo file vede quante aree
// ci sono senza doverle contare fra novanta righe.
//
// Qui restano le rotte che non sono di nessuna area: i file statici, la salute del server e la
// porta d'ingresso.
func (s *Server) Registra(mux *http.ServeMux) {
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.statici()))
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /{$}", s.autenticato(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/inbox", http.StatusFound) }))
	s.registraInbox(mux)
	s.registraRFQ(mux)
	s.registraDistinta(mux)
	// giro 5, A1d: la Distinta nuova, dietro [motore_a] (motore_a.go)
	s.registraMotoreA(mux)
	s.registraPostazioni(mux)
	s.registraAdmin(mux)
}

// statici serve i file statici. Quelli che non cambiano mai sotto lo stesso indirizzo (pdf.js, che sta in
// una cartella con la sua versione, e i file chiesti con l'impronta ?v=) il browser li tiene: il modulo del
// lavoratore di pdf.js pesa piu' di un megabyte, e il Fascicolo si apre molte volte al giorno.
//
// Si servono file, non cartelle: una cartella risponde 404 invece dell'elenco del suo contenuto, che
// non serve a nessuna pagina e dice a chiunque che cosa c'e' sul server.
func (s *Server) statici() http.Handler {
	h := http.FileServerFS(s.Static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nome := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if nome == "" {
			nome = "."
		}
		if st, err := fs.Stat(s.Static, nome); err == nil && st.IsDir() {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "pdfjs-") || r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		// Il tipo lo dice il server, non il registro di Windows: un .mjs servito come text/plain e il
		// browser non carica il modulo (e la vista Documenti resta vuota).
		switch strings.ToLower(filepath.Ext(r.URL.Path)) {
		case ".mjs", ".js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".wasm":
			w.Header().Set("Content-Type", "application/wasm")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".bcmap", ".pfb", ".icc":
			w.Header().Set("Content-Type", "application/octet-stream")
		case ".ttf":
			w.Header().Set("Content-Type", "font/ttf")
		}
		h.ServeHTTP(w, r)
	})
}

// statico e' l'indirizzo di un file statico con l'impronta del suo contenuto: {{statico "fascicolo.mjs"}}.
// Le impronte le calcola Init; un file che non c'e' torna senza impronta.
func (s *Server) statico(nome string) string {
	if v, ok := s.versioni[nome]; ok {
		return "/static/" + nome + "?v=" + v
	}
	return "/static/" + nome
}

// ---------------------------------------------------------------- rendering

type vista struct {
	Utente    *db.Utente
	Titolo    string
	Dati      any
	Frammento bool
	// Admin costruisce la barra di navigazione: le voci tecniche esistono solo per chi può aprirle.
	// Lo decide `almeno`, la stessa funzione del wrapper delle rotte, perché una barra che si
	// calcola da sola prima o poi mostra una voce che porta a un 403 (o peggio, la nasconde a chi
	// invece potrebbe).
	Admin bool
	// Stato è la testata: postazione della sessione, stato per casella, worker di analisi (M2).
	Stato *statoUI
	// Serve è il ruolo che la pagina del divieto (nega) dice che basterebbe; vuoto = il divieto non
	// dipende dal ruolo.
	Serve db.RuoloUtente
}

// frammentoRichiesto dice se questa richiesta vuole un pezzo di pagina o la pagina intera.
//
// HTMX chiede un pezzo. Tranne quando l'operatore preme «indietro» e la copia in cache non c'e'
// piu': allora rifa' la richiesta con HX-Request: true PIU' HX-History-Restore-Request: true, e si
// aspetta la PAGINA, perche' deve rimetterla al posto di tutto il corpo. Rispondergli con un
// frammento significa lasciargli in mano una colonna sola, senza testata e senza navigazione.
func frammentoRichiesto(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true"
}

func (s *Server) rendi(w http.ResponseWriter, r *http.Request, pagina, frammento string, titolo string, dati any) {
	t := s.pagine[pagina]
	v := vista{Utente: utenteDa(r.Context()), Titolo: titolo, Dati: dati, Frammento: frammentoRichiesto(r)}
	v.Admin = almeno(v.Utente, db.RuoloUtenteAdmin)
	if !v.Frammento && v.Utente != nil {
		v.Stato = s.stato(r.Context(), sessioneDa(r.Context()))
	}
	nome := "layout"
	if v.Frammento && frammento != "" {
		nome = frammento
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, nome, v); err != nil {
		s.Log.Error("template", "pagina", pagina, "err", err)
	}
}

func (s *Server) frammento(w http.ResponseWriter, nome string, dati any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pagine["inbox.html"].ExecuteTemplate(w, nome, vista{Dati: dati, Frammento: true}); err != nil {
		s.Log.Error("template", "frammento", nome, "err", err)
	}
}

func utenteDa(ctx context.Context) *db.Utente {
	u, _ := ctx.Value(ctxUtente).(*db.Utente)
	return u
}

// ---------------------------------------------------------------- healthz

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	out := worker.Salute{DB: "ok", NAS: "non_raggiungibile"}
	var v int
	if err := s.Pool.QueryRow(r.Context(), "SELECT max(versione) FROM schema_versione").Scan(&v); err != nil {
		out.DB = "errore: " + err.Error()
	}
	out.Versione = v
	if s.NAS.Raggiungibile() {
		out.NAS = "ok"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// ---------------------------------------------------------------- sessioni

func (s *Server) autenticato(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieSessione)
		if err != nil {
			s.aLogin(w, r)
			return
		}
		q := db.New(s.Pool)
		riga, err := q.GetSessione(r.Context(), c.Value)
		if err != nil {
			s.aLogin(w, r)
			return
		}
		_ = q.ToccaSessione(r.Context(), c.Value)
		u := riga.Utente
		sess := sessioneUI{Token: c.Value, Utente: &u, Postazione: riga.PostazioneID, NomeHost: riga.NomeHost.String, Origine: riga.PostazioneOrigine.String}
		s.riabbinaPostazione(r, q, &sess)
		ctx := context.WithValue(r.Context(), ctxUtente, &u)
		ctx = context.WithValue(ctx, ctxSessione, sess)
		// `consultazione` e' sola lettura, e lo e' QUI: una regola per metodo, in un punto solo,
		// invece di un permesso da ricordarsi su ogni rotta che scrive — e le rotte che scrivono
		// sono destinate a moltiplicarsi. Vale su tutto cio' che sta dietro all'autenticazione;
		// `/logout` non ci sta, e chi consulta deve comunque poter uscire.
		if metodoCheScrive(r.Method) && !almeno(&u, db.RuoloUtenteOperatore) {
			s.nega(w, r.WithContext(ctx), "Il ruolo «consultazione» vede il Cockpit e non lo cambia.", db.RuoloUtenteOperatore)
			return
		}
		h(w, r.WithContext(ctx))
	}
}

// riabbinaPostazione ripara il caso piu' normale che ci sia: il browser aperto PRIMA che il worker
// di quel PC facesse il suo primo claim. Al login l'IP non corrispondeva a niente, la sessione e'
// nata senza postazione, e senza questo l'operatore resterebbe «Sei su: —» per dodici ore — con le
// azioni su Outlook spente — anche molto dopo che il worker si e' acceso. Uscire e rientrare
// funzionerebbe, ma nessuno sa che e' quello il rimedio.
//
// Ripara SOLO una sessione senza origine, cioè una che non ha mai saputo dove si trova (compresa
// quella a cui l'operatore ha rimesso il «—», che vuol dire «non lo so»). Una postazione SCELTA in
// testata — origine «scelta» — non si tocca mai: quella è una decisione, e una schermata che disfa
// al poll successivo la decisione appena presa è peggio di una che non aiuta.
//
// L'autorizzazione resta quella di P1: passa da `abbinaPostazione`, cioe' da `puoUsarePostazione`.
// L'IP e' un indizio su QUALE postazione, mai un permesso ad usarla.
func (s *Server) riabbinaPostazione(r *http.Request, q *db.Queries, sess *sessioneUI) {
	if sess.Postazione.Valid || sess.Origine != "" {
		return
	}
	ip, ok := s.indirizzoDi(r)
	if !ok {
		return
	}
	id, nome := s.abbinaPostazione(r.Context(), q, sess.Utente, ip)
	if !id.Valid {
		return
	}
	if err := q.SetSessionePostazione(r.Context(), db.SetSessionePostazioneParams{
		Token: sess.Token, PostazioneID: id, PostazioneOrigine: pgtype.Text{String: "ip", Valid: true},
	}); err != nil {
		s.Log.Error("riabbinamento della sessione", "utente", sess.Utente.Sigla, "err", err)
		return
	}
	sess.Postazione, sess.NomeHost, sess.Origine = id, nome, "ip"
	s.Log.Info("sessione riabbinata alla postazione per IP", "utente", sess.Utente.Sigla, "postazione", nome, "ip", ip.String())
}

func (s *Server) aLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	s.rendi(w, r, "login.html", "", "Accesso", map[string]any{"Errore": r.URL.Query().Get("errore")})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	q := db.New(s.Pool)
	u, err := q.GetUtentePerSigla(r.Context(), strings.ToUpper(strings.TrimSpace(r.FormValue("sigla"))))
	if err != nil || !u.PasswordHash.Valid || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash.String), []byte(r.FormValue("password"))) != nil {
		http.Redirect(w, r, "/login?errore=credenziali", http.StatusFound)
		return
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	// Abbinamento per IP (voce 2.7): se un worker attivo di una postazione autorizzata ha fatto
	// claim da questo stesso indirizzo, la sessione nasce su quella postazione ('ip'). Altrimenti
	// nasce senza, e l'operatore sceglie dalla testata.
	var post uuid.NullUUID
	var origine pgtype.Text
	if ip, ok := s.indirizzoDi(r); ok {
		id, motivo := s.abbinaPostazione(r.Context(), q, &u, ip)
		if id.Valid {
			post, origine = id, pgtype.Text{String: "ip", Valid: true}
			s.Log.Info("sessione abbinata alla postazione per IP", "utente", u.Sigla, "postazione", motivo, "ip", ip.String())
		} else {
			s.Log.Info("sessione senza postazione", "utente", u.Sigla, "ip", ip.String(), "motivo", motivo)
		}
	}
	if _, err := q.CreaSessioneConPostazione(r.Context(), db.CreaSessioneConPostazioneParams{
		Token: tok, UtenteID: u.UtenteID, ScadeIl: time.Now().Add(12 * time.Hour), PostazioneID: post, PostazioneOrigine: origine,
	}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// `Secure` solo quando il server parla davvero TLS (voce 2.5): metterlo sempre farebbe sparire il
	// cookie su http, cioè renderebbe impossibile il login in sviluppo, e la cura sarebbe togliere
	// l'attributo — che è peggio di non averlo mai messo. `SameSite=Lax` vale in tutti e due i casi.
	http.SetCookie(w, &http.Cookie{Name: cookieSessione, Value: tok, Path: "/", HttpOnly: true,
		Secure: s.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 12 * 3600})
	http.Redirect(w, r, "/inbox", http.StatusFound)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieSessione); err == nil {
		_ = db.New(s.Pool).EliminaSessione(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieSessione, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// avviso risponde con un frammento al posto del pulsante che è stato premuto: chi ha premuto legge
// che cosa è successo, invece di vedere una pagina che non cambia.
func (s *Server) avviso(w http.ResponseWriter, testo string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="avviso">%s</div>`, template.HTMLEscapeString(testo))
}

// avvisoErrore è avviso per un'azione che NON è partita: stesso posto, colore diverso. Risponde 200
// perché è un frammento HTMX da mostrare, non un errore del server.
func (s *Server) avvisoErrore(w http.ResponseWriter, testo string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div class="avviso errore-box">%s</div>`, template.HTMLEscapeString(testo))
}
