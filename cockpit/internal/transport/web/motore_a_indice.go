package web

import (
	"path"
	"strings"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/ancoraggio"
	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/fotorfq"
	"promatec/cockpit/internal/core/valutazione"
	"promatec/cockpit/internal/platform/db"
)

// motore_a_indice.go: l'indice della Distinta nuova del motore A (giro 5, A1d; BE-09; contratto A1d §2.3; v1.1 §4.3;
// X-02…X-06, X-09; T-A1d-09, T-A1d-10, T-A1d-19…T-A1d-27). Il template non scorre gli elenchi dell'esito per trovare
// un dato: chiama questi metodi di .Dati.Indice, che fanno solo ricerche sulla fotografia e sull'esito dello stesso
// calcolo. Gli 11 metodi del contratto (§2.3) e le 14 ricerche in più (v1.1 §4.3) leggono campi e confrontano valori:
// nessuna regola di dominio, nessuno stato calcolato, nessuna query.

// indiceMotoreA: le ricerche della pagina (contratto A1d §2.3; v1.1 §4.3) sulla stessa fotografia e sullo stesso esito
// del calcolo: niente SQL in più, nessuna regola, nessuna corrispondenza inventata, nessun Rif spezzato. Nil-sicuro: un
// indice nil risponde con il valore zero. Le mappe servono solo a cercare, mai a scorrere.
type indiceMotoreA struct {
	t          *fotorfq.Thread
	e          *valutazione.EsitoThread
	c          *confronto.Esito
	allegati   map[uuid.UUID]*fotorfq.Allegato
	file       map[uuid.UUID]*valutazione.FileInterpretato
	assoc      map[uuid.UUID]*valutazione.AssociazioneFile
	badge      map[uuid.UUID]*confronto.EsitoFile
	prodotti   map[string]*valutazione.ProdottoValutato
	componenti map[uuid.UUID]*fotorfq.Componente
	anteprime  map[uuid.UUID]*valutazione.RiferimentoAnteprima // il primo 2D dell'esito per allegato (Anteprima, X-09)
	gruppi     map[string][]gruppoMA                           // per prodotto, alla prima richiesta
	nodi       map[string][]*valutazione.NodoBOM               // per prodotto+"\x00"+gruppo, alla prima richiesta
}

// nuovoIndiceMotoreA: nil se l'esito è nil; con t o c nil le ricerche che li usano rispondono vuoto. Le mappe tengono il
// primo elemento per chiave, come una ricerca lineare dall'inizio.
func nuovoIndiceMotoreA(t *fotorfq.Thread, e *valutazione.EsitoThread, c *confronto.Esito) *indiceMotoreA {
	if e == nil {
		return nil
	}
	x := &indiceMotoreA{t: t, e: e, c: c,
		allegati:   map[uuid.UUID]*fotorfq.Allegato{},
		file:       map[uuid.UUID]*valutazione.FileInterpretato{},
		assoc:      map[uuid.UUID]*valutazione.AssociazioneFile{},
		badge:      map[uuid.UUID]*confronto.EsitoFile{},
		prodotti:   map[string]*valutazione.ProdottoValutato{},
		componenti: map[uuid.UUID]*fotorfq.Componente{},
		anteprime:  map[uuid.UUID]*valutazione.RiferimentoAnteprima{},
		gruppi:     map[string][]gruppoMA{},
		nodi:       map[string][]*valutazione.NodoBOM{},
	}
	if t != nil {
		for i := range t.Allegati {
			if _, ok := x.allegati[t.Allegati[i].ID]; !ok {
				x.allegati[t.Allegati[i].ID] = &t.Allegati[i]
			}
		}
		for i := range t.Componenti {
			if _, ok := x.componenti[t.Componenti[i].ID]; !ok {
				x.componenti[t.Componenti[i].ID] = &t.Componenti[i]
			}
		}
	}
	for i := range e.File {
		if _, ok := x.file[e.File[i].AllegatoID]; !ok {
			x.file[e.File[i].AllegatoID] = &e.File[i]
		}
	}
	for i := range e.Associazioni {
		if _, ok := x.assoc[e.Associazioni[i].AllegatoID]; !ok {
			x.assoc[e.Associazioni[i].AllegatoID] = &e.Associazioni[i]
		}
	}
	if c != nil {
		for i := range c.File {
			if _, ok := x.badge[c.File[i].File.AllegatoID]; !ok {
				x.badge[c.File[i].File.AllegatoID] = &c.File[i]
			}
		}
	}
	for i := range e.ProdottiValutati {
		if _, ok := x.prodotti[e.ProdottiValutati[i].Rif]; !ok {
			x.prodotti[e.ProdottiValutati[i].Rif] = &e.ProdottiValutati[i]
		}
	}
	// Il primo 2D dell'esito per allegato (X-09; T-A1d-19), in quest'ordine: per ogni prodotto le voci dei documenti, i
	// previsti, i nodi della BOM di lavoro, i nodi delle candidate, i confermati senza STEP.
	for i := range e.ProdottiValutati {
		p := &e.ProdottiValutati[i]
		for j := range p.Documenti.Voci {
			x.anteprimeDelGruppo(p.Documenti.Voci[j].Disegni)
		}
		for j := range p.Documenti.Previsti {
			x.anteprimeDelGruppo(p.Documenti.Previsti[j].Disegni)
		}
		for j := range p.Nodi {
			x.anteprimeDelGruppo(&p.Nodi[j].Disegni)
		}
		for k := range p.StruttureCandidate {
			for j := range p.StruttureCandidate[k].Nodi {
				x.anteprimeDelGruppo(&p.StruttureCandidate[k].Nodi[j].Disegni)
			}
		}
		for j := range p.ConfermatiSenzaSTEP {
			x.anteprimeDelGruppo(&p.ConfermatiSenzaSTEP[j].Disegni)
		}
	}
	return x
}

