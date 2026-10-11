package web

// motore_a.go: la Distinta nuova del motore A (giro 5, A1d; contratto A1d §2, §3.1; piano 7.4.2-7.4.3; piano A1d
// backend §5.1).
//
// A1d-P2: i dati della pagina e del JSON di /motore-a/dati (contratto §2.2, §2.4), la porta (flag spento = 404 a tutti
// prima della sessione; acceso = solo l'admin), il calcolo in sola lettura con il limite dell'indice e un recover
// (caricatore.Carica → valutazione.Calcola → la copia di passaggio → confronto.Confronta), la fase registrata e
// l'esistenza della RFQ in una transazione READ ONLY sua, e il ramo dell'esito dei gesti che threadFrammento richiama
// (contratto §3.1; v1.1 §4.10). A1d-P3: la pagina HTML (motoreAPagina, rendiMotoreA; il template motore_a.html, che
// Init carica con le altre pagine, T-A1d-06). Le ricerche per il template stanno in motore_a_indice.go, le etichette in
// motore_a_etichette.go, la copia di passaggio in motore_a_passaggio.go, l'anteprima JSON dello STEP in
// motore_a_anteprima.go.
//
// Una GET di motore-a non scrive niente: nessun job, nessun prepara, nessuna sincronizzazione. Le sole letture del DB
// sono quelle del caricatore (REPEATABLE READ READ ONLY, caricatore.go:109), GetThread, GetFaseCorrente e
// ListFaseCatalogo (READ ONLY, T-A1d-02) e quelle di EffettoAutorizzazione (motore_a_anteprima.go, T-A1d-03). Vale
// per il dominio: la riga della sessione la tocca autenticato (server.go), come su ogni rotta (I-04 della revisione
// di A1d-P2).

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/fotorfq"
	"promatec/cockpit/internal/core/fotorfq/caricatore"
	"promatec/cockpit/internal/core/inbox/classificazione/motorea"
	"promatec/cockpit/internal/core/valutazione"
	"promatec/cockpit/internal/platform/db"
)

// ErroreRegoleMotoreA: il testo della pagina quando le regole non si sono caricate (piano 7.6.2). Generico: il dettaglio
// è nel log dell'avvio (app/runtime/motore_a.go).
const ErroreRegoleMotoreA = "Regole del motore A non caricate: il dettaglio è nel log del server."

// AnteprimaMotoreA: che cosa serve alla pagina, preparato una volta all'avvio da app/runtime (piano 7.4.2).
//   - Accesa: [motore_a].anteprima.
//   - Regole: le grammatiche compilate dall'indice, con i limiti dell'indice; nil = non caricate, e Errore dice perché.
//   - Ingressi: gli ingressi dei casi, dal file che l'indice dichiara (R29 b = C); vuoti senza il file.
//   - Limite: il tempo massimo di un calcolo della pagina (limiti.anteprima.tempo_massimo_ms dell'indice, R43 B).
//   - Calcola: nil = il percorso di sempre (calcolaMotoreA); le prove lo sostituiscono con uno che sbaglia, va in panic
//     o non finisce in tempo.
//   - leggiFase: nil = faseMotoreA (il pool); solo le prove del pacchetto la sostituiscono, così i rami di
//     paginaMotoreADa (la RFQ che non c'è = 404, le regole non caricate, il tempo, l'errore) si provano senza
//     database (R-1 della revisione di A1d-P2; da ratificare con T-A1d-02, come faseMotoreADa). app/runtime non la
//     vede e non la imposta.
type AnteprimaMotoreA struct {
	Accesa    bool
	Regole    *motorea.InsiemeRegole
	Ingressi  valutazione.Ingressi
	Limite    time.Duration
	Errore    string
	Calcola   func(ctx context.Context, thread uuid.UUID) (fotorfq.Fotografia, valutazione.Esito, confronto.Esito, error)
	leggiFase func(ctx context.Context, thread uuid.UUID) (*faseRegistrata, error)
}

// ---------------------------------------------------------------- i dati della pagina (contratto §2.2, §2.4)

