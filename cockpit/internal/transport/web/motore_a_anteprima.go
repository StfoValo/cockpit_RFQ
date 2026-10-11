package web

// motore_a_anteprima.go: l'anteprima dell'autorizzazione dello STEP strutturale in JSON, per la Distinta nuova del
// motore A (giro 5, A1d-P2; BE-11; contratto A1d §3.4 A; v1.1 §4.8; piano A1d backend §5.6; T-A1d-03, T-A1d-28).
//
// La pagina /thread/{id}/motore-a non ha il modulo HTML del Fascicolo: prima del gesto «Autorizza lo STEP
// strutturale» legge questa anteprima con il ponte (leggi, una fetch) e la mostra in una finestra sua. Il calcolo è
// quello del Fascicolo, riusato e non copiato: la richiesta la legge richiestaAutorizzazione (fascicolo_rotte.go:292),
// l'effetto lo calcola fascicolo.EffettoAutorizzazione (autorizzazione.go:151), le frasi sono le righe del modulo
// legacy fasc_autorizzazione (fascicolo.html:1198-1235), nello stesso ordine. Nessuna regola nuova: la sola
// differenza dichiarata dal gestore legacy (anteprimaAutorizzazione, fascicolo_rotte.go:338, che legge sul pool) è la
// transazione READ ONLY e REPEATABLE READ (T-A1d-03). La rotta la monta registraMotoreA (motore_a.go) dietro
// conMotoreA e soloAdmin: qui non si guardano né il flag né il ruolo, salvo per la firma.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"promatec/cockpit/internal/core/rfq/fascicolo"
	"promatec/cockpit/internal/platform/db"
)

// anteprimaStepMA: l'anteprima dell'autorizzazione dello STEP strutturale in JSON (contratto §3.4 A; v1.1 §4.8). I
// campi della v1 restano; la v1.1 aggiunge gia, autorizzabile, inviabile, figli, deleghe[].spenta e deleghe[].revoca.
// Gli elenchi ci sono sempre ([]), mai null.
type anteprimaStepMA struct {
	Spento         string                  `json:"spento,omitempty"`
	Errore         string                  `json:"errore,omitempty"`
	Gia            string                  `json:"gia,omitempty"` // v1.1: «X è già lo STEP autorizzato di Y»
	Autorizzabile  bool                    `json:"autorizzabile"` // v1.1: Effetto.Autorizzabile() (autorizzazione.go:137)
	Inviabile      bool                    `json:"inviabile"`     // v1.1: il modulo legacy avrebbe il bottone
	Frasi          []string                `json:"frasi"`
	Figli          []fascicolo.NodoEffetto `json:"figli"` // v1.1: le chiavi sono i tag di NodoEffetto (RAC-10)
	Bottone        string                  `json:"bottone,omitempty"`
	Firma          string                  `json:"firma,omitempty"` // solo dal ruolo operatore in su
	PresaDAtto     string                  `json:"presa_d_atto,omitempty"`
	Sorgenti       []sceltaMA              `json:"sorgenti"`
	Raggruppamenti []sceltaMA              `json:"raggruppamenti"`
	Deleghe        []delegaMA              `json:"deleghe"`
}

// sceltaMA: una scelta della finestra (una radice da indicare, un raggruppamento da dichiarare): la chiave del nodo,
// che il gesto rimanda (sorgente, raggruppamento), e il testo del modulo legacy.
type sceltaMA struct {
	Chiave    string `json:"chiave"`
	Etichetta string `json:"etichetta"`
}

// delegaMA: una delega possibile (fascicolo.DelegaPossibile) con il testo del modulo legacy, il motivo per cui è
// spenta e le autorizzazioni che revocherebbe, nominate (v1.1). Revoca è [] e mai null.
type delegaMA struct {
	Chiave    string   `json:"chiave"`
	Etichetta string   `json:"etichetta"`
	Spenta    string   `json:"spenta,omitempty"` // v1.1
	Revoca    []string `json:"revoca"`           // v1.1
}

// conElenchi: gli elenchi vuoti diventano [] (mai null nel JSON, v1.1 §4.8), anche la revoca di ogni delega: la
// finestra della pagina li scorre senza controllare che ci siano.
func (a anteprimaStepMA) conElenchi() anteprimaStepMA {
	if a.Frasi == nil {
		a.Frasi = []string{}
	}
	if a.Figli == nil {
		a.Figli = []fascicolo.NodoEffetto{}
	}
	if a.Sorgenti == nil {
		a.Sorgenti = []sceltaMA{}
	}
	if a.Raggruppamenti == nil {
		a.Raggruppamenti = []sceltaMA{}
	}
	if a.Deleghe == nil {
		a.Deleghe = []delegaMA{}
	}
	for i := range a.Deleghe {
		if a.Deleghe[i].Revoca == nil {
			a.Deleghe[i].Revoca = []string{}
		}
	}
	return a
}