// anteprimeDelGruppo: il primario se c'è, poi gli alternativi; un gruppo nil (un puntatore dell'esito) si salta.
func (x *indiceMotoreA) anteprimeDelGruppo(g *valutazione.GruppoDisegni2D) {
	if g == nil {
		return
	}
	if g.Primario != nil {
		x.anteprimaDelDisegno(g.Primario)
	}
	for i := range g.Alternativi {
		x.anteprimaDelDisegno(&g.Alternativi[i])
	}
}

// anteprimaDelDisegno: la copia dell'Anteprima del 2D, per il suo allegato (Disegno2D.AllegatoID è un puntatore), se
// l'allegato non ne ha già una.
func (x *indiceMotoreA) anteprimaDelDisegno(d *valutazione.Disegno2D) {
	if d.AllegatoID == nil {
		return
	}
	if _, ok := x.anteprime[*d.AllegatoID]; ok {
		return
	}
	r := copiaAnteprimaMA(d.Anteprima)
	x.anteprime[*d.AllegatoID] = &r
}

// vuoto: l'indice non risponde (nil, o senza esito).
func (x *indiceMotoreA) vuoto() bool { return x == nil || x.e == nil }

// ---- le forme di ritorno (v1.1 §4.3; piano del frontend §4.4) ----

// autorizzazioneMA: i campi dell'anteprima dello STEP strutturale (contratto §3.4 A; T-A1d-23).
type autorizzazioneMA struct {
	Documento *uuid.UUID // documento.id dello STEP: il campo «documento» del gesto
	Nodo      string     // «<allegato_id>:<chiave>»: il campo «nodo» (la delega)
	Sorgente  string     // la chiave della radice: il campo «sorgente»
}

// relazioneMA: la riga aperta di relazione_proposta di un arco (contratto §3.4 B).
type relazioneMA struct {
	Allegato uuid.UUID
	Padre    string // relazione_proposta.padre_chiave
	Figlio   string // relazione_proposta.figlio_chiave
}

// rimozioneMA: la rimozione_proposta aperta di un conflitto (contratto §3.4 B).
type rimozioneMA struct{ Step, Padre, Figlio uuid.UUID }

// gruppoMA: una scheda dell'albero (X-02, X-03; T-A1d-09, T-A1d-27). Chiave opaca, mai spezzata (T-A1d-FE-01).
type gruppoMA struct {
	Chiave     string                          // "bom" | "cand:<allegato_id>:<radice>" | "senza"
	Tipo       string                          // "bom" | "cand" | "senza"
	AllegatoID *uuid.UUID                      // lo STEP del gruppo; nil per "senza"
	Radice     string                          // il Rif della radice della struttura; "" per "senza"
	NomeFile   string                          // il nome del file dello STEP; "" per "senza"
	Struttura  *valutazione.StrutturaCandidata // solo per "cand"
	Nodi       []*valutazione.NodoBOM          // i nodi del gruppo, ognuno una volta, nell'ordine del gruppo
}

// figlioMA: un figlio di un nodo nel gruppo (X-04; contratto §1.6.1).
type figlioMA struct {
	Nodo               *valutazione.NodoBOM
	Quantita           *int // dai fatti dello STEP; nil per un confermato senza STEP (T-A1d-20)
	QuantitaConfermata *int // la quantità della relazione confermata (contratto §1.6.1)
	Decisa             bool // Parentela.Decisa
}

// legamiMA: «nel file sotto X» e «confermato sotto Y», sempre accanto, mai fusi (X-05; v1.1 §4.5).
type legamiMA struct {
	DelFile    []legameFileMA
	Confermati []legameConfermatoMA
}

// legameFileMA: un padre del nodo nel file (la Parentela), con la quantità dei fatti e quella confermata accanto.
type legameFileMA struct {
	Padre              string // il Rif del padre nel file
	Quantita           *int
	QuantitaConfermata *int
	Decisa             bool
}

// legameConfermatoMA: una relazione confermata corrente verso il componente del nodo (fotorfq.Thread.Relazioni, cioè
// componente_relazione dopo sposta, collega e scollega).
type legameConfermatoMA struct {
	PadreID  uuid.UUID // componente.id del padre: i campi «padre» e «da» dei gesti
	PadreRif string    // il Rif del padre nel gruppo, o in Nodi; «componente:<uuid>» se non c'è (rifNelGruppo)
	Codice   string    // il codice del padre
	Qta      int       // componente_relazione.qta
	NelFile  bool      // il padre confermato è anche un padre del file del nodo
}

// componenteMA: il componente registrato (v1.1 §4.6): Rev e Descrizione come sono nel DB.
type componenteMA struct {
	ID              uuid.UUID
	Codice          string
	Rev             *string // registrata
	Descrizione     *string // registrata; nil = nessuna («Rev e descrizione» resta spento)
	Tipo            string
	Archiviato      bool
	StepStrutturale *uuid.UUID
}

// correnteMA: un documento corrente dello stesso componente e tipo: le risposte di «scelta» (aggiungi o
// sostituisci:<id>) di assegna (v1.1 §4.11).
type correnteMA struct {
	DocumentoID uuid.UUID
	NomeFile    string
	Rev         *string
}

// Le chiavi e i tipi dei gruppi (X-02; T-A1d-09): stringhe opache e stabili, che non dipendono dall'ordine.
const (
	gruppoBomMA   = "bom"
	gruppoCandMA  = "cand"
	gruppoSenzaMA = "senza"
)