// paginaMotoreA: i dati della pagina e del frammento (contratto A1d §2.2), e il JSON di /motore-a/dati (§2.4).
type paginaMotoreA struct {
	ThreadID  uuid.UUID                `json:"thread_id"`
	Vista     string                   `json:"vista"`    // "distinta" | "documenti" | "rfq"
	Prodotto  string                   `json:"prodotto"` // il Rif scelto; "" senza prodotti
	Testa     testaMotoreA             `json:"testa"`
	Esito     *valutazione.EsitoThread `json:"esito"`     // nil se il calcolo non è riuscito
	Confronto *confronto.Esito         `json:"confronto"` // nil come sopra
	Errore    string                   `json:"errore"`    // il calcolo non è riuscito: si mostra così com'è
	Scrive    bool                     `json:"-"`         // i pulsanti dei gesti sono attivi (almeno operatore)
	Indice    *indiceMotoreA           `json:"-"`         // le ricerche, §2.3
	Calcolato time.Time                `json:"calcolato"` // Fotografia.PresaIl (il now() della transazione)
}

type testaMotoreA struct {
	Cliente            string          `json:"cliente"`
	Oggetto            string          `json:"oggetto"`
	RiferimentoCliente string          `json:"riferimento_cliente"`
	StatoThread        string          `json:"stato_thread"` // "APERTA" | "CHIUSA" (= Esito.Fascicolo.FaseThread)
	Fase               *faseRegistrata `json:"fase"`         // nil = nessuna fase aperta
}

// faseRegistrata: la fase di lavoro dalla vista v_thread_fase (EB7-3), letta dal gestore, non dall'esito.
type faseRegistrata struct {
	Nome      string    `json:"nome"`
	Etichetta string    `json:"etichetta"` // fase_catalogo.descrizione
	Inizio    time.Time `json:"inizio"`
	GgInFase  int32     `json:"gg_in_fase"`
	Semaforo  string    `json:"semaforo"` // bianco | rosso | giallo | verde (migrations/0001, la vista)
	Terminale bool      `json:"terminale"`
}

// ---------------------------------------------------------------- costanti, errori, viste

// versioneEsitoMotoreA: la versione dell'esito che la pagina sa leggere (X-07; A1d v1.1 §4.12): i campi EB7 di P7e.
// Un'altra versione dà Errore finché la pagina non è riletta contro di lei (T-A1d-17). Riletta l'11/10 contro
// valutazione-3 (D-CHIUS-3): cambiano solo le regole del tipo «finito» proposto (D-P7e-1, D-CHIUS-2, D-CHIUS-4) e due
// codici di diagnostica nuovi; nessun campo dell'esito cambia, quindi la pagina li mostra com'è.
const versioneEsitoMotoreA = "valutazione-3"

const (
	erroreCalcoloMotoreA = "Il calcolo del motore A non è riuscito: il dettaglio è nel log del server."
	erroreTempoMotoreA   = "Il calcolo del motore A non è finito nel tempo che l'indice delle regole concede: il dettaglio è nel log del server."
)

var (
	errRFQAssente   = errors.New("motore A: la RFQ non esiste")
	errTempoMotoreA = errors.New("motore A: tempo scaduto")
	errPanicMotoreA = errors.New("motore A: panic nel calcolo")
)

// Le viste della pagina (contratto §2.1; v1.1 §4.1): schede, non pagine diverse.
const (
	vistaDistinta  = "distinta"
	vistaDocumenti = "documenti"
	vistaRFQ       = "rfq"
)

// vistaMotoreA: la vista chiesta, se è una delle tre; altrimenti distinta.
func vistaMotoreA(v string) string {
	switch v {
	case vistaDistinta, vistaDocumenti, vistaRFQ:
		return v
	}
	return vistaDistinta
}

// prodottoScelto: il Rif chiesto, se è fra i prodotti valutati; altrimenti il primo; "" senza prodotti.
func prodottoScelto(e *valutazione.EsitoThread, rif string) string {
	if e == nil || len(e.ProdottiValutati) == 0 {
		return ""
	}
	for _, p := range e.ProdottiValutati {
		if p.Rif == rif {
			return p.Rif
		}
	}
	return e.ProdottiValutati[0].Rif
}

// ---------------------------------------------------------------- rotte e porta