// motoreAAnteprimaStep: GET /thread/{id}/motore-a/anteprima/step-strutturale?componente={cid}&documento=…|nodo=…
// &sorgente=…; riusa richiestaAutorizzazione (fascicolo_rotte.go:292) e fascicolo.EffettoAutorizzazione
// (autorizzazione.go:151), con il componente in query al posto di {cid}, in una transazione READ ONLY e REPEATABLE
// READ sua (T-A1d-03). Un rifiuto è 200 con errore; altro è 500 testuale; Cache-Control: no-store.
//
// Come il gestore legacy (fascicolo_rotte.go:338-376): id non valido o form illeggibile → 400, poi tutto è JSON. Un
// componente che non è un UUID è 200 con errore, come distintaTipo (distinta.go:822-824), e non legge il database; i
// rifiuti della richiesta (documento o nodo non validi, tutti e due insieme) e quelli del calcolo («il componente non
// è di questa RFQ», «il nodo indicato non è una radice di …») sono 200 con errore = spiegaErrore; un errore del
// database è 500 «anteprima non riuscita», con il dettaglio nel log. Una RFQ che non esiste dà «il componente non è
// di questa RFQ», non 404, come il legacy (D-A1d-16). La firma c'è solo dal ruolo operatore in su: la rotta è
// dell'admin (soloAdmin ferma gli altri prima), la regola resta qui come nel legacy (distinta.go:839).
func (s *Server) motoreAAnteprimaStep(w http.ResponseWriter, r *http.Request) {
	// nessuna risposta di questa rotta si tiene in cache: l'anteprima cambia con ogni gesto, anche di un collega
	w.Header().Set("Cache-Control", "no-store")
	thread, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "id non valido", 400)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "richiesta non valida", 400)
		return
	}
	var out anteprimaStepMA
	cid, err := uuid.Parse(r.Form.Get("componente"))
	if err != nil {
		out = anteprimaStepMA{Errore: "componente non valido"}.conElenchi()
	} else {
		var e fascicolo.Effetto
		req, err := richiestaAutorizzazione(r, cid)
		if err == nil {
			e, err = s.effettoInSolaLettura(r.Context(), thread, req)
		}
		if err != nil {
			var rf rifiuto
			if !errors.As(err, &rf) {
				s.Log.Error("motore A: anteprima dello STEP non riuscita", "thread", thread, "componente", cid, "err", err)
				http.Error(w, "anteprima non riuscita", 500)
				return
			}
			out = anteprimaStepMA{Errore: spiegaErrore(err)}.conElenchi()
		} else {
			out = anteprimaStepDa(e, almeno(utenteDa(r.Context()), db.RuoloUtenteOperatore))
		}
	}
	// prima il buffer, poi le intestazioni: mai un JSON a metà con un 200 già partito
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		s.Log.Error("motore A: anteprima dello STEP non codificata", "thread", thread, "componente", cid, "err", err)
		http.Error(w, "anteprima non riuscita", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff") // I-02 della revisione di A1d-P2, come i 400 e i 500 di http.Error
	_, _ = w.Write(buf.Bytes())
}

// effettoInSolaLettura: EffettoAutorizzazione su db.New(tx) di una transazione READ ONLY, REPEATABLE READ, sempre
// chiusa con il rollback. Un errore con SQLSTATE 25006 (una lettura che scrive o blocca) va nel log con il codice.
// Nessun ripiego sul pool nel codice (T-A1d-03): le query raggiunte sono semplici SELECT (piano §12, RK-09) e la prova
// sul database è A1d-L4-05; se trovasse un 25006, la scelta è di Pietro.
func (s *Server) effettoInSolaLettura(ctx context.Context, thread uuid.UUID, req fascicolo.RichiestaAutorizzazione) (fascicolo.Effetto, error) {
	return effettoInSolaLetturaDa(ctx, s.Pool, s.Log, thread, req)
}

// iniziatoreMA: chi apre la transazione dell'anteprima. Lo soddisfa *pgxpool.Pool (s.Pool), come
// caricatore.Iniziatore; la prova L1 passa un iniziatore finto, che registra le opzioni e risponde senza database.
type iniziatoreMA interface {
	BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error)
}