// asseStrutturaMA: l'asse di ricerca «struttura» di Conflitti, tutti gli assi tranne lo smistamento (T-A1d-25; piano
// del frontend D-PF-15). Non è un valore di valutazione.AsseConflitto: è un raggruppamento di ricerca (D-A1d-11).
const asseStrutturaMA = "struttura"

// prefissoComponenteMA: «componente:», il prefisso di ancoraggio.RifComponente, preso dalla funzione stessa.
var prefissoComponenteMA = strings.TrimSuffix(ancoraggio.RifComponente(uuid.Nil), uuid.Nil.String())

// ---- i metodi del contratto §2.3 (11) ----

// File: il record del file (e.File con AllegatoID == a); nil se assente.
func (x *indiceMotoreA) File(a uuid.UUID) *valutazione.FileInterpretato {
	if x.vuoto() {
		return nil
	}
	return x.file[a]
}

// NomeFile: il nome dell'allegato, fra tutti gli allegati del thread; "" se assente.
func (x *indiceMotoreA) NomeFile(a uuid.UUID) string {
	if x.vuoto() {
		return ""
	}
	if al := x.allegati[a]; al != nil {
		return al.NomeFile
	}
	return ""
}

// Associazione: l'associazione del file (e.Associazioni con AllegatoID == a); nil se assente.
func (x *indiceMotoreA) Associazione(a uuid.UUID) *valutazione.AssociazioneFile {
	if x.vuoto() {
		return nil
	}
	return x.assoc[a]
}

// Badge: il badge vecchio/nuovo del file (c.File con File.AllegatoID == a); nil se assente o senza confronto.
func (x *indiceMotoreA) Badge(a uuid.UUID) *confronto.EsitoFile {
	if x.vuoto() {
		return nil
	}
	return x.badge[a]
}

// Prodotto: il prodotto valutato con quel Rif; nil se assente.
func (x *indiceMotoreA) Prodotto(rif string) *valutazione.ProdottoValutato {
	if x.vuoto() {
		return nil
	}
	return x.prodotti[rif]
}

// Nodo: il primo nodo con quel Rif (T-A1d-10), in ordine: Nodi, i Nodi di ogni struttura candidata (nell'ordine
// dell'esito), ConfermatiSenzaSTEP. Lo stesso Rif in due gruppi (D6 di P7e): l'albero usa NodoIn.
func (x *indiceMotoreA) Nodo(prodotto, rif string) *valutazione.NodoBOM {
	p := x.Prodotto(prodotto)
	if p == nil {
		return nil
	}
	for i := range p.Nodi {
		if p.Nodi[i].Nodo.Rif == rif {
			return &p.Nodi[i]
		}
	}
	for k := range p.StruttureCandidate {
		for i := range p.StruttureCandidate[k].Nodi {
			if p.StruttureCandidate[k].Nodi[i].Nodo.Rif == rif {
				return &p.StruttureCandidate[k].Nodi[i]
			}
		}
	}
	for i := range p.ConfermatiSenzaSTEP {
		if p.ConfermatiSenzaSTEP[i].Nodo.Rif == rif {
			return &p.ConfermatiSenzaSTEP[i]
		}
	}
	return nil
}

// QuantitaConfermata: la quantità dell'arco confermato fra padre e figlio (contratto §1.6.1), una copia; nil se non
// c'è. Prima l'arco delle strutture del prodotto con la sua Decisione (Ancoraggi.Strutture[].Archi[].Decisione), poi, per
// un confermato senza STEP, la sua Parentela decisa (lì la quantità è quella della relazione confermata).
func (x *indiceMotoreA) QuantitaConfermata(prodotto, padre, figlio string) *int {
	if x.vuoto() {
		return nil
	}
	for s := range x.e.Ancoraggi.Strutture {
		st := &x.e.Ancoraggi.Strutture[s]
		if st.Target != prodotto {
			continue
		}
		for a := range st.Archi {
			arco := &st.Archi[a]
			if arco.Padre == padre && arco.Figlio == figlio && arco.Decisione != nil && arco.Decisione.Quantita != nil {
				return copiaInteroMA(arco.Decisione.Quantita)
			}
		}
	}
	if p := x.Prodotto(prodotto); p != nil {
		for i := range p.ConfermatiSenzaSTEP {
			n := &p.ConfermatiSenzaSTEP[i]
			if n.Nodo.Rif != figlio {
				continue
			}
			for _, par := range n.Parentela {
				if par.Padre == padre && par.Decisa {
					return copiaInteroMA(par.Quantita)
				}
			}
		}
	}
	return nil
}

// PropostaAperta: il documento_proposta.id aperto del file, una copia; nil se non c'è (contratto §3.4 C). La proposta
// aperta di un allegato è una sola (UNIQUE).
func (x *indiceMotoreA) PropostaAperta(a uuid.UUID) *uuid.UUID {
	if x.vuoto() || x.t == nil {
		return nil
	}
	for i := range x.t.Proposte {
		if x.t.Proposte[i].AllegatoID == a && x.t.Proposte[i].Stato == string(db.StatoPropostaAperta) {
			id := x.t.Proposte[i].ID
			return &id
		}
	}
	return nil
}