// registraMotoreA monta le rotte della Distinta nuova (contratto §2.1, §3.4 A). Prima il flag, poi soloAdmin: la
// protezione sta nel punto di montaggio (piano 7.4.3). La pagina (A1d-P3, piano A1d backend §8 n.2) ha la stessa porta
// del JSON e dell'anteprima.
func (s *Server) registraMotoreA(mux *http.ServeMux) {
	mux.HandleFunc("GET /thread/{id}/motore-a", s.conMotoreA(s.soloAdmin(s.motoreAPagina)))
	mux.HandleFunc("GET /thread/{id}/motore-a/dati", s.conMotoreA(s.soloAdmin(s.motoreADati)))
	mux.HandleFunc("GET /thread/{id}/motore-a/anteprima/step-strutturale", s.conMotoreA(s.soloAdmin(s.motoreAAnteprimaStep)))

	// Gli altri metodi sugli stessi indirizzi (S-01 della revisione di A1d-P2; contratto §2.1 «a flag spento 404 a
	// tutti»): senza questi schemi il mux risponderebbe 405 con Allow prima della porta, e a flag spento la rotta si
	// distinguerebbe da un indirizzo che non esiste. Lo schema con il metodo è più specifico: GET e HEAD restano alle
	// righe qui sopra. Una riga gemella per ognuna delle tre, la pagina compresa (D-A1d-P2C-05).
	mux.HandleFunc("/thread/{id}/motore-a", s.conMotoreA(metodoNonAmmessoMotoreA))
	mux.HandleFunc("/thread/{id}/motore-a/dati", s.conMotoreA(metodoNonAmmessoMotoreA))
	mux.HandleFunc("/thread/{id}/motore-a/anteprima/step-strutturale", s.conMotoreA(metodoNonAmmessoMotoreA))
}

// metodoNonAmmessoMotoreA: un metodo diverso da GET e HEAD su un indirizzo di motore-a, a flag acceso. È il 405 che il
// mux darebbe da sé (Allow: GET, HEAD; nessuna sessione letta), ma dietro conMotoreA: a flag spento è 404 (S-01).
func metodoNonAmmessoMotoreA(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

// conMotoreA è la porta: a flag spento 404 a tutti, anonimi compresi, PRIMA della sessione (nessun GetSessione né
// ToccaSessione: R36); la rotta non si distingue da un indirizzo che non esiste, con ogni metodo (le righe gemelle di
// registraMotoreA, S-01). A flag acceso passa la mano.
func (s *Server) conMotoreA(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.MotoreA.Accesa {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}
}

// ---------------------------------------------------------------- il dato della pagina

// paginaMotoreADa prepara la pagina di un thread. Falso = la RFQ non esiste (404, P-12). Non restituisce mai un errore:
// ogni altro problema diventa Errore (testo generico) e una riga di log con il thread.
//
// Un panic fuori dal calcolo (la lettura della fase, la pagina composta, l'indice) diventa anche lui l'errore generico,
// con la pila nel log, e non una connessione chiusa senza risposta (R-02 e R-03 della revisione di A1d-P2): la pagina
// resta senza esito, indice e confronto, mai a metà.
func (s *Server) paginaMotoreADa(ctx context.Context, r *http.Request, thread uuid.UUID) (pagina *paginaMotoreA, trovata bool) {
	a := s.MotoreA // una fotografia per richiesta (T-A1d-15): le prove cambiano il flag a server acceso
	q := r.URL.Query()
	p := &paginaMotoreA{ThreadID: thread, Vista: vistaMotoreA(q.Get("vista")), Scrive: almeno(utenteDa(ctx), db.RuoloUtenteOperatore)}
	defer func() {
		if v := recover(); v != nil {
			s.Log.Error("motore A: panic nel preparare la pagina", "thread", thread, "panic", v, "pila", string(debug.Stack()))
			pagina = &paginaMotoreA{ThreadID: p.ThreadID, Vista: p.Vista, Scrive: p.Scrive, Testa: testaMotoreA{Fase: p.Testa.Fase},
				Errore: erroreCalcoloMotoreA}
			trovata = true
		}
	}()

	leggiFase := a.leggiFase
	if leggiFase == nil {
		leggiFase = s.faseMotoreA
	}
	fase, err := leggiFase(ctx, thread)
	if errors.Is(err, errRFQAssente) {
		return nil, false
	}
	if err != nil {
		s.logNonRiuscitoMotoreA(ctx, "motore A: la RFQ e la fase non si leggono", thread, err)
		p.Errore = erroreCalcoloMotoreA
		return p, true
	}
	p.Testa.Fase = fase

	if a.Regole == nil {
		p.Errore = cmp.Or(a.Errore, ErroreRegoleMotoreA)
		return p, true
	}

	f, e, c, err := s.calcolaConLimite(ctx, a, thread)
	if err != nil {
		if errors.Is(err, errTempoMotoreA) {
			s.Log.Warn("motore A: tempo scaduto", "thread", thread, "limite", a.Limite)
			p.Errore = erroreTempoMotoreA
		} else {
			s.logNonRiuscitoMotoreA(ctx, "motore A: calcolo non riuscito", thread, err)
			p.Errore = erroreCalcoloMotoreA
		}
		return p, true
	}

	componiPaginaMotoreA(p, f, e, c, q.Get("prodotto"))
	if p.Errore != "" {
		s.Log.Error("motore A: esito non usabile", "thread", thread, "errore", p.Errore)
	}
	return p, true
}

// logNonRiuscitoMotoreA: la riga di log di un passo della pagina che non è riuscito. Una richiesta chiusa da chi l'aveva
// fatta (una ricarica, un'altra pagina) non è un errore del server: Info e non Error, così il log non si riempie di
// ERROR falsi (R-04 della revisione di A1d-P2). La pagina ha comunque l'errore generico (la risposta non arriva).
func (s *Server) logNonRiuscitoMotoreA(ctx context.Context, msg string, thread uuid.UUID, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		s.Log.Info(msg+": la richiesta è stata chiusa da chi l'aveva fatta", "thread", thread, "err", err)
		return
	}
	s.Log.Error(msg, "thread", thread, "err", err)
}