// effettoInSolaLetturaDa è effettoInSolaLettura con l'iniziatore e il log passati da fuori (la sola parte che si
// prova senza database). Le opzioni sono quelle del caricatore (caricatore.go:76): REPEATABLE READ, READ ONLY.
func effettoInSolaLetturaDa(ctx context.Context, p iniziatoreMA, log *slog.Logger, thread uuid.UUID, req fascicolo.RichiestaAutorizzazione) (fascicolo.Effetto, error) {
	tx, err := p.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fascicolo.Effetto{}, fmt.Errorf("motore A: la transazione in sola lettura dell'anteprima non si apre: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // in sola lettura non c'è niente da confermare: mai un commit
	e, err := fascicolo.EffettoAutorizzazione(ctx, db.New(tx), thread, req)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "25006" {
		// read_only_sql_transaction: una delle letture di EffettoAutorizzazione scrive o blocca (RK-09)
		log.Error("motore A: l'anteprima dello STEP ha chiesto di scrivere in una transazione in sola lettura",
			"thread", thread, "componente", req.Componente, "sqlstate", pg.Code, "err", err)
	}
	return e, err
}

// anteprimaStepDa: dal risultato del dominio al JSON, puro (si prova in L1; T-A1d-18). Le frasi sono le righe che oggi
// scrive il modulo legacy fasc_autorizzazione (fascicolo.html), nello stesso ordine (T-A1d-28): il titolo; con il
// riquadro spento nient'altro (il motivo sta in spento); poi «già», la domanda delle radici, e con la sorgente i figli
// diretti, la guida, le rimozioni, le revoche e gli avvisi. Le scelte (radici, raggruppamenti, deleghe) e il bottone
// hanno i testi del modulo. firma: chi chiede arriva almeno al ruolo operatore; senza, la firma non c'è e niente è
// inviabile, come nel modulo legacy, che senza Scrive non ha il form (fascicolo.html:1218).
func anteprimaStepDa(e fascicolo.Effetto, firma bool) anteprimaStepMA {
	codice := e.Componente.Codice
	titolo := "Autorizzazione: "
	if e.Delega {
		titolo = "Delega: "
	}
	titolo += cmp.Or(e.NomeFile, "lo STEP") + " per " + codice
	if e.Titolare != "" {
		titolo += " (nel file, sotto " + e.Titolare + ")"
	}
	out := anteprimaStepMA{Autorizzabile: e.Autorizzabile(), Frasi: []string{titolo}}
	if e.Spento != "" {
		// il riquadro spento dice solo perché: niente scelte, niente bottone, niente firma, niente da inviare
		out.Spento = e.Spento
		return out.conElenchi()
	}
	if e.Gia != "" {
		out.Gia = e.Gia
		out.Frasi = append(out.Frasi, e.Gia+".")
	}
	if e.Sorgente == nil && len(e.Radici) > 0 {
		// più radici e nessuna indicata: la persona sceglie, e nessuna è scelta in partenza (U7)
		out.Frasi = append(out.Frasi, fmt.Sprintf("Il file ha %d radici: quale è %s? Nessuna è scelta.", len(e.Radici), codice))
		for _, n := range e.Radici {
			out.Sorgenti = append(out.Sorgenti, sceltaMA{Chiave: n.Chiave, Etichetta: fmt.Sprintf("%s (%d figli nel file)", n.Nome, n.Figli)})
		}
	}
	if e.Sorgente != nil {
		out.Frasi = append(out.Frasi, fmt.Sprintf("%d %s nell'autorità di %s:", len(e.Figli),
			plurale(len(e.Figli), "figlio diretto entra", "figli diretti entrano"), codice))
		out.Figli = e.Figli
		sotto := ""
		for _, x := range e.GuidaSotto {
			sotto += " · " + x
		}
		out.Frasi = append(out.Frasi, fmt.Sprintf("%d %s%s: si vedono, e non entrano nella BOM senza un'altra autorizzazione.",
			e.Guida, plurale(e.Guida, "nodo del file resta guida", "nodi del file restano guida"), sotto))
		for _, x := range e.Rimozioni {
			out.Frasi = append(out.Frasi, "Rimozione: "+x)
		}
		if e.RimozioniSospese != "" {
			out.Frasi = append(out.Frasi, "Rimozioni: "+e.RimozioniSospese+".")
		}
		for _, x := range e.Revoche {
			out.Frasi = append(out.Frasi, "Un componente, un'autorizzazione: "+x+" si revoca.")
		}
		for _, x := range e.Avvisi {
			out.Frasi = append(out.Frasi, x+".")
		}
		out.PresaDAtto = e.PresaDAtto
		for _, g := range e.Raggruppamenti {
			out.Raggruppamenti = append(out.Raggruppamenti, sceltaMA{Chiave: g.Nodo.Chiave,
				Etichetta: fmt.Sprintf("%s è %s (un contenitore): i suoi %d figli diventano figli diretti", g.Nodo.Nome, codice, len(g.Figli))})
		}
		for _, d := range e.Deleghe {
			out.Deleghe = append(out.Deleghe, delegaMA{Chiave: d.Nodo.Chiave,
				Etichetta: fmt.Sprintf("Autorizza questo STEP anche per %s (%s, %d figli nel file)",
					cmp.Or(d.Codice, "il componente di "+d.Nodo.Nome+", quando sarà deciso"), d.Nodo.Nome, d.Nodo.Figli),
				Spenta: d.Spenta, Revoca: d.Revoca})
		}
		switch {
		case e.Gia == "":
			out.Bottone = e.Bottone()
		case len(e.Deleghe) > 0 || len(e.Raggruppamenti) > 0:
			out.Bottone = "Autorizza le scelte"
		}
	}
	if firma {
		out.Firma = e.Firma
	}
	// la condizione del bottone di fasc_autorizzazione: il form c'è solo con Scrive (fascicolo.html:1218), il bottone
	// solo senza «già» oppure con deleghe o raggruppamenti da scegliere (:1228)
	out.Inviabile = firma && e.Spento == "" && e.Sorgente != nil && (e.Gia == "" || len(e.Deleghe) > 0 || len(e.Raggruppamenti) > 0)
	return out.conElenchi()
}