// AutorizzazioneDi: i campi dell'anteprima dello STEP strutturale per il prodotto e l'allegato (contratto §3.4 A;
// T-A1d-23). Nessuna struttura del prodotto nell'allegato → nil; una → AutorizzazioneDiStruttura; più d'una → il modo
// documento senza Sorgente (l'anteprima elenca le radici e non ne sceglie nessuna, U7), nil senza il documento. Se lo
// STEP si può autorizzare lo dice l'anteprima, non questa ricerca.
func (x *indiceMotoreA) AutorizzazioneDi(prodotto string, a uuid.UUID) *autorizzazioneMA {
	ss := x.struttureNellAllegato(prodotto, a)
	switch len(ss) {
	case 0:
		return nil
	case 1:
		return x.AutorizzazioneDiStruttura(prodotto, a, ss[0].Radice)
	}
	p := x.Prodotto(prodotto)
	if p == nil || p.ComponenteID == nil {
		return nil
	}
	doc := x.documentoDelComponente(*p.ComponenteID, a)
	if doc == nil {
		return nil
	}
	return &autorizzazioneMA{Documento: doc}
}

// RelazioneProposta: la riga aperta di relazione_proposta dell'arco (padre, figlio sono Rif di nodi) fra gli allegati
// delle strutture del prodotto (contratto §3.4 B): il Rif si ricompone con ancoraggio.RifNodo e lo sha256 della
// struttura, mai spezzato. nil se non c'è, o se la riga è in più allegati del prodotto (lo stesso contenuto in due file:
// si decide nel Fascicolo).
func (x *indiceMotoreA) RelazioneProposta(prodotto, padre, figlio string) *relazioneMA {
	if x.vuoto() || x.t == nil {
		return nil
	}
	sha := map[uuid.UUID]string{}
	for s := range x.e.Ancoraggi.Strutture {
		if st := &x.e.Ancoraggi.Strutture[s]; st.Target == prodotto {
			sha[st.AllegatoID] = st.Sha256
		}
	}
	var trovata *relazioneMA
	for i := range x.t.RigheRelazioneProposta {
		r := &x.t.RigheRelazioneProposta[i]
		s, ok := sha[r.AllegatoID]
		if !ok || r.Stato != string(db.StatoPropostaAperta) ||
			ancoraggio.RifNodo(s, r.PadreChiave) != padre || ancoraggio.RifNodo(s, r.FiglioChiave) != figlio {
			continue
		}
		if trovata == nil {
			trovata = &relazioneMA{Allegato: r.AllegatoID, Padre: r.PadreChiave, Figlio: r.FiglioChiave}
		} else if trovata.Allegato != r.AllegatoID {
			return nil
		}
	}
	return trovata
}

// Rimozione: la rimozione aperta di un conflitto (contratto §3.4 B); nil se il conflitto non è una rimozione (tipo
// arco, motivo rimozione, con il documento dello STEP). La riga si trova ricostruendo il Rif del conflitto come lo
// costruisce valutazione (RifArco fra i due RifComponente): la stringa del conflitto non si spezza.
func (x *indiceMotoreA) Rimozione(c valutazione.Conflitto) *rimozioneMA {
	if x.vuoto() || x.t == nil {
		return nil
	}
	if c.Tipo != valutazione.ConflittoArco || c.Motivo != valutazione.MotivoConflittoRimozione ||
		c.EvidenzaProposta.DocumentoID == nil {
		return nil
	}
	step := *c.EvidenzaProposta.DocumentoID
	for _, r := range x.t.RimozioniAperte {
		if r.StepDocumentoID == step &&
			valutazione.RifArco(ancoraggio.RifComponente(r.PadreID), ancoraggio.RifComponente(r.FiglioID)) == c.Rif {
			return &rimozioneMA{Step: r.StepDocumentoID, Padre: r.PadreID, Figlio: r.FiglioID}
		}
	}
	return nil
}

// ---- le ricerche in più (v1.1 §4.3; X-02…X-06, X-09) ----

// Gruppi: le schede dell'albero del prodotto, nessuna preselezionata (nessun campo dice «scelta»; T-A1d-09, T-A1d-27):
// «bom» se il prodotto ha la BOM di lavoro (Nodi), con lo STEP e la radice della sua struttura; una «cand» per ogni
// struttura candidata, nell'ordine dell'esito; «senza» se ci sono confermati senza STEP. In cache per prodotto.
func (x *indiceMotoreA) Gruppi(prodotto string) []gruppoMA {
	p := x.Prodotto(prodotto)
	if p == nil {
		return nil
	}
	if g, ok := x.gruppi[prodotto]; ok {
		return g
	}
	var out []gruppoMA
	if len(p.Nodi) > 0 {
		g := gruppoMA{Chiave: gruppoBomMA, Tipo: gruppoBomMA, Nodi: x.nodiDelGruppo(prodotto, gruppoBomMA)}
		if st := x.bomDi(prodotto); st != nil {
			id := st.AllegatoID
			g.AllegatoID, g.Radice, g.NomeFile = &id, st.Radice, x.NomeFile(st.AllegatoID)
		}
		out = append(out, g)
	}
	for k := range p.StruttureCandidate {
		sc := &p.StruttureCandidate[k]
		id := sc.AllegatoID
		chiave := chiaveCandMA(sc)
		out = append(out, gruppoMA{Chiave: chiave, Tipo: gruppoCandMA, AllegatoID: &id, Radice: sc.Radice,
			NomeFile: x.NomeFile(sc.AllegatoID), Struttura: sc, Nodi: x.nodiDelGruppo(prodotto, chiave)})
	}
	if len(p.ConfermatiSenzaSTEP) > 0 {
		out = append(out, gruppoMA{Chiave: gruppoSenzaMA, Tipo: gruppoSenzaMA, Nodi: x.nodiDelGruppo(prodotto, gruppoSenzaMA)})
	}
	if x.gruppi == nil {
		x.gruppi = map[string][]gruppoMA{}
	}
	x.gruppi[prodotto] = out
	return out
}