// componiPaginaMotoreA: la pagina da un calcolo riuscito, senza DB (si prova in L1; T-A1d-18). Una versione dell'esito
// che la pagina non conosce (X-07), o un esito senza il thread chiesto, diventano Errore: mai una pagina a metà.
func componiPaginaMotoreA(p *paginaMotoreA, f fotorfq.Fotografia, e valutazione.Esito, c confronto.Esito, prodotto string) {
	if e.VersioneValutazione != versioneEsitoMotoreA {
		p.Errore = fmt.Sprintf("L'esito del motore A è nella versione «%s»: questa pagina legge solo «%s».",
			e.VersioneValutazione, versioneEsitoMotoreA)
		return
	}
	var ft *fotorfq.Thread
	for i := range f.Thread {
		if f.Thread[i].ID == p.ThreadID {
			ft = &f.Thread[i]
			break
		}
	}
	var et *valutazione.EsitoThread
	for i := range e.Thread {
		if e.Thread[i].ThreadID == p.ThreadID {
			et = &e.Thread[i]
			break
		}
	}
	if ft == nil || et == nil {
		p.Errore = erroreCalcoloMotoreA
		return
	}
	p.Testa = testaDa(f, ft, p.Testa.Fase)
	p.Esito = et
	p.Confronto = &c
	// le ricerche e l'esito vengono dalla stessa fotografia (piano §5.4.1)
	p.Indice = nuovoIndiceMotoreA(ft, et, &c)
	p.Prodotto = prodottoScelto(et, prodotto)
	p.Calcolato = f.PresaIl
}

// testaDa: la testa della pagina dalla fotografia, pura (contratto §2.2). Cliente: Foto.Clienti[ClienteID].RagioneSociale;
// Oggetto, RiferimentoCliente: thread_offerta (nil = ""); StatoThread: thread_offerta.stato.
func testaDa(f fotorfq.Fotografia, t *fotorfq.Thread, fase *faseRegistrata) testaMotoreA {
	out := testaMotoreA{StatoThread: t.Stato, Fase: fase}
	for _, c := range f.Clienti {
		if c.ID == t.ClienteID {
			out.Cliente = c.RagioneSociale
			break
		}
	}
	if t.Oggetto != nil {
		out.Oggetto = *t.Oggetto
	}
	if t.Riferimento != nil {
		out.RiferimentoCliente = *t.Riferimento
	}
	return out
}

// ---------------------------------------------------------------- il calcolo

// risultatoMotoreA: quello che la goroutine del calcolo consegna.
type risultatoMotoreA struct {
	f   fotorfq.Fotografia
	e   valutazione.Esito
	c   confronto.Esito
	err error
}

// calcolaConLimite chiama a.Calcola (o calcolaMotoreA) con il limite dell'indice e con un recover, in una goroutine
// (T-A1d-15): il calcolo in memoria non guarda il contesto, quindi il limite lo fa la select. Il canale ha posto per un
// risultato: la goroutine finisce anche dopo il tempo scaduto, e il suo risultato si butta (il motore è puro).
// a.Limite <= 0 = nessun limite (lo può fare solo una prova: Limiti.Valida rifiuta lo zero nell'indice).
//
// Un errore che arriva quando il limite è già scaduto è il tempo scaduto: le letture del caricatore guardano il
// contesto e possono rispondere prima della select; così un limite che scade dentro Carica dà sempre il testo del
// tempo (A1d-L4-08), non quello di un calcolo non riuscito.
func (s *Server) calcolaConLimite(ctx context.Context, a AnteprimaMotoreA, thread uuid.UUID) (fotorfq.Fotografia, valutazione.Esito, confronto.Esito, error) {
	calcola := a.Calcola
	if calcola == nil {
		calcola = func(ctx context.Context, th uuid.UUID) (fotorfq.Fotografia, valutazione.Esito, confronto.Esito, error) {
			return s.calcolaMotoreA(ctx, a, th)
		}
	}
	if a.Limite > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.Limite)
		defer cancel()
	}
	tempo := func() error {
		return fmt.Errorf("%w (%s): %w", errTempoMotoreA, a.Limite, ctx.Err())
	}
	fine := make(chan risultatoMotoreA, 1)
	go func() {
		// il recover sta qui dentro: un panic in una goroutine senza recupero fermerebbe il server. La pila va
		// nell'errore, che finisce solo nel log (mai al cliente): così si trova il punto del motore (R-03)
		defer func() {
			if v := recover(); v != nil {
				fine <- risultatoMotoreA{err: fmt.Errorf("%w: %v\n%s", errPanicMotoreA, v, debug.Stack())}
			}
		}()
		f, e, c, err := calcola(ctx, thread)
		fine <- risultatoMotoreA{f, e, c, err}
	}()
	select {
	case r := <-fine:
		if r.err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fotorfq.Fotografia{}, valutazione.Esito{}, confronto.Esito{}, tempo()
		}
		return r.f, r.e, r.c, r.err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fotorfq.Fotografia{}, valutazione.Esito{}, confronto.Esito{}, tempo()
		}
		// la richiesta l'ha chiusa chi l'aveva fatta: un errore qualunque per paginaMotoreADa
		return fotorfq.Fotografia{}, valutazione.Esito{}, confronto.Esito{}, fmt.Errorf("motore A: richiesta annullata: %w", ctx.Err())
	}
}

// calcolaMotoreA è il percorso di sempre, lo stesso di bancoa -dsn -thread (piano 7.4.3; R42 B):
//  1. caricatore.Carica sul pool, del solo thread, senza messaggi fuori RFQ (T-A1d-01): una transazione REPEATABLE READ
//     READ ONLY, chiusa prima del calcolo (caricatore.go:109);
//  2. valutazione.Calcola in memoria, con le regole e gli ingressi dell'avvio;
//  3. la copia di passaggio (motore_a_passaggio.go) e confronto.Confronta, senza atteso (R2).
//
// Non salva niente. L'EsitoThread è lo stesso del banco; Esito.Impronta no (T-A1d-01). Il motore è immutabile
// (motorea.CompilaVerificato): le richieste concorrenti condividono a.Regole senza lucchetti.
func (s *Server) calcolaMotoreA(ctx context.Context, a AnteprimaMotoreA, thread uuid.UUID) (fotorfq.Fotografia, valutazione.Esito, confronto.Esito, error) {
	f, err := caricatore.Carica(ctx, s.Pool, caricatore.Richiesta{Thread: []uuid.UUID{thread}})
	if err != nil {
		return fotorfq.Fotografia{}, valutazione.Esito{}, confronto.Esito{}, err
	}
	e, err := valutazione.Calcola(f, a.Regole, a.Ingressi)
	if err != nil {
		return fotorfq.Fotografia{}, valutazione.Esito{}, confronto.Esito{}, err
	}
	var et *valutazione.EsitoThread
	for i := range e.Thread {
		if e.Thread[i].ThreadID == thread {
			et = &e.Thread[i]
			break
		}
	}
	if et == nil {
		return fotorfq.Fotografia{}, valutazione.Esito{}, confronto.Esito{}, errors.New("motore A: l'esito non ha il thread chiesto")
	}
	c := confronto.Confronta(maInFiles(et.Confrontabili), maInProdotti(et.ProdottiConfrontabili), nil)
	return f, e, c, nil
}