// NodoIn: il nodo con quel Rif nel solo gruppo (D6 di P7e: lo stesso Rif in due strutture); nil fuori dal gruppo.
func (x *indiceMotoreA) NodoIn(prodotto, gruppo, rif string) *valutazione.NodoBOM {
	for _, n := range x.nodiDelGruppo(prodotto, gruppo) {
		if n.Nodo.Rif == rif {
			return n
		}
	}
	return nil
}

// Radici: i nodi del gruppo senza un padre nel gruppo, nell'ordine del gruppo: un nodo è radice se nessuna sua
// Parentela ha per Padre il Rif di un nodo dello stesso gruppo.
func (x *indiceMotoreA) Radici(prodotto, gruppo string) []*valutazione.NodoBOM {
	nn := x.nodiDelGruppo(prodotto, gruppo)
	nel := make(map[string]bool, len(nn))
	for _, n := range nn {
		nel[n.Nodo.Rif] = true
	}
	var out []*valutazione.NodoBOM
	for _, n := range nn {
		if !unPadreNelGruppoMA(n, nel) {
			out = append(out, n)
		}
	}
	return out
}

// Figli: i figli di rif nel gruppo, nell'ordine del gruppo: ogni nodo con una Parentela verso rif, una voce per arco.
// Un figlio con due padri compare sotto tutti e due (contratto §1.6.1). Quantita è quella dei fatti (Parentela); per un
// confermato senza STEP è nil, perché lì la Parentela porta la quantità confermata (T-A1d-20), che sta in
// QuantitaConfermata come per ogni altro arco.
func (x *indiceMotoreA) Figli(prodotto, gruppo, rif string) []figlioMA {
	var out []figlioMA
	for _, n := range x.nodiDelGruppo(prodotto, gruppo) {
		for _, par := range n.Parentela {
			if par.Padre != rif {
				continue
			}
			f := figlioMA{Nodo: n, QuantitaConfermata: x.QuantitaConfermata(prodotto, rif, n.Nodo.Rif), Decisa: par.Decisa}
			if !confermatoSenzaSTEPMA(n) {
				f.Quantita = copiaInteroMA(par.Quantita)
			}
			out = append(out, f)
		}
	}
	return out
}

// Legami: «nel file sotto X» e «confermato sotto Y» del nodo nel gruppo, sempre accanto (X-05; D5 di P7e).
//   - DelFile: una voce per Parentela, con la quantità dei fatti e quella confermata; vuoto per un confermato senza STEP,
//     che non ha un file (T-A1d-20).
//   - Confermati: le relazioni confermate correnti verso il componente del nodo (componenteDelNodoMA), nell'ordine della
//     fotografia, con il Rif del padre nel gruppo (rifNelGruppo) e NelFile se quel Rif è anche un padre del file.
func (x *indiceMotoreA) Legami(prodotto, gruppo, rif string) legamiMA {
	var out legamiMA
	n := x.NodoIn(prodotto, gruppo, rif)
	if n == nil {
		return out
	}
	if !confermatoSenzaSTEPMA(n) {
		for _, par := range n.Parentela {
			out.DelFile = append(out.DelFile, legameFileMA{Padre: par.Padre, Quantita: copiaInteroMA(par.Quantita),
				QuantitaConfermata: x.QuantitaConfermata(prodotto, par.Padre, rif), Decisa: par.Decisa})
		}
	}
	p := x.Prodotto(prodotto)
	k := x.componenteDelNodoMA(p, gruppo, n)
	if k == nil || x.t == nil {
		return out
	}
	for _, r := range x.t.Relazioni {
		if r.FiglioID != *k {
			continue
		}
		c := legameConfermatoMA{PadreID: r.PadreID, PadreRif: x.rifNelGruppo(p, gruppo, r.PadreID), Qta: r.Qta}
		if comp := x.componenti[r.PadreID]; comp != nil {
			c.Codice = comp.Codice
		}
		for _, d := range out.DelFile {
			if d.Padre == c.PadreRif {
				c.NelFile = true
				break
			}
		}
		out.Confermati = append(out.Confermati, c)
	}
	return out
}

// Componente: il componente registrato (v1.1 §4.6). v è un uuid.UUID, un *uuid.UUID (nil → nil) o un Rif
// «componente:<uuid>», che si accetta solo se è esattamente ancoraggio.RifComponente di quell'UUID; altro → nil.
func (x *indiceMotoreA) Componente(v any) *componenteMA {
	if x.vuoto() {
		return nil
	}
	var id uuid.UUID
	switch w := v.(type) {
	case uuid.UUID:
		id = w
	case *uuid.UUID:
		if w == nil {
			return nil
		}
		id = *w
	case string:
		resto, ok := strings.CutPrefix(w, prefissoComponenteMA)
		if !ok {
			return nil
		}
		u, err := uuid.Parse(resto)
		if err != nil || ancoraggio.RifComponente(u) != w {
			return nil
		}
		id = u
	default:
		return nil
	}
	c := x.componenti[id]
	if c == nil {
		return nil
	}
	return &componenteMA{ID: c.ID, Codice: c.Codice, Rev: copiaTestoMA(c.Rev), Descrizione: copiaTestoMA(c.Descrizione),
		Tipo: c.Tipo, Archiviato: c.ArchiviatoIl != nil, StepStrutturale: maCopiaUUID(c.StepStrutturaleID)}
}