// ---------------------------------------------------------------- la fase e l'esistenza della RFQ

// faseMotoreA legge, in una transazione READ ONLY sua (eccezione RO-DOMINIO dichiarata, T-A1d-02), l'esistenza della RFQ
// (GetThread, T-A1d-12: Carica dà un errore non tipizzato per una RFQ che non c'è, caricatore.go:238-239) e la fase
// registrata (GetFaseCorrente, la vista v_thread_fase; ListFaseCatalogo per l'etichetta). Nessuna fase aperta = nil.
func (s *Server) faseMotoreA(ctx context.Context, thread uuid.UUID) (*faseRegistrata, error) {
	return faseMotoreADa(ctx, s.Pool, thread)
}

// faseMotoreADa è faseMotoreA con chi apre la transazione passato da fuori (iniziatoreMA, motore_a_anteprima.go; lo
// soddisfa *pgxpool.Pool): la sola parte che si prova senza database (la modalità della transazione, il rollback, il
// 404 dalla RFQ che non c'è). Le tre query sono semplici SELECT già generate (§12, RK-09); mai un commit.
func faseMotoreADa(ctx context.Context, p iniziatoreMA, thread uuid.UUID) (*faseRegistrata, error) {
	tx, err := p.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("motore A: la transazione in sola lettura della fase non si apre: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // in sola lettura non c'è niente da confermare
	q := db.New(tx)
	if _, err := q.GetThread(ctx, thread); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errRFQAssente
		}
		return nil, fmt.Errorf("motore A: la RFQ non si legge: %w", err)
	}
	v, err := q.GetFaseCorrente(ctx, thread)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("motore A: la fase non si legge: %w", err)
	}
	cat, err := q.ListFaseCatalogo(ctx)
	if err != nil {
		return nil, fmt.Errorf("motore A: il catalogo delle fasi non si legge: %w", err)
	}
	descrizione := ""
	for _, c := range cat {
		if c.NomeFase == v.NomeFase {
			descrizione = c.Descrizione
			break
		}
	}
	return &faseRegistrata{Nome: string(v.NomeFase), Etichetta: descrizione, Inizio: v.Inizio, GgInFase: v.GgInFase,
		Semaforo: v.Semaforo, Terminale: v.Terminale}, nil
}

// ---------------------------------------------------------------- la pagina (A1d-P3)

// motoreAPagina: GET /thread/{id}/motore-a. La pagina intera (layout + contenuto) o, con htmx, il frammento
// motore_a_corpo (contratto §2.1, §3.3; piano A1d backend §8 n.3). Cache-Control: no-store; HEAD: solo le intestazioni,
// nessun calcolo e nessuna lettura (T-A1d-13, come /dati). Una RFQ che non c'è è un 404 (P-12); ogni altro problema è
// la pagina con Errore (paginaMotoreADa). Non scrive niente: nessun job, nessun prepara (DB7-6), nessuna
// sincronizzazione; le letture sono quelle di paginaMotoreADa.
func (s *Server) motoreAPagina(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	thread, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id non valido", 400)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		return
	}
	p, ok := s.paginaMotoreADa(r.Context(), r, thread)
	if !ok {
		http.Error(w, "RFQ non trovata", 404)
		return
	}
	s.rendiMotoreA(w, r, p)
}

// rendiMotoreA è rendi (server.go) in un buffer (T-A1d-05): la pagina intera («layout») o, con HX-Request senza
// HX-History-Restore-Request (frammentoRichiesto), «motore_a_corpo». La vista è quella di rendi: Utente e Admin
// sempre, Stato solo per la pagina con un utente (nessuna lettura del DB senza utente nel contesto); il titolo è
// «Distinta» (contratto §2.2). Un errore di template, anche il panic di una ricerca dell'indice (text/template lo
// recupera e lo restituisce come errore), è un 500 testuale, mai una pagina a metà: le intestazioni e i byte si
// scrivono solo dopo l'esecuzione riuscita.
func (s *Server) rendiMotoreA(w http.ResponseWriter, r *http.Request, p *paginaMotoreA) {
	t := s.pagine["motore_a.html"]
	if t == nil {
		http.Error(w, "pagina non disponibile", 500)
		return
	}
	v := vista{Utente: utenteDa(r.Context()), Titolo: "Distinta", Dati: p, Frammento: frammentoRichiesto(r)}
	v.Admin = almeno(v.Utente, db.RuoloUtenteAdmin)
	if !v.Frammento && v.Utente != nil {
		v.Stato = s.stato(r.Context(), sessioneDa(r.Context()))
	}
	nome := "layout"
	if v.Frammento {
		nome = "motore_a_corpo"
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, nome, v); err != nil {
		s.Log.Error("template", "pagina", "motore_a.html", "err", err)
		http.Error(w, "errore nel disegnare la pagina", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// ---------------------------------------------------------------- i gestori di P2

// motoreADati: GET /thread/{id}/motore-a/dati. Il JSON di §2.4, per il pannello «Dati e chiamate»; mai con htmx
// (la guardia di layout.html scarta le 2xx non HTML). HEAD: solo le intestazioni, nessun calcolo e nessuna lettura
// (T-A1d-13): dice 200 anche per una RFQ che non c'è, che con GET è 404 (da ratificare con T-A1d-13).
// Cache-Control: no-store su ogni risposta, anche il 400 e il 404: i dati cambiano con ogni gesto. Il JSON ha anche
// X-Content-Type-Options: nosniff (I-02 della revisione di A1d-P2; i 400 e i 404 di http.Error lo hanno già).
func (s *Server) motoreADati(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	thread, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id non valido", 400)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		return
	}
	p, ok := s.paginaMotoreADa(r.Context(), r, thread)
	if !ok {
		http.Error(w, "RFQ non trovata", 404)
		return
	}
	scriviDatiMotoreA(w, p)
}

// scriviDatiMotoreA: il JSON della pagina in un buffer, poi le intestazioni (application/json; charset=utf-8, no-store,
// nosniff) e i byte. Senza DB: si prova in L1 (T-A1d-18).
func scriviDatiMotoreA(w http.ResponseWriter, p *paginaMotoreA) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(p); err != nil {
		// non capita con questi tipi; mai un JSON a metà con un 200 già partito
		http.Error(w, "dati non disponibili", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(buf.Bytes())
}

// ---------------------------------------------------------------- il ramo dell'esito dei gesti (§5.1.8)
//
// La risposta dei gesti partiti dalla Distinta nuova (contratto A1d §3.1; v1.1 §4.10; piano A1d backend §5.1.8 e §6;
// P-07 b; T-A1d-07, T-A1d-11, T-A1d-36).
//
// I gesti della pagina /thread/{id}/motore-a sono le rotte di sempre: le chiama il ponte (web/static/motore_a_gesti.mjs)
// con HX-Request e con HX-Current-URL = l'indirizzo della pagina. Le loro risposte passano tutte da threadFrammento,
// che oggi sceglie la vecchia interfaccia dall'intestazione (la Distinta, il Fascicolo, la pagina della RFQ) e dice un
// rifiuto con HTTP 200, dentro il frammento. Per la pagina nuova threadFrammento ha un ramo, il primo, solo a flag
// acceso: risponde con un solo <div data-ma-esito="ok|no">testo</div> e mai con la vecchia interfaccia. Il ponte legge
// l'esito e non inserisce niente nella pagina; dopo un «ok» rilegge #ma.
//
// «no» vuol dire che niente è cambiato: le frasi di avvisoNegativo (distinta.go), più un elenco chiuso di frasi di
// rifiuto che avvisoNegativo legge «ok» (P-07 b, T-A1d-11). L'elenco vale solo in questo ramo: avvisoNegativo, la
// Distinta e il Fascicolo legacy non cambiano. Le frasi idempotenti («Nessun cambiamento: …», «… è già …», «Niente da
// preparare…») restano «ok»: il gesto è andato e lo stato è quello chiesto. La guardia che classifica le frasi di
// tutti i chiamanti di threadFrammento è TestMotoreALeFrasiDeiGestiSonoClassificate (motore_a_test.go, T-A1d-39):
// chi aggiunge una frase con 200 deve dire se è un rifiuto.

// dallaMotoreA dice se la richiesta viene dalla Distinta nuova di questa RFQ: il percorso di HX-Current-URL è
// /thread/{id}/motore-a (la / finale tollerata, la query ignorata). Il thread si confronta come UUID, non come testo:
// un indirizzo scritto con le maiuscole è la stessa pagina (T-A1d-07).
func dallaMotoreA(h interface{ Get(string) string }, thread uuid.UUID) bool {
	u, err := url.Parse(h.Get("HX-Current-URL"))
	if err != nil {
		return false
	}
	resto, ok := strings.CutPrefix(strings.TrimSuffix(u.Path, "/"), "/thread/")
	if !ok {
		return false
	}
	id, coda, ok := strings.Cut(resto, "/")
	if !ok || coda != "motore-a" {
		return false
	}
	t, err := uuid.Parse(id)
	return err == nil && t == thread
}

// rifiutiMotoreA: l'elenco chiuso delle frasi con cui un gesto dice, con HTTP 200, che non ha fatto niente e che
// avvisoNegativo legge «ok» (P-07 b; T-A1d-11; l'elenco con file e riga sta nel §6 del piano di A1d backend). Una frase
// vale se comincia con prefisso e contiene contiene ("" = qualunque). Solo nel ramo nuovo: avvisoNegativo e la
// Distinta legacy non cambiano. Il confronto è per prefisso, come quello di avvisoNegativo: una frase riuscita che
// cominciasse con il nome di un nodo uguale a un prefisso si leggerebbe «no» (R-4 della revisione di A1d-P2, in DUBBI).
// Le righe dei commenti valgono per i file di A1d-P2; il nome della funzione non si sposta.
var rifiutiMotoreA = []struct{ prefisso, contiene string }{
	{"Copie NON rimesse in coda", ""},                 // thread.go:227 (esitoCopie.Frase), riprova-copie: la capacità è spenta
	{"Nessuna copia nuova", " non accodat"},           // thread.go:249 con :234 (esitoCopie.Frase), riprova-copie: niente accodato, per errori
	{"Le proposte di una RFQ ", " non si aggiornano"}, // smistamento.go:150, RFQ chiusa o unita a un'altra
	{"Nessun file scelto", ""},                        // fascicolo.go:740, assegna senza file
	// fuori dal ponte (rotte fuori dalla lista del contratto), quelli che il §6.2 del piano elenca; l'elenco dei
	// rifiuti fuori dal ponte non è completo (agente.go, richieste.go:244/:300/:344/:367, triage.go: D-PONTE-02, R-3
	// della revisione di A1d-P2), nessuno di loro arriva dalle rotte del ponte:
	{"Prima crea", ""},            // allegati.go:156, :267 (scarica, riscarica)
	{"Scegli un fornitore", ""},   // richieste.go:59
	{"Fornitore non trovato", ""}, // richieste.go:64
	{"Richiesta non creata", ""},  // richieste.go:86
	{"Richiesta non trovata", ""}, // richieste.go:219
}

// esitoMotoreA: "no" se il testo è un rifiuto (le frasi di avvisoNegativo o dell'elenco chiuso), altrimenti "ok". Le
// frasi idempotenti («Nessun cambiamento: …», «… è già …», «Niente da preparare…») restano «ok».
func esitoMotoreA(avviso string) string {
	if avvisoNegativo(avviso) {
		return "no"
	}
	for _, r := range rifiutiMotoreA {
		if strings.HasPrefix(avviso, r.prefisso) && strings.Contains(avviso, r.contiene) {
			return "no"
		}
	}
	return "ok"
}

// rispondiMotoreA risponde a un gesto partito dalla Distinta nuova: 200, text/html, no-store, un solo elemento
// <div data-ma-esito="ok|no">testo</div>, con l'escape di html/template; mai la vecchia interfaccia, nessuna lettura
// del DB. Un avviso vuoto dà <div data-ma-esito="ok"></div>: il ponte scrive il testo di P-09 (T-A1d-32).
func (s *Server) rispondiMotoreA(w http.ResponseWriter, avviso string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `<div data-ma-esito="%s">%s</div>`, esitoMotoreA(avviso), template.HTMLEscapeString(avviso))
}