// Correnti: i documenti correnti (SostituitoDa nil) del componente con quel tipo, nell'ordine della fotografia
// (T-A1d-26): le risposte possibili di «scelta» in assegna.
func (x *indiceMotoreA) Correnti(componente uuid.UUID, tipo string) []correnteMA {
	if x.vuoto() || x.t == nil {
		return nil
	}
	var out []correnteMA
	for i := range x.t.Documenti {
		d := &x.t.Documenti[i]
		if d.ComponenteID != nil && *d.ComponenteID == componente && d.Tipo == tipo && d.SostituitoDa == nil {
			out = append(out, correnteMA{DocumentoID: d.ID, NomeFile: d.NomeFile, Rev: copiaTestoMA(d.Rev)})
		}
	}
	return out
}

// Anteprima: il riferimento per l'anteprima di un allegato, nella forma di Disegno2D.Anteprima (X-09), una copia.
// Prima il primo 2D dell'esito con quell'allegato; altrimenti dall'allegato stesso: nil senza sha256, il formato
// dall'estensione dichiarata o da quella del nome (come valutazione), Disponibile solo per il PDF (T-A1d-19, LD-03).
// Se il PDF si apre davvero lo dice la GET dell'anteprima (404/409/415, D-A1d-09).
func (x *indiceMotoreA) Anteprima(a uuid.UUID) *valutazione.RiferimentoAnteprima {
	if x.vuoto() {
		return nil
	}
	if r := x.anteprime[a]; r != nil {
		c := copiaAnteprimaMA(*r)
		return &c
	}
	al := x.allegati[a]
	if al == nil || al.Sha256 == nil || *al.Sha256 == "" {
		return nil
	}
	id := a
	f := valutazione.FormatoDi(estensioneMA(al))
	return &valutazione.RiferimentoAnteprima{Sha256: *al.Sha256, Formato: f, AllegatoID: &id,
		Disponibile: f == valutazione.Formato2DPDF}
}

// Conflitti: i conflitti del prodotto (T-A1d-25): asse "" tutti; "struttura" tutti tranne lo smistamento; uno dei tre
// assi di valutazione.AsseConflitto quell'asse; ogni altro valore nessuno.
func (x *indiceMotoreA) Conflitti(prodotto, asse string) []valutazione.Conflitto {
	if x.vuoto() {
		return nil
	}
	switch valutazione.AsseConflitto(asse) {
	case "", asseStrutturaMA, valutazione.AsseNomenclatura, valutazione.AsseGerarchia, valutazione.AsseSmistamento:
	default:
		return nil
	}
	var out []valutazione.Conflitto
	for _, c := range x.e.Conflitti {
		if c.Prodotto != prodotto {
			continue
		}
		if asse == "" || (asse == asseStrutturaMA && c.Asse != valutazione.AsseSmistamento) || string(c.Asse) == asse {
			out = append(out, c)
		}
	}
	return out
}

// ConflittiDelThread: i conflitti senza prodotto (Prodotto "").
func (x *indiceMotoreA) ConflittiDelThread() []valutazione.Conflitto {
	if x.vuoto() {
		return nil
	}
	var out []valutazione.Conflitto
	for _, c := range x.e.Conflitti {
		if c.Prodotto == "" {
			out = append(out, c)
		}
	}
	return out
}

// Voce: la voce certa del fabbisogno del prodotto per (componente, tipo di documento); nil se non c'è.
func (x *indiceMotoreA) Voce(prodotto string, componente uuid.UUID, tipo string) *valutazione.VoceFabbisogno {
	p := x.Prodotto(prodotto)
	if p == nil {
		return nil
	}
	for i := range p.Documenti.Voci {
		if v := &p.Documenti.Voci[i]; v.ComponenteID == componente && v.TipoDocumento == tipo {
			return v
		}
	}
	return nil
}

// CopieAperte: le voci del prodotto con la copia sul NAS non completata (StatoNas in coda o in errore), in ordine
// (T-A1d-26).
func (x *indiceMotoreA) CopieAperte(prodotto string) []*valutazione.VoceFabbisogno {
	p := x.Prodotto(prodotto)
	if p == nil {
		return nil
	}
	var out []*valutazione.VoceFabbisogno
	for i := range p.Documenti.Voci {
		if v := &p.Documenti.Voci[i]; v.StatoNas == string(db.StatoNasInCoda) || v.StatoNas == string(db.StatoNasErrore) {
			out = append(out, v)
		}
	}
	return out
}

// CandidatiFuori: i candidati della fonte che non sono una struttura candidata (R-FE-11; T-A1d-24), copie: un candidato
// è «dentro» se ha l'allegato o lo sha256 di una struttura candidata del prodotto.
func (x *indiceMotoreA) CandidatiFuori(prodotto string) []ancoraggio.DocumentoCandidato {
	p := x.Prodotto(prodotto)
	if p == nil {
		return nil
	}
	var out []ancoraggio.DocumentoCandidato
	for i := range p.Fonte.Candidati {
		c := &p.Fonte.Candidati[i]
		dentro := false
		for k := range p.StruttureCandidate {
			sc := &p.StruttureCandidate[k]
			if (c.AllegatoID != nil && *c.AllegatoID == sc.AllegatoID) || (c.Sha256 != "" && c.Sha256 == sc.Sha256) {
				dentro = true
				break
			}
		}
		if !dentro {
			out = append(out, copiaCandidatoMA(*c))
		}
	}
	return out
}

// AutorizzazioneDiStruttura: i campi dell'anteprima dello STEP strutturale per una struttura del prodotto (l'allegato e
// la radice; contratto §3.4 A; T-A1d-23):
//  1. il prodotto con il suo componente (la rotta vuole {cid}), altrimenti nil;
//  2. la struttura del prodotto in quell'allegato con quella radice, altrimenti nil;
//  3. la chiave della radice: quella della riga di componente_proposta dell'allegato il cui RifNodo è la radice;
//     senza la riga, il resto della radice dopo ancoraggio.RifNodo(sha256, ""), l'inverso esatto di RifNodo con lo
//     sha256 noto; vuota → nil;
//  4. radice del file → modo documento: il primo documento corrente del componente del prodotto che ha quell'allegato
//     fra i suoi (lo STEP si autorizza con un documento confermato: P-14, D-A1d-08), con la chiave come Sorgente; senza
//     il documento nil;
//  5. radice interna → modo delega: Nodo «<allegato_id>:<chiave>», senza Sorgente.
func (x *indiceMotoreA) AutorizzazioneDiStruttura(prodotto string, a uuid.UUID, radice string) *autorizzazioneMA {
	if x.vuoto() || x.t == nil {
		return nil
	}
	p := x.Prodotto(prodotto)
	if p == nil || p.ComponenteID == nil {
		return nil
	}
	var st *ancoraggio.StrutturaProdotto
	for _, s := range x.struttureNellAllegato(prodotto, a) {
		if s.Radice == radice {
			st = s
			break
		}
	}
	if st == nil {
		return nil
	}
	chiave, riga := "", false
	for i := range x.t.RigheComponenteProposta {
		r := &x.t.RigheComponenteProposta[i]
		if r.AllegatoID == a && ancoraggio.RifNodo(r.Sha256, r.Chiave) == radice {
			chiave, riga = r.Chiave, true
			break
		}
	}
	if !riga {
		if resto, ok := strings.CutPrefix(radice, ancoraggio.RifNodo(st.Sha256, "")); ok {
			chiave = resto
		}
	}
	if chiave == "" {
		return nil
	}
	if !st.RadiceDelFile {
		return &autorizzazioneMA{Nodo: a.String() + ":" + chiave}
	}
	doc := x.documentoDelComponente(*p.ComponenteID, a)
	if doc == nil {
		return nil
	}
	return &autorizzazioneMA{Documento: doc, Sorgente: chiave}
}

// ---- i nodi di un gruppo, il componente di un nodo e il Rif di un padre (T-A1d-21, T-A1d-22) ----

// nodiDelGruppo: i nodi di un gruppo, ognuno una volta (T-A1d-21; X-03), in cache per prodotto e gruppo:
//   - bom: i nodi di Nodi, in ordine; poi, ripetendo sui confermati senza STEP in ordine finché se ne aggiunge uno,
//     quelli con un padre (Parentela.Padre) già nel gruppo: compaiono come figli del loro padre, e la chiusura porta
//     anche i figli dei figli;
//   - cand:…: i nodi della struttura candidata la cui chiave ricomposta è il gruppo (la chiave non si spezza);
//   - senza: tutti i confermati senza STEP;
//   - altro: nil.
func (x *indiceMotoreA) nodiDelGruppo(prodotto, gruppo string) []*valutazione.NodoBOM {
	p := x.Prodotto(prodotto)
	if p == nil {
		return nil
	}
	chiave := prodotto + "\x00" + gruppo
	if n, ok := x.nodi[chiave]; ok {
		return n
	}
	var out []*valutazione.NodoBOM
	switch gruppo {
	case gruppoBomMA:
		nel := map[string]bool{}
		for i := range p.Nodi {
			out = append(out, &p.Nodi[i])
			nel[p.Nodi[i].Nodo.Rif] = true
		}
		aggiunto := make([]bool, len(p.ConfermatiSenzaSTEP))
		for altri := true; altri; {
			altri = false
			for i := range p.ConfermatiSenzaSTEP {
				if n := &p.ConfermatiSenzaSTEP[i]; !aggiunto[i] && unPadreNelGruppoMA(n, nel) {
					out = append(out, n)
					nel[n.Nodo.Rif], aggiunto[i], altri = true, true, true
				}
			}
		}
	case gruppoSenzaMA:
		for i := range p.ConfermatiSenzaSTEP {
			out = append(out, &p.ConfermatiSenzaSTEP[i])
		}
	default:
		for k := range p.StruttureCandidate {
			if sc := &p.StruttureCandidate[k]; chiaveCandMA(sc) == gruppo {
				for i := range sc.Nodi {
					out = append(out, &sc.Nodi[i])
				}
				break
			}
		}
	}
	if x.nodi == nil {
		x.nodi = map[string][]*valutazione.NodoBOM{}
	}
	x.nodi[chiave] = out
	return out
}

// componenteDelNodoMA: il componente di un nodo nel gruppo (T-A1d-22): la sua Decisione; altrimenti, solo nel gruppo bom
// e solo per la radice della BOM di lavoro, il componente del target (contratto §1.3, §1.4; T-B4-38: la radice della BOM
// di lavoro prende il componente del target, quella di una candidata no; la stessa regola di componenteDelNodo di
// valutazione, che non è esportata); altrimenti nil. Una copia.
func (x *indiceMotoreA) componenteDelNodoMA(p *valutazione.ProdottoValutato, gruppo string,
	n *valutazione.NodoBOM) *uuid.UUID {
	switch {
	case p == nil || n == nil:
		return nil
	case n.Nodo.Decisione != nil:
		id := n.Nodo.Decisione.ComponenteID
		return &id
	case gruppo == gruppoBomMA && p.ComponenteID != nil:
		if st := x.bomDi(p.Rif); st != nil && n.Nodo.Rif == st.Radice {
			return maCopiaUUID(p.ComponenteID)
		}
	}
	return nil
}

// rifNelGruppo: il Rif con cui un componente padre compare nella pagina (T-A1d-22), in quest'ordine: il primo nodo del
// gruppo che lo rappresenta; il primo nodo di Nodi che lo rappresenta; ancoraggio.RifComponente. Gli ultimi due sono la
// regola con cui valutazione scrive il Padre della Parentela di un confermato senza STEP (P7e): così, nella scheda
// «senza», PadreRif coincide con il Padre che Figli e Radici leggono.
func (x *indiceMotoreA) rifNelGruppo(p *valutazione.ProdottoValutato, gruppo string, id uuid.UUID) string {
	if p == nil {
		return ancoraggio.RifComponente(id)
	}
	for _, n := range x.nodiDelGruppo(p.Rif, gruppo) {
		if k := x.componenteDelNodoMA(p, gruppo, n); k != nil && *k == id {
			return n.Nodo.Rif
		}
	}
	for i := range p.Nodi {
		if k := x.componenteDelNodoMA(p, gruppoBomMA, &p.Nodi[i]); k != nil && *k == id {
			return p.Nodi[i].Nodo.Rif
		}
	}
	return ancoraggio.RifComponente(id)
}

// bomDi: la prima struttura del prodotto che è la BOM di lavoro (al più una per prodotto, R76 A); nil senza.
func (x *indiceMotoreA) bomDi(prodotto string) *ancoraggio.StrutturaProdotto {
	if x.vuoto() {
		return nil
	}
	for s := range x.e.Ancoraggi.Strutture {
		if st := &x.e.Ancoraggi.Strutture[s]; st.Target == prodotto && st.Stato == ancoraggio.StatoBOMDiLavoroProposta {
			return st
		}
	}
	return nil
}

// struttureNellAllegato: le strutture del prodotto in quell'allegato (BOM di lavoro o candidate), nell'ordine di
// ancoraggio (T-A1d-23).
func (x *indiceMotoreA) struttureNellAllegato(prodotto string, a uuid.UUID) []*ancoraggio.StrutturaProdotto {
	if x.vuoto() {
		return nil
	}
	var out []*ancoraggio.StrutturaProdotto
	for s := range x.e.Ancoraggi.Strutture {
		if st := &x.e.Ancoraggi.Strutture[s]; st.Target == prodotto && st.AllegatoID == a {
			out = append(out, st)
		}
	}
	return out
}

// documentoDelComponente: l'ID del primo documento corrente del componente che ha l'allegato fra i suoi
// (documento_provenienza); nil senza. È il documento confermato che l'autorizzazione dello STEP vuole
// (fascicolo.fileProprio).
func (x *indiceMotoreA) documentoDelComponente(componente, a uuid.UUID) *uuid.UUID {
	if x.vuoto() || x.t == nil {
		return nil
	}
	for i := range x.t.Documenti {
		d := &x.t.Documenti[i]
		if d.SostituitoDa != nil || d.ComponenteID == nil || *d.ComponenteID != componente {
			continue
		}
		for _, al := range d.Allegati {
			if al == a {
				id := d.ID
				return &id
			}
		}
	}
	return nil
}

// chiaveCandMA: la chiave del gruppo di una struttura candidata, «cand:<allegato_id>:<radice>» (X-02).
func chiaveCandMA(sc *valutazione.StrutturaCandidata) string {
	return gruppoCandMA + ":" + sc.AllegatoID.String() + ":" + sc.Radice
}

// unPadreNelGruppoMA: il nodo ha almeno un padre (Parentela.Padre) fra i Rif dati.
func unPadreNelGruppoMA(n *valutazione.NodoBOM, nel map[string]bool) bool {
	for _, par := range n.Parentela {
		if nel[par.Padre] {
			return true
		}
	}
	return false
}

// confermatoSenzaSTEPMA: il nodo è un componente confermato senza STEP (EB7-1; contratto §1.4: il Rif è
// «componente:<uuid>», e non c'è un file). Non si guarda SenzaFile: anche un nodo dello STEP senza un file ancorato ha
// SenzaFile (ancoraggio, ricalcolaSenzaFile), e la sua Parentela porta la quantità dei fatti (contratto §1.6.1).
func confermatoSenzaSTEPMA(n *valutazione.NodoBOM) bool {
	return strings.HasPrefix(n.Nodo.Rif, prefissoComponenteMA)
}

// estensioneMA: l'estensione dichiarata dell'allegato, o quella del nome, senza il punto e senza maiuscole (la stessa
// lettura di valutazione per la fonte, che non la esporta).
func estensioneMA(a *fotorfq.Allegato) string {
	if a.Estensione != nil && *a.Estensione != "" {
		return strings.ToLower(strings.TrimPrefix(*a.Estensione, "."))
	}
	return strings.ToLower(strings.TrimPrefix(path.Ext(a.NomeFile), "."))
}

func copiaInteroMA(v *int) *int {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

func copiaTestoMA(v *string) *string {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

func copiaAnteprimaMA(r valutazione.RiferimentoAnteprima) valutazione.RiferimentoAnteprima {
	r.DocumentoID = maCopiaUUID(r.DocumentoID)
	r.AllegatoID = maCopiaUUID(r.AllegatoID)
	return r
}

func copiaCandidatoMA(c ancoraggio.DocumentoCandidato) ancoraggio.DocumentoCandidato {
	c.AllegatoID = maCopiaUUID(c.AllegatoID)
	c.DocumentoID = maCopiaUUID(c.DocumentoID)
	c.Radici = maCopiaStringhe(c.Radici)
	return c
}
