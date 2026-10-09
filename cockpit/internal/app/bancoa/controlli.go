package bancoa

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/estrazione/evidenze"
	"promatec/cockpit/internal/core/fotorfq"
	"promatec/cockpit/internal/core/inbox/classificazione/motorea"
	"promatec/cockpit/internal/core/registro/regole/grammatica"
	valut "promatec/cockpit/internal/core/valutazione"
	"promatec/cockpit/internal/platform/dataset"
	"promatec/cockpit/internal/platform/migrazioni"
)

// controlli.go: i controlli del runner (piano 6.4.9, «Controlli del runner», n.1–5; 6.7.2: A1c-L1P-01 e -02 sono i
// n.5, n.1 e n.4 nelle corse del banco). Girano prima degli esiti. Un controllo fallito è una differenza (uscita 1),
// mai un «mancante»; un controllo che non si può fare perché un ingresso manca è NON ESEGUITO (uscita 3). Ogni
// funzione è pura e restituisce l'esito del controllo, che il runner scrive nel rapporto.

// Le voci dei controlli del runner nel rapporto, con i nomi del piano.
const (
	ControlloScenario      = "runner_1_scenario_e_casi"
	ControlloRisoluzione   = "runner_2_id_e_sha_degli_attesi"
	ControlloFonti         = "runner_3_export_e_fonti"
	ControlloCasiIndice    = "runner_4_casi_e_indice"
	ControlloAttesiInteri  = "runner_5_attesi_interi"
	ControlloTraduzione    = "traduzione_degli_attesi"
	ControlloInvariantiC5  = "invarianti_da_rivedere"
	ControlloBaselineRighe = "baseline_righe_e_basi"
	// ControlloLetture: le letture attese dei file della baseline (oltre a base, che è il controllo (2)) e dei file reali,
	// contro l'interpretazione del file; le chiavi booleane delle voci che nessun altro controllo verifica (R-101; la
	// regola delle chiavi accettate).
	ControlloLetture = "letture_e_invarianti_dei_file"
	// ControlloCasiFoto: i thread e i messaggi del file dei casi contro la fotografia (T-B6-74, T-B6-112; R-103).
	ControlloCasiFoto = "casi_e_fotografia"
	// ControlloClientiExport: i clienti dei thread esportati con la ragione sociale negli export (T-B6-104).
	ControlloClientiExport = "clienti_degli_export"
	// ControlloCopia, ControlloSentinelle: la copia del manifest con -dsn (6.4.9, passo 3; R-106; T-B6-105; le
	// sentinelle delegate ad A1c-L4D-01: R117 b).
	ControlloCopia      = "copia"
	ControlloSentinelle = "sentinelle"
)

// La regola delle chiavi accettate (T-B6-102, precisata dall'orchestratore dopo la revisione di P9): una chiave degli
// attesi che il runner accetta è verificata, oppure è dichiarata non verificata, fra i nonFatti del controllo che la
// riguarda, con il percorso e senza valori. Mai accettata e buttata in silenzio. Le chiavi sconosciute restano
// differenze al n.5; le chiavi delle sezioni a forma libera che non sono ID vanno in un elenco informativo (R-109).

// esitoControllo: come è andato un controllo, prima di diventare un Controllo del rapporto.
type esitoControllo struct {
	nome       string
	eseguito   bool
	differenze []string // ognuna conta 1
	nonFatti   []string // le parti che non si sono potute fare (rendono il controllo NON ESEGUITO se non ci sono differenze)
	// nonApplicabili: le parti che in questa corsa non si applicano, perché il piano le assegna all'altra modalità (la
	// riga della C5 dell'altra modalità: R-117). Solo informazione: non cambiano lo stato del controllo.
	nonApplicabili []string
	// ambitiDedotti: le chiavi giudicate su un ambito dedotto dalle chiavi sorelle della voce, con l'unità (le chiavi
	// dei token: T-B6-220, precisata dall'orchestratore con R-140). Solo informazione: non cambiano lo stato del
	// controllo, ma il rapporto dice su quale unità la chiave è stata giudicata.
	ambitiDedotti []string
	// delegato: il controllo lo verifica una prova fuori dal runner, nominata nel motivo (R117 b: le sentinelle). Il
	// controllo non è eseguito dal runner, ma non è NON ESEGUITO: è «delegato», e non decide l'uscita.
	delegato bool
	// fuoriPerimetro: le parti che una decisione dell'utente ha messo fuori dal perimetro approvato, con il percorso (D-R116
	// F, decisa dall'utente l'08/10: con gli export, il n.1 senza i finiti). Un'esclusione specifica e motivata, ratificata
	// come la delega delle sentinelle: non rende il controllo NON ESEGUITO, e la tabella delle classi ha la sua riga, con
	// la motivazione e la fonte (classi.go). Mai un'esenzione generica.
	fuoriPerimetro []string
	motivo         string
}

// contestoCorsa: ciò che i controlli devono sapere della corsa per non dare per verificato ciò che non hanno visto.
//   - parziale, scelti: la fotografia ha solo i thread scelti (-thread);
//   - export: la fotografia viene dagli export (le righe decise si confrontano con quelle dell'export, senza deciso_da);
//   - componentiAssenti: la sezione dei componenti è assente (gli export): i finiti non si vedono (R70 A);
//   - nonValutabili: i thread che non si possono valutare per un limite degli ingressi, con il motivo (T-B6-104: il
//     cliente senza la ragione sociale negli export). Ciò che dipende dalla loro valutazione è una parte non
//     verificabile, mai una differenza.
type contestoCorsa struct {
	parziale          bool
	scelti            map[uuid.UUID]bool
	export            bool
	componentiAssenti bool
	nonValutabili     map[uuid.UUID]string
}

// controllo: l'esito come voce del rapporto. Le differenze prevalgono su ciò che non si è potuto fare (R44). Un
// controllo delegato, senza differenze né parti non fatte, è «delegato» (R117 b). Le parti fuori perimetro (D-R116 F) e
// quelle non applicabili non cambiano lo stato; le parti fuori perimetro si contano nel motivo, perché un controllo
// passato con una parte esclusa non si legga come passato per intero (i dettagli le elencano una per una; T-B6c-11).
func (e esitoControllo) controllo() Controllo {
	c := Controllo{Nome: e.nome, Stato: ControlloEseguito, Differenze: len(e.differenze), Motivo: e.motivo}
	switch {
	case !e.eseguito && e.delegato && len(e.differenze) == 0 && len(e.nonFatti) == 0:
		c.Stato = ControlloDelegato
	case !e.eseguito:
		c.Stato = ControlloNonEseguito
	case len(e.differenze) == 0 && len(e.nonFatti) > 0:
		c.Stato = ControlloNonEseguito
		c.Motivo = unisciMotivi(c.Motivo, fmt.Sprintf("%d parti non verificabili", len(e.nonFatti)))
	}
	if len(e.fuoriPerimetro) > 0 {
		c.Motivo = unisciMotivi(c.Motivo, fmt.Sprintf("%d parti fuori perimetro, nei dettagli", len(e.fuoriPerimetro)))
	}
	return c
}

func unisciMotivi(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "; " + b
}

// controlloScenario (n.1; T-B0-15; R29 a; R48 A): gli ingressi del caso dello scenario nel file dei casi coincidono
// con la sezione dello scenario degli attesi:
//   - c'è un caso per il thread dello scenario, con un segmento del messaggio dello scenario (il segmento lo controlla
//     ValidaUso dentro valutazione: un segmento che non esiste nel documento rende il thread non valutato, con
//     errore_valutazione, e il predicato qui sotto non si verifica);
//   - l'autorità del caso è quella degli attesi, tradotta (scenario_richiesta_inoltrata → scenario);
//   - prodotti_confermati_db dice se il thread della fotografia ha un target confermato, con il predicato di R70 A
//     (valutazione.TargetConfermato) sui prodotti valutati del thread.
//
// Senza la sezione dello scenario non c'è niente da confrontare (eseguito, senza differenze); senza il file dei casi
// letto il controllo è NON ESEGUITO. Il predicato non si verifica, ed è una parte non verificabile (mai una differenza),
// se il thread dello scenario non è fra quelli scelti (R-104) o se non si valuta per un limite degli ingressi
// (T-B6-104). Se la sezione dei componenti è assente (gli export) e il thread non ha un target confermato dagli
// identificativi, il predicato non si decide (R-105: i finiti di origine manuale non si vedono; un target confermato
// trovato vale anche senza i componenti): quella parte è fuori dal perimetro approvato, con la sua riga nella tabella
// delle classi (D-R116 F, decisa dall'utente l'08/10, coerente con D-R115 A: il campione di export è incompleto e non si
// completa). È un'esclusione specifica e motivata, ratificata: non rende il controllo NON ESEGUITO e vale solo con gli
// export; con -dsn i componenti ci sono e niente cambia.
func controlloScenario(s *ScenarioAtteso, in valut.Ingressi, casiLetti bool, esito *valut.Esito, cc contestoCorsa) esitoControllo {
	e := esitoControllo{nome: ControlloScenario, eseguito: true}
	switch {
	case s == nil:
		e.motivo = "nessuna sezione dello scenario negli attesi"
		return e
	case !casiLetti:
		e.eseguito, e.motivo = false, "il file dei casi non è stato letto"
		return e
	case s.ThreadID == nil:
		e.differenze = append(e.differenze, "lo scenario degli attesi non dice il thread")
		return e
	}
	caso := in.CasoDelThread(*s.ThreadID)
	if caso == nil {
		e.differenze = append(e.differenze, "il file dei casi non ha un caso per il thread dello scenario")
		return e
	}
	if s.MessaggioID == nil {
		e.differenze = append(e.differenze, "lo scenario degli attesi non dice il messaggio")
	} else {
		trovato := false
		for _, sg := range caso.Segmenti {
			if sg.MessaggioID == *s.MessaggioID {
				trovato = true
			}
		}
		if !trovato {
			e.differenze = append(e.differenze, "il caso dello scenario non ha un segmento del messaggio degli attesi")
		}
	}
	if a, ok := traduciAutorita(s.AutoritaTarget); !ok {
		e.differenze = append(e.differenze, fmt.Sprintf("autorita_target %q non si traduce", s.AutoritaTarget))
	} else if a != string(caso.Autorita) {
		e.differenze = append(e.differenze, fmt.Sprintf("l'autorità del caso è %q, gli attesi dicono %q", string(caso.Autorita), a))
	}
	if s.ProdottiConfermatiDB == nil {
		e.differenze = append(e.differenze, "lo scenario degli attesi non dice prodotti_confermati_db")
		return e
	}
	if esito == nil {
		e.differenze = append(e.differenze, "la valutazione non c'è: il predicato dei target confermati non si verifica")
		return e
	}
	var et *valut.EsitoThread
	for i := range esito.Thread {
		if esito.Thread[i].ThreadID == *s.ThreadID {
			et = &esito.Thread[i]
		}
	}
	switch {
	case et == nil && cc.parziale:
		e.nonFatti = append(e.nonFatti, "scenario_inoltro.prodotti_confermati_db: il thread dello scenario non è fra quelli scelti")
	case et == nil:
		e.differenze = append(e.differenze, "il thread dello scenario non è nella fotografia")
	case cc.nonValutabili[*s.ThreadID] != "":
		e.nonFatti = append(e.nonFatti, "scenario_inoltro.prodotti_confermati_db: "+cc.nonValutabili[*s.ThreadID])
	case et.Motivo == valut.MotivoThreadErroreValutazione:
		e.differenze = append(e.differenze, "il thread dello scenario ha un errore della valutazione: il predicato dei target confermati non si verifica")
	default:
		d, nf := predicatoConfermati(*et, *s.ProdottiConfermatiDB, cc.componentiAssenti, "scenario_inoltro.prodotti_confermati_db")
		switch {
		case d != "":
			e.differenze = append(e.differenze, d)
		case nf != "" && cc.export:
			e.fuoriPerimetro = append(e.fuoriPerimetro, nf+" (D-R116 F)")
		case nf != "":
			e.nonFatti = append(e.nonFatti, nf)
		}
	}
	return e
}

// predicatoConfermati: «il thread ha un target confermato» (R70 A, valutazione.TargetConfermato sui prodotti valutati)
// contro il valore atteso. Restituisce la differenza, oppure la parte non verificabile: senza la sezione dei componenti
// un thread senza target confermati dagli identificativi può averne uno da un finito, quindi il predicato non si decide.
func predicatoConfermati(et valut.EsitoThread, atteso, componentiAssenti bool, percorso string) (differenza, nonFatto string) {
	confermati := 0
	for _, pv := range et.ProdottiValutati {
		if valut.TargetConfermato(pv) {
			confermati++
		}
	}
	switch {
	case confermati > 0 && atteso:
	case confermati > 0:
		return fmt.Sprintf("%s falso, ma il thread ha %d target confermati (R70 A)", percorso, confermati), ""
	case componentiAssenti:
		return "", percorso + ": i componenti non sono nella fotografia, e senza i finiti il predicato di R70 A non si decide"
	case atteso:
		return percorso + " vero, ma il thread non ha target confermati (R70 A)", ""
	}
	return "", ""
}

// controlloRisoluzione (n.2; ATT «fixture_reali»; D7): ogni ID e ogni sha256 degli attesi si risolve nella fotografia.
//   - Le voci di file non risolte e i file in due sezioni (che il gate non conta due volte) vengono dalla traduzione.
//   - Il thread e il messaggio dello scenario.
//   - Gli ID e gli sha256 della conferma dell'albero e dei casi d'integrazione, per chiave: allegato, thread, messaggio,
//     componente (anche padre e figlio), documento, sha256. Un componente non si verifica se la sezione dei
//     componenti è assente (gli export).
//   - Da B6c: il thread e il cliente della sezione C5 (sezioneDaRivedere, T-B6c-08), e il conteggio hash_distinti
//     dello scenario, che si conta sulla fotografia (hashDistinti, T-B6c-09). Il thread_id delle voci viene dalla
//     traduzione, come nome_file.
//
// parziale: la fotografia ha solo i thread scelti (-thread): un allegato o un thread che non c'è può essere di un altro
// thread, quindi non è una differenza ma una parte non verificabile.
func controlloRisoluzione(s SezioniAttesi, tr traduzione, ix indiceFoto, f fotorfq.Fotografia, parziale bool) esitoControllo {
	e := esitoControllo{nome: ControlloRisoluzione, eseguito: true}
	for _, nr := range tr.nonRisolte {
		if parziale && (strings.HasSuffix(nr, "l'allegato non è nella fotografia") || strings.Contains(nr, "nessun allegato della fotografia")) {
			e.nonFatti = append(e.nonFatti, nr)
			continue
		}
		e.differenze = append(e.differenze, nr)
	}
	e.differenze = append(e.differenze, tr.doppie...)
	e.differenze = append(e.differenze, tr.nomi...)
	if sc := s.Scenario; sc != nil && sc.ThreadID != nil {
		if _, ok := ix.thread[*sc.ThreadID]; !ok {
			if parziale {
				e.nonFatti = append(e.nonFatti, "il thread dello scenario non è fra quelli scelti")
			} else {
				e.differenze = append(e.differenze, "il thread dello scenario non è nella fotografia")
			}
		} else if sc.MessaggioID != nil && ix.messaggi[*sc.MessaggioID] != *sc.ThreadID {
			e.differenze = append(e.differenze, "il messaggio dello scenario non è un messaggio del suo thread")
		}
	}
	if sc := s.Scenario; sc != nil {
		for _, c := range sc.Conteggi {
			if c.Nome == conteggioHashDistinti {
				d, nf := hashDistinti(tr, ix, c.Valore, parziale)
				e.differenze, e.nonFatti = aggiungiNonVuota(e.differenze, d), aggiungiNonVuota(e.nonFatti, nf)
			}
		}
	}
	d, nf := sezioneDaRivedere(s, ix, f, parziale)
	e.differenze, e.nonFatti = append(e.differenze, d...), append(e.nonFatti, nf...)
	componentiAssenti := f.Sezioni[fotorfq.SezioneComponenti].Stato == fotorfq.StatoSezioneAssente
	for _, id := range append(append([]IDAtteso(nil), s.Albero...), s.Integrazione...) {
		if id.Chiave == "sha256" {
			switch {
			case ix.sha[strings.ToLower(id.Valore)]:
			case parziale:
				e.nonFatti = append(e.nonFatti, id.Percorso+": non è fra i contenuti dei thread scelti")
			default:
				e.differenze = append(e.differenze, id.Percorso+": nessun contenuto della fotografia ha questo sha256")
			}
			continue
		}
		u, err := uuid.Parse(id.Valore)
		if err != nil {
			e.differenze = append(e.differenze, id.Percorso+": l'UUID non si legge")
			continue
		}
		var trovato bool
		switch id.Chiave {
		case "allegato_id":
			_, trovato = ix.allegati[u]
		case "thread_id":
			_, trovato = ix.thread[u]
		case "messaggio_id":
			_, trovato = ix.messaggi[u]
		case "documento_id":
			trovato = ix.documenti[u]
		case "componente_id", "padre_id", "figlio_id":
			if componentiAssenti {
				e.nonFatti = append(e.nonFatti, id.Percorso+": i componenti non sono nella fotografia")
				continue
			}
			trovato = ix.componenti[u]
		}
		switch {
		case trovato:
		case parziale:
			e.nonFatti = append(e.nonFatti, id.Percorso+": non è fra i thread scelti")
		default:
			e.differenze = append(e.differenze, id.Percorso+": non si risolve nella fotografia")
		}
	}
	return e
}

// aggiungiNonVuota: s con x, se x non è vuota.
func aggiungiNonVuota(s []string, x string) []string {
	if x == "" {
		return s
	}
	return append(s, x)
}

// conteggioHashDistinti: il conteggio dello scenario che si conta sulla fotografia, non sulle voci (T-B6c-09).
const conteggioHashDistinti = "hash_distinti"

// hashDistinti (T-B6c-09): il conteggio hash_distinti dello scenario contro gli sha256 distinti degli allegati delle sue
// voci, come sono nella fotografia (le voci non scrivono sempre lo sha256; dove lo scrivono, il n.2 lo controlla già).
// Una voce che non si risolve, o un allegato senza sha256, rende il conteggio non decidibile: una parte non verificata,
// mai una differenza; un numero diverso è una differenza.
func hashDistinti(tr traduzione, ix indiceFoto, dichiarato int, parziale bool) (differenza, nonFatto string) {
	const p = "scenario_inoltro.conteggi." + conteggioHashDistinti
	sha := map[string]bool{}
	nonRisolte, senzaSha := 0, 0
	for _, vt := range tr.voci {
		if vt.voce.Sezione != confronto.SezioneScenario {
			continue
		}
		id, motivo := risolviFile(vt.voce, ix)
		if motivo != "" {
			nonRisolte++
			continue
		}
		a := ix.allegati[id].allegato
		if a.Sha256 == nil {
			senzaSha++
			continue
		}
		sha[strings.ToLower(*a.Sha256)] = true
	}
	switch {
	case nonRisolte > 0 && parziale:
		return "", p + ": il thread dello scenario non è fra quelli scelti, o una sua voce non si risolve: il conteggio non si decide"
	case nonRisolte > 0:
		return "", fmt.Sprintf("%s: %d voci dello scenario non si risolvono nella fotografia: il conteggio non si decide", p, nonRisolte)
	case senzaSha > 0:
		return "", fmt.Sprintf("%s: %d allegati delle voci dello scenario senza sha256 nella fotografia: il conteggio non si decide", p, senzaSha)
	case len(sha) != dichiarato:
		return fmt.Sprintf("%s dichiara %d, gli allegati delle voci dello scenario nella fotografia hanno %d sha256 distinti", p, dichiarato, len(sha)), ""
	}
	return "", ""
}

// decisoreDellaSezione (C-05, T-B6c-15, proposta tecnica): il deciso_da della sezione C5 contro gli utenti della
// fotografia (fotorfq.Utenti: UUID e sigla; con -dsn tutti, dalla query ListSigleUtenti del piano 6.5). Nessun SQL nuovo.
//   - Con gli export gli utenti non ci sono: chi ha deciso non si confronta, ed è una parte non verificata (R-102), mai
//     un passato.
//   - Un valore vuoto non nomina un utente: non verificato, con il motivo.
//   - Un UUID dev'essere l'ID di un utente della fotografia; una sigla, alla lettera, la sigla di un utente. Altrimenti è
//     una differenza: con -dsn la fotografia ha tutti gli utenti, e un utente che manca non è un limite della corsa
//     (come confrontaUtente). Gli utenti non dipendono dai thread scelti: con -thread vale lo stesso.
//
// La sezione non dice a quale riga si riferisce chi ha deciso: si verifica che sia un utente, non quale riga abbia
// deciso (quello lo fanno le voci, riga per riga, con confrontaUtente).
func decisoreDellaSezione(d IDAtteso, f fotorfq.Fotografia) (differenza, nonFatto string) {
	if f.Origine == fotorfq.OrigineExport {
		return "", d.Percorso + ": gli export non hanno gli utenti: chi ha deciso non si confronta (R-102)"
	}
	if strings.TrimSpace(d.Valore) == "" {
		return "", d.Percorso + ": il valore è vuoto e non nomina un utente"
	}
	if u, err := uuid.Parse(d.Valore); err == nil {
		for _, x := range f.Utenti {
			if x.ID == u {
				return "", ""
			}
		}
		return d.Percorso + ": l'UUID non è quello di un utente della fotografia", ""
	}
	for _, x := range f.Utenti {
		if x.Sigla == d.Valore {
			return "", ""
		}
	}
	return d.Percorso + ": nessun utente della fotografia ha questa sigla", ""
}

// sezioneDaRivedere (T-B6c-08): il thread della sezione C5 si risolve nella fotografia; il cliente, se la sezione lo
// scrive con l'UUID, è quello del thread della sezione (o, senza il thread, il cliente di un thread della fotografia).
// Con -thread un thread che non c'è può non essere fra quelli scelti: una parte non verificabile. Da C-05 anche chi ha
// deciso la sezione (decisoreDellaSezione).
func sezioneDaRivedere(s SezioniAttesi, ix indiceFoto, f fotorfq.Fotografia, parziale bool) (differenze, nonFatti []string) {
	if d := s.DecisoDaRivedere; d != nil {
		dif, nf := decisoreDellaSezione(*d, f)
		differenze, nonFatti = aggiungiNonVuota(differenze, dif), aggiungiNonVuota(nonFatti, nf)
	}
	var thread *fotorfq.Thread
	if id := s.ThreadDaRivedere; id != nil {
		u, err := uuid.Parse(id.Valore)
		i, ok := ix.thread[u]
		switch {
		case err != nil:
			differenze = append(differenze, id.Percorso+": l'UUID non si legge")
		case ok:
			thread = &f.Thread[i]
		case parziale:
			nonFatti = append(nonFatti, id.Percorso+": non è fra i thread scelti")
		default:
			differenze = append(differenze, id.Percorso+": non si risolve nella fotografia")
		}
	}
	if c := s.ClienteDaRivedere; c != nil {
		u, _ := uuid.Parse(c.Valore) // LeggiSezioni lo tiene solo se è un UUID
		switch {
		case thread != nil:
			if thread.ClienteID != u {
				differenze = append(differenze, c.Percorso+": non è il cliente del thread della sezione")
			}
		case s.ThreadDaRivedere != nil:
			// il thread della sezione non si è risolto: il suo cliente non si confronta (la parte del thread è già sopra)
			nonFatti = append(nonFatti, c.Percorso+": il thread della sezione non è nella fotografia, e il cliente non si confronta")
		default:
			trovato := false
			for _, t := range f.Thread {
				trovato = trovato || t.ClienteID == u
			}
			switch {
			case trovato:
			case parziale:
				nonFatti = append(nonFatti, c.Percorso+": non è il cliente di un thread scelto")
			default:
				differenze = append(differenze, c.Percorso+": non è il cliente di un thread della fotografia")
			}
		}
	}
	return differenze, nonFatti
}

// controlloFonti (n.3, solo -exports): gli export del manifest e le fonti degli attesi coincidono, nelle due direzioni
// (6.4.9: «coincidono»). Lo sha256 di ogni export del manifest sta fra le fonti; e ogni sha256 delle fonti (la chiave
// sha256, che negli attesi hanno gli export: il dump ha sha256_file e non entra) è un export del manifest, altrimenti
// un export che gli attesi elencano si vedrebbe solo come sezione assente [T].
func controlloFonti(s *SezioniAttesi, export []string) esitoControllo {
	e := esitoControllo{nome: ControlloFonti, eseguito: true}
	if s == nil {
		e.eseguito, e.motivo = false, "gli attesi non sono stati letti"
		return e
	}
	var manifest []string
	for _, sha := range export {
		manifest = append(manifest, strings.ToLower(sha))
		if !dentro(strings.ToLower(sha), s.Fonti) {
			e.differenze = append(e.differenze, "uno sha256 degli export non è fra le fonti degli attesi: "+sha)
		}
	}
	for _, sha := range s.Fonti {
		if !dentro(sha, manifest) {
			e.differenze = append(e.differenze, "uno sha256 delle fonti degli attesi non è fra gli export del manifest: "+sha)
		}
	}
	return e
}

// controlloCasiIndice (n.4; R29 b C): il file dei casi della voce casi del manifest è quello che l'indice delle regole
// dichiara per l'anteprima (stesso sha256). Se no, banco e anteprima leggerebbero ingressi diversi.
func controlloCasiIndice(shaCasi string, indice *grammatica.IndiceRegole) esitoControllo {
	e := esitoControllo{nome: ControlloCasiIndice, eseguito: true}
	switch {
	case shaCasi == "":
		e.eseguito, e.motivo = false, "il file dei casi non è stato letto"
	case indice == nil:
		e.eseguito, e.motivo = false, "l'indice delle regole non è stato letto"
	case indice.Casi == nil:
		e.differenze = append(e.differenze, "l'indice delle regole non dichiara il file dei casi")
	case !strings.EqualFold(indice.Casi.Sha256, shaCasi):
		e.differenze = append(e.differenze, "il file dei casi del manifest non è quello dell'indice delle regole")
	}
	return e
}

// controlloAttesiInteri (n.5; A1c-L1P-01): gli attesi si leggono per intero, con lo sha256 del manifest: nessuna
// chiave sconosciuta (né per LeggiAttesi né per LeggiSezioni), e i conteggi che gli attesi dichiarano coincidono con le
// loro voci. I numeri del piano non stanno nel codice: il rapporto scrive quante voci ha ogni sezione, e il revisore li
// confronta.
func controlloAttesiInteri(letti bool, motivoNonLetti string, s *SezioniAttesi) esitoControllo {
	e := esitoControllo{nome: ControlloAttesiInteri, eseguito: true}
	switch {
	case !letti && s == nil:
		e.eseguito, e.motivo = false, motivoNonLetti
		return e
	case !letti:
		e.differenze = append(e.differenze, "gli attesi non si leggono: "+motivoNonLetti)
	}
	if s == nil {
		return e
	}
	for _, k := range s.NonTradotte {
		e.differenze = append(e.differenze, "chiave non tradotta: "+k)
	}
	if s.Scenario != nil {
		for _, c := range controllaConteggi(s.Scenario) {
			e.differenze = append(e.differenze, c)
		}
	}
	return e
}

// I nomi dei conteggi dello scenario che il runner sa contare dalle voci [T]: i file per collocazione attesa
// (radice, figlio; fuori_scenario, come gli attesi chiamano la collocazione fuori richiesta), tutti i file, i prodotti.
// Da B6c i nomi che la prima corsa ha mostrato (T-B6c-09; il piano, 6.4.9: «8 radice, 28 figlio (22 con due radici),
// 13 fuori richiesta»): candidati_radice e candidati_figlio sono i file attesi radice e figlio; file_condivisi i figli
// con più di una radice; allegati i file, una voce per allegato; hash_distinti si conta sulla fotografia, nel n.2
// (conteggioHashDistinti). Un altro nome non si conta in silenzio: è una chiave non tradotta.
var conteggiNoti = []string{"radice", "figlio", "fuori_scenario", "file", "prodotti", "candidati_radice", "candidati_figlio",
	"file_condivisi", "allegati", conteggioHashDistinti}

// collocazioneDelConteggio: la collocazione attesa che un conteggio dei file conta (radice o figlio), se è uno di quelli.
func collocazioneDelConteggio(nome string) (string, bool) {
	switch nome {
	case "radice", "candidati_radice":
		return confronto.AttesoRadice, true
	case "figlio", "candidati_figlio":
		return confronto.AttesoFiglio, true
	}
	return "", false
}

// controllaConteggi: i conteggi dichiarati dallo scenario contro le sue voci; hash_distinti lo conta il n.2 sulla
// fotografia.
func controllaConteggi(s *ScenarioAtteso) []string {
	var out []string
	for _, c := range s.Conteggi {
		if c.Nome == conteggioHashDistinti {
			continue
		}
		n, noto := contaVociScenario(s, c.Nome)
		switch {
		case !noto:
			out = append(out, "chiave non tradotta: scenario_inoltro.conteggi."+c.Nome)
		case n != c.Valore:
			out = append(out, fmt.Sprintf("scenario_inoltro.conteggi.%s dichiara %d, le voci sono %d", c.Nome, c.Valore, n))
		}
	}
	return out
}

// contaVociScenario: le voci dello scenario per un nome di conteggio, e se il nome è noto.
func contaVociScenario(s *ScenarioAtteso, nome string) (int, bool) {
	n := 0
	if coll, ok := collocazioneDelConteggio(nome); ok {
		for _, v := range s.File {
			if v.Atteso == coll {
				n++
			}
		}
		return n, true
	}
	switch nome {
	case "file_condivisi":
		for _, v := range s.File {
			if v.Atteso == confronto.AttesoFiglio && len(v.Radici) > 1 {
				n++
			}
		}
	case "fuori_scenario":
		for _, v := range s.File {
			if v.Atteso == confronto.AttesoFuori || v.AttesoStato == confronto.StatoAttesoFuoriScenario {
				n++
			}
		}
	case "file", "allegati":
		n = len(s.File)
	case "prodotti":
		n = len(s.Prodotti)
	default:
		return 0, false
	}
	return n, true
}

// controlloTraduzione: gli errori della traduzione delle sezioni (D8, R-51, le radici vuote, i valori fuori elenco) e
// i valori che non si leggono. Ognuno è una differenza del runner: una voce che si contraddice non entra negli esiti.
func controlloTraduzione(s *SezioniAttesi, tr traduzione) esitoControllo {
	e := esitoControllo{nome: ControlloTraduzione, eseguito: s != nil}
	if s == nil {
		e.motivo = "gli attesi non sono stati letti"
		return e
	}
	e.differenze = append(append(e.differenze, s.Errori...), tr.errori...)
	return e
}

// ---- gli invarianti delle righe decise (baseline (1) e C5) ----

// confrontoRiga: una riga attesa contro la riga della fotografia, campo per campo. Un campo che la fotografia non porta
// (gli export: deciso_da) non si confronta, e lo dice nonConfrontabili: chi lo usa ne fa una parte non verificabile.
// Un campo che quella riga non ha (per esempio confermato_il in una proposta) non arriva qui: è un errore di traduzione
// (campiDellaRiga, R-102); il ramo default resta per prudenza e dà una parte non verificabile, mai un silenzio.
type confrontoRiga struct {
	differenze       []string
	nonConfrontabili []string
}

// fontiRighe: ciò che il confronto delle righe decise (baseline (1) e C5) sa della fotografia oltre alla riga (B6c):
//   - utenti: gli utenti della fotografia, UUID → sigla (fotorfq.Utente; con -dsn tutti, dalla query ListSigleUtenti
//     del piano 6.5, per la sigla dell'utente nel deciso_da degli attesi C5; con gli export nessuno);
//   - ix: l'indice della fotografia, per il messaggio e il thread dell'allegato della provenienza (T-B6c-02);
//   - export: la fotografia viene dagli export, senza gli utenti e senza deciso_da (R-102);
//   - provenienzeComplete: la sezione delle provenienze è completa (-dsn); con gli export è parziale, solo gli allegati
//     del thread esportato (export.go).
type fontiRighe struct {
	utenti              map[uuid.UUID]string
	ix                  indiceFoto
	export              bool
	provenienzeComplete bool
}

// fontiDi: le fontiRighe di una fotografia.
func fontiDi(f fotorfq.Fotografia, ix indiceFoto, export bool) fontiRighe {
	fr := fontiRighe{utenti: make(map[uuid.UUID]string, len(f.Utenti)), ix: ix, export: export,
		provenienzeComplete: f.Sezioni[fotorfq.SezioneProvenienze].Stato == fotorfq.StatoSezioneCompleta}
	for _, u := range f.Utenti {
		fr.utenti[u.ID] = u.Sigla
	}
	return fr
}

// confrontaUtente (B6c, punto 2 del compito): un campo di chi ha deciso o confermato (deciso_da, confermato_da) contro
// l'UUID della fotografia. La C5 degli attesi lo scrive con la sigla dell'utente (l'estrazione del 03/10 legge la
// sigla; il piano lo prevede: 6.5, ListSigleUtenti), la fotografia con l'UUID.
//   - Un valore atteso che è un UUID (o null) si confronta come prima (differenzaCampo).
//   - Una sigla, con -dsn, si confronta con la sigla dell'utente della fotografia che ha quell'UUID, alla lettera: un
//     campo assente, un UUID senza utente nella fotografia o una sigla diversa sono differenze. La fotografia di -dsn ha
//     tutti gli utenti: un utente che manca non è un limite della corsa, e non si dichiara «non verificato».
//   - Con gli export gli utenti non ci sono: una sigla non si confronta, e nemmeno un campo che l'export non porta; sono
//     parti non verificabili, mai un passato (come deciso_da con gli export, R-102).
//
// La regola vale per ogni riga decisa che il runner confronta (la C5 e la baseline): è la stessa funzione, e una sigla
// nella baseline si giudicherebbe allo stesso modo (T-B6c-10). Nessun SQL nuovo: gli utenti li legge già il caricatore.
func confrontaUtente(nome string, c CampoAtteso, ottenuto *uuid.UUID, fr fontiRighe) (differenza, nonConfrontabile string) {
	_, err := uuid.Parse(c.Valore)
	sigla := !c.Nullo && err != nil
	assente := ottenuto == nil || *ottenuto == uuid.Nil
	switch {
	case fr.export && assente:
		return "", nome + " (non è negli export)"
	case fr.export && sigla:
		return "", nome + " (sigla: gli export non hanno gli utenti, e la sigla non si confronta)"
	case !sigla:
		return differenzaCampo(nome, c, uuidTesto(ottenuto)), ""
	case assente:
		return nome + ": attesa la sigla di un utente, nella fotografia è assente", ""
	}
	s, ok := fr.utenti[*ottenuto]
	switch {
	case !ok:
		return nome + ": l'utente della fotografia non è fra gli utenti letti: la sigla non si confronta con niente", ""
	case s != c.Valore:
		return nome + ": la sigla non è quella dell'utente della fotografia", ""
	}
	return "", ""
}

// confrontaProposta: una riga attesa di proposta contro la proposta attuale del file. export: la riga è quella
// dell'export (senza deciso_da, R-102); fr: gli utenti, per deciso_da scritto con la sigla (confrontaUtente).
func confrontaProposta(riga []CampoAtteso, p *fotorfq.PropostaAttuale, export bool, fr fontiRighe) confrontoRiga {
	var out confrontoRiga
	if len(riga) == 0 {
		return out
	}
	if p == nil {
		out.differenze = append(out.differenze, "la fotografia non ha la proposta del file")
		return out
	}
	for _, c := range riga {
		var ottenuto *string
		switch c.Nome {
		case "codice":
			ottenuto = p.Codice
		case "rev":
			ottenuto = p.Rev
		case "componente_id":
			ottenuto = uuidTesto(p.ComponenteID)
		case "proposta_id":
			ottenuto = uuidTesto(&p.ID)
		case "stato":
			ottenuto = &p.Stato
		case "fonte":
			ottenuto = &p.Fonte
		case "tipo", "tipo_proposto":
			ottenuto = &p.Tipo
		case "deciso_il":
			ottenuto = tempoTesto(p.DecisoIl)
		case "deciso_da":
			if export {
				out.nonConfrontabili = append(out.nonConfrontabili, "proposta.deciso_da (non è negli export)")
				continue
			}
			d, nc := confrontaUtente("proposta.deciso_da", c, p.DecisoDa, fr)
			out.differenze, out.nonConfrontabili = aggiungiNonVuota(out.differenze, d), aggiungiNonVuota(out.nonConfrontabili, nc)
			continue
		default:
			out.nonConfrontabili = append(out.nonConfrontabili, "proposta."+c.Nome)
			continue
		}
		if d := differenzaCampo("proposta."+c.Nome, c, ottenuto); d != "" {
			out.differenze = append(out.differenze, d)
		}
	}
	return out
}

// confrontaDocumento: una riga attesa di documento contro il documento confermato che porta il file. fr: gli utenti,
// per confermato_da scritto con la sigla (confrontaUtente), e l'indice e lo stato delle provenienze, per la
// provenienza (allegatoDellaProvenienza, messaggioDellaProvenienza: T-B6c-02).
func confrontaDocumento(riga []CampoAtteso, d *fotorfq.DocumentoConfermato, fr fontiRighe) confrontoRiga {
	var out confrontoRiga
	if len(riga) == 0 {
		return out
	}
	if d == nil {
		out.differenze = append(out.differenze, "la fotografia non ha un documento confermato che porti il file")
		return out
	}
	for _, c := range riga {
		var ottenuto *string
		switch c.Nome {
		case "codice":
			ottenuto = d.Codice
		case "rev":
			ottenuto = d.Rev
		case "componente_id":
			ottenuto = uuidTesto(d.ComponenteID)
		case "documento_id":
			s := d.ID.String()
			ottenuto = &s
		case "thread_id":
			ottenuto = uuidTesto(&d.ThreadID)
		case "tipo":
			ottenuto = &d.Tipo
		case "estensione":
			ottenuto = &d.Estensione
		case "sostituito_da":
			ottenuto = uuidTesto(d.SostituitoDa)
		case "confermato_il":
			ottenuto = tempoTesto(&d.ConfermatoIl)
		case "confermato_da":
			x, nc := confrontaUtente("documento.confermato_da", c, &d.ConfermatoDa, fr)
			out.differenze, out.nonConfrontabili = aggiungiNonVuota(out.differenze, x), aggiungiNonVuota(out.nonConfrontabili, nc)
			continue
		case "allegato_id":
			x, nc := allegatoDellaProvenienza(c, d, fr)
			out.differenze, out.nonConfrontabili = aggiungiNonVuota(out.differenze, x), aggiungiNonVuota(out.nonConfrontabili, nc)
			continue
		case "messaggio_id":
			x, nc := messaggioDellaProvenienza(c, riga, fr)
			out.differenze, out.nonConfrontabili = aggiungiNonVuota(out.differenze, x), aggiungiNonVuota(out.nonConfrontabili, nc)
			continue
		case "sha256":
			ottenuto = &d.Sha256
		case "nome_file":
			ottenuto = &d.NomeFile
		default:
			out.nonConfrontabili = append(out.nonConfrontabili, "documento."+c.Nome)
			continue
		}
		if x := differenzaCampo("documento."+c.Nome, c, ottenuto); x != "" {
			out.differenze = append(out.differenze, x)
		}
	}
	return out
}

// allegatoDellaProvenienza (T-B6c-02): allegato_id di una riga attesa del documento. La fotografia porta del documento
// gli allegati della provenienza (documento_provenienza.allegato_id; con gli export solo quelli del thread esportato, e
// la sezione è parziale), non le righe di provenienza intere.
//   - Un allegato atteso è giusto se è fra quelli del documento. Se non c'è è una differenza quando la fotografia
//     l'avrebbe: le provenienze sono complete (-dsn), oppure l'allegato è del thread del documento (gli export tolgono
//     solo quelli fuori dal thread). Altrimenti non si decide.
//   - Un null atteso (una provenienza senza allegato) non si decide: il documento della voce porta l'allegato della voce
//     (righeDelFile), e la riga attesa può essere un'altra provenienza dello stesso documento, che la fotografia non
//     distingue.
func allegatoDellaProvenienza(c CampoAtteso, d *fotorfq.DocumentoConfermato, fr fontiRighe) (differenza, nonConfrontabile string) {
	const nome = "documento.allegato_id"
	if c.Nullo {
		return "", nome + " (null: una provenienza senza allegato non si distingue fra le provenienze del documento nella fotografia)"
	}
	u, err := uuid.Parse(c.Valore)
	if err != nil {
		return nome + ": l'UUID non si legge", ""
	}
	for _, a := range d.Allegati {
		if a == u {
			return "", ""
		}
	}
	if ra, ok := fr.ix.allegati[u]; fr.provenienzeComplete || (ok && !ra.fuori && ra.thread == d.ThreadID) {
		return nome + ": non è fra gli allegati della provenienza del documento nella fotografia", ""
	}
	return "", nome + " (la sezione delle provenienze è parziale e l'allegato non è del thread del documento: non si decide)"
}

// messaggioDellaProvenienza (T-B6c-02): messaggio_id di una riga attesa del documento. La fotografia non porta
// documento_provenienza.messaggio_id (nessun SQL nuovo, T-B0-36). Il prodotto scrive la provenienza con l'allegato e il
// messaggio di quell'allegato insieme (la conferma dall'allegato, transport/web/allegati.go): il messaggio atteso si
// verifica come il messaggio, nella fotografia, dell'allegato atteso della stessa riga. Senza un allegato nella riga, con
// un allegato che la fotografia non ha, o con un null atteso, non si decide: una parte non verificabile, mai un passato.
func messaggioDellaProvenienza(c CampoAtteso, riga []CampoAtteso, fr fontiRighe) (differenza, nonConfrontabile string) {
	const nome = "documento.messaggio_id"
	if c.Nullo {
		return "", nome + " (null: la fotografia non porta il messaggio della provenienza)"
	}
	a, ok := campo(riga, "allegato_id")
	if !ok || a.Nullo {
		return "", nome + " (la riga non dice l'allegato della provenienza: la fotografia non porta il messaggio della provenienza, e il messaggio si ricava solo dall'allegato)"
	}
	u, err := uuid.Parse(a.Valore)
	ra, trovato := fr.ix.allegati[u]
	if err != nil || !trovato {
		return "", nome + " (l'allegato della provenienza non è nella fotografia: il suo messaggio non si ricava)"
	}
	if !strings.EqualFold(ra.allegato.MessaggioID.String(), c.Valore) {
		return nome + ": non è il messaggio dell'allegato della provenienza nella fotografia", ""
	}
	return "", ""
}

// differenzaCampo: "" se il campo atteso coincide con quello della fotografia. Un null atteso vuole un campo assente; gli
// istanti si confrontano al millisecondo, in UTC; gli UUID e gli sha256 senza le maiuscole.
func differenzaCampo(nome string, c CampoAtteso, ottenuto *string) string {
	switch {
	case c.Nullo && ottenuto == nil:
		return ""
	case c.Nullo:
		return nome + ": atteso null, nella fotografia c'è un valore"
	case ottenuto == nil:
		return nome + ": atteso un valore, nella fotografia è assente"
	}
	atteso, valore := c.Valore, *ottenuto
	if strings.HasSuffix(nome, "_il") {
		ta, err := tempoAtteso(atteso)
		if err != nil {
			return nome + ": " + err.Error()
		}
		atteso = ta.Format("2006-01-02T15:04:05.000Z07:00")
	}
	if strings.HasSuffix(nome, "_id") || strings.HasSuffix(nome, "_da") || strings.HasSuffix(nome, "sha256") {
		atteso, valore = strings.ToLower(atteso), strings.ToLower(valore)
	}
	if atteso != valore {
		return nome + ": diverso dalla fotografia"
	}
	return ""
}

func uuidTesto(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func tempoTesto(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	return &s
}

// righeDelFile: la proposta attuale e il documento confermato che porta un allegato, nella fotografia.
func righeDelFile(t *fotorfq.Thread, allegato uuid.UUID) (*fotorfq.PropostaAttuale, *fotorfq.DocumentoConfermato) {
	var p *fotorfq.PropostaAttuale
	var d *fotorfq.DocumentoConfermato
	for i := range t.Proposte {
		if t.Proposte[i].AllegatoID == allegato {
			p = &t.Proposte[i]
		}
	}
	for i := range t.Documenti {
		for _, a := range t.Documenti[i].Allegati {
			if a == allegato {
				d = &t.Documenti[i]
			}
		}
	}
	return p, d
}

// esitoBaseline: i controlli (1) e (2) di una voce della baseline (6.4.9): le righe di proposta e di documento della
// fotografia uguali alla decisione attesa, e la base letta dal motore nel file uguale ad atteso.base (con le letture
// d'identità del file: Nuovo.Basi, esatto come la chiave «base» dei casi, R25 c). Righe e Base dicono che la parte si
// è verificata ed è uguale; Differenze sono gli scarti; NonVerificate le parti che la corsa non può verificare
// (deciso_da con gli export: R-102; il file di un thread che non si valuta per un limite degli ingressi: T-B6-104).
// Una decisione è preservata solo senza differenze e senza parti non verificate.
//
// Revisione: l'indicatore di revisione della riga di confronto del file (valore, le due revisioni, la provenienza della
// vecchia, il motivo), nil se il file non ha una riga. Serve alla voce del gate delle decisioni preservate, che conta le
// differenze di revisione diagnosticate (R113 B ratificata; E2 §3.4); non entra in Righe né in Base, e non toglie la
// preservazione: l'indicatore è separato dalla correttezza dell'associazione (R113).
type esitoBaseline struct {
	Percorso      string                         `json:"percorso"`
	ID            string                         `json:"id,omitempty"`
	AllegatoID    uuid.UUID                      `json:"allegato_id"`
	Righe         bool                           `json:"righe"`
	Base          bool                           `json:"base"`
	Differenze    []string                       `json:"differenze,omitempty"`
	NonVerificate []string                       `json:"non_verificate,omitempty"`
	Revisione     *confronto.IndicatoreRevisione `json:"revisione,omitempty"`
}

// controllaBaseline: (1) e (2) per ogni voce risolta della baseline. Per un file che il motore non ha valutato la
// differenza dice «file non valutato», con il motivo del motore (R-115), non «la base è diversa». revisioni: gli
// indicatori di revisione delle righe di confronto, per allegato (R113 B).
func controllaBaseline(tr traduzione, f fotorfq.Fotografia, ix indiceFoto, nuovi map[uuid.UUID]confronto.File,
	revisioni map[uuid.UUID]confronto.IndicatoreRevisione, cc contestoCorsa) []esitoBaseline {
	var out []esitoBaseline
	fr := fontiDi(f, ix, cc.export)
	for _, vt := range tr.voci {
		if vt.voce.Sezione != confronto.SezioneBaseline || !vt.risolta {
			continue
		}
		t := &f.Thread[ix.thread[vt.thread]]
		p, d := righeDelFile(t, vt.file.AllegatoID)
		eb := esitoBaseline{Percorso: vt.voce.Percorso, ID: vt.voce.ID, AllegatoID: vt.file.AllegatoID}
		if r, ok := revisioni[vt.file.AllegatoID]; ok {
			eb.Revisione = &r
		}
		cp := confrontaProposta(vt.voce.Proposta, p, cc.export, fr)
		cd := confrontaDocumento(vt.voce.Documento, d, fr)
		eb.Differenze = append(append(eb.Differenze, cp.differenze...), cd.differenze...)
		eb.NonVerificate = append(append(eb.NonVerificate, cp.nonConfrontabili...), cd.nonConfrontabili...)
		if atteso, ok := vt.voce.invariante("componente_id_proposta_vuoto_preservato"); ok {
			if p == nil || (p.ComponenteID == nil) != atteso {
				eb.Differenze = append(eb.Differenze, "componente_id_proposta_vuoto_preservato non coincide")
			}
		}
		// decisioni_invariate vero (falso è un errore di traduzione) lo verificano le righe di questo controllo (1):
		// senza righe attese non c'è niente che lo verifichi, e la parte resta non verificata (T-B6c-01).
		if atteso, ok := vt.voce.invariante("decisioni_invariate"); ok && atteso && len(vt.voce.Proposta) == 0 && len(vt.voce.Documento) == 0 {
			eb.NonVerificate = append(eb.NonVerificate, "atteso.decisioni_invariate: la voce non ha righe decise da confrontare")
		}
		eb.Righe = len(eb.Differenze) == 0
		n, ok := nuovi[vt.file.AllegatoID]
		base := vt.file.BaseLetta
		switch {
		case cc.nonValutabili[vt.thread] != "":
			eb.NonVerificate = append(eb.NonVerificate, "atteso.base: "+cc.nonValutabili[vt.thread])
		case !ok || !n.Nuovo.Valutato:
			motivo := "file non valutato"
			if ok && n.Nuovo.Motivo != "" {
				motivo += ": " + n.Nuovo.Motivo
			}
			eb.Differenze = append(eb.Differenze, motivo+" (la base letta non c'è)")
		case len(n.Nuovo.Basi) == 1 && n.Nuovo.Basi[0] == base:
			eb.Base = true
		default:
			eb.Differenze = append(eb.Differenze, "la base letta nel file non è atteso.base")
		}
		out = append(out, eb)
	}
	return out
}

// controlloBaselineDi: i controlli (1) e (2) della baseline come voce dei controlli: una voce che non li passa è una
// differenza (il gate la conta anche fra le decisioni preservate); una parte non verificata è fra i nonFatti, quindi il
// controllo senza differenze è NON ESEGUITO, come la C5 per lo stesso caso (R-102).
func controlloBaselineDi(eb []esitoBaseline, letti bool) esitoControllo {
	e := esitoControllo{nome: ControlloBaselineRighe, eseguito: letti}
	if !letti {
		e.motivo = "gli attesi non sono stati letti"
		return e
	}
	for _, x := range eb {
		for _, d := range x.Differenze {
			e.differenze = append(e.differenze, x.Percorso+": "+d)
		}
		for _, d := range x.NonVerificate {
			e.nonFatti = append(e.nonFatti, x.Percorso+": "+d)
		}
	}
	return e
}

// controlloInvariantiC5 (6.4.9, «La sezione C5 nel runner»): per ogni file della C5,
//   - con -dsn le righe della fotografia uguali a decisione_proposta e decisione_documento (sovrascritta_dal_motore
//     falso); con -exports uguali a nell_export_proposte (la riga della proposta dell'export; il nome neutro della
//     chiave, come M-21). La riga dell'altra modalità non si applica a questa corsa (il piano assegna ogni riga a una
//     modalità sola, e la verifica la corsa dell'altra): va fra le parti non applicabili, un elenco informativo che non
//     cambia l'uscita (R-117). Chi ha deciso e chi ha confermato la C5 lo scrive con la sigla: confrontaUtente;
//   - componente_id_proposta_vuoto_preservato uguale al valore dichiarato;
//   - in_baseline uguale a «il file è nella baseline degli attesi»; conta_nel_gate falso (la C5 è fuori dal gate);
//   - mostrata_con_badge_di_confronto: il file ha una riga di confronto, e quindi il badge calcolato contro la decisione
//     (R31 a); badge sono i badge del confronto, per allegato.
//
// Le righe della conferma dell'albero (RigheAlbero) non sono qui: sono righe legacy, non un'uscita del motore A, e gli
// attesi le portano come contesto. Il runner ne risolve gli ID (n.2) e non verifica che siano invariate; stanno nella
// sezione informativa del rapporto righe_conferma_albero, con il conteggio e il motivo (D-R116 I, decisa dall'utente
// l'08/10). È un'esclusione specifica e motivata dalla parte obbligatoria di questo controllo, ratificata come la delega
// delle sentinelle: il controllo resta obbligatorio per le sue parti verificabili, i file confermati. L'etichetta «da
// rivedere» e le anomalie restano nel rapporto, accanto agli esiti del file.
func controlloInvariantiC5(s *SezioniAttesi, tr traduzione, f fotorfq.Fotografia, ix indiceFoto, cc contestoCorsa, badge map[uuid.UUID]confronto.Badge) esitoControllo {
	e := esitoControllo{nome: ControlloInvariantiC5, eseguito: s != nil}
	if s == nil {
		e.motivo = "gli attesi non sono stati letti"
		return e
	}
	fr := fontiDi(f, ix, cc.export)
	inBaseline := map[uuid.UUID]bool{}
	for _, vt := range tr.voci {
		if vt.voce.Sezione == confronto.SezioneBaseline && vt.risolta {
			inBaseline[vt.file.AllegatoID] = true
		}
	}
	for _, vt := range tr.voci {
		if vt.voce.Sezione != confronto.SezioneDaRivedere || !vt.risolta {
			continue
		}
		t := &f.Thread[ix.thread[vt.thread]]
		p, d := righeDelFile(t, vt.file.AllegatoID)
		var righe []confrontoRiga
		if cc.export {
			righe = append(righe, confrontaProposta(vt.voce.Export, p, true, fr))
			if len(vt.voce.Proposta) > 0 {
				e.nonApplicabili = append(e.nonApplicabili, vt.voce.Percorso+".decisione_proposta: si verifica con -dsn")
			}
			if len(vt.voce.Documento) > 0 {
				e.nonApplicabili = append(e.nonApplicabili, vt.voce.Percorso+".decisione_documento: si verifica con -dsn")
			}
		} else {
			righe = append(righe, confrontaProposta(vt.voce.Proposta, p, false, fr), confrontaDocumento(vt.voce.Documento, d, fr))
			if len(vt.voce.Export) > 0 {
				e.nonApplicabili = append(e.nonApplicabili, vt.voce.Percorso+".nell_export_proposte: si verifica con -exports")
			}
		}
		for _, r := range righe {
			for _, x := range r.differenze {
				e.differenze = append(e.differenze, vt.voce.Percorso+": "+x)
			}
			for _, x := range r.nonConfrontabili {
				e.nonFatti = append(e.nonFatti, vt.voce.Percorso+": "+x)
			}
		}
		if v, ok := vt.voce.invariante("mostrata_con_badge_di_confronto"); ok {
			if m := cc.nonValutabili[vt.thread]; m != "" {
				e.nonFatti = append(e.nonFatti, vt.voce.Percorso+".mostrata_con_badge_di_confronto: "+m)
			} else if _, ha := badge[vt.file.AllegatoID]; ha != v {
				e.differenze = append(e.differenze, vt.voce.Percorso+": mostrata_con_badge_di_confronto non coincide con le righe del confronto")
			}
		}
		if v, ok := vt.voce.invariante("sovrascritta_dal_motore"); ok && v {
			e.differenze = append(e.differenze, vt.voce.Percorso+": sovrascritta_dal_motore vero: una decisione sovrascritta non è un invariante")
		}
		if v, ok := vt.voce.invariante("componente_id_proposta_vuoto_preservato"); ok && (p == nil || (p.ComponenteID == nil) != v) {
			e.differenze = append(e.differenze, vt.voce.Percorso+": componente_id_proposta_vuoto_preservato non coincide")
		}
		if v, ok := vt.voce.invariante("in_baseline"); ok && v != inBaseline[vt.file.AllegatoID] {
			e.differenze = append(e.differenze, vt.voce.Percorso+": in_baseline non coincide con la baseline degli attesi")
		}
		if v, ok := vt.voce.invariante("conta_nel_gate"); ok && v {
			e.differenze = append(e.differenze, vt.voce.Percorso+": conta_nel_gate vero, ma la C5 è fuori dal gate")
		}
	}
	sort.Strings(e.nonFatti)
	return e
}

// motivoRigheAlbero: il motivo della sezione informativa delle righe della «Conferma l'albero» (D-R116 I).
const motivoRigheAlbero = "righe legacy della «Conferma l'albero»: non sono un'uscita del motore A, e gli attesi le portano come contesto; il runner ne risolve gli ID (n.2) e non verifica che siano invariate nella fotografia (R-109; D-R116 I, decisa dall'utente l'08/10)"

// righeConfermaAlbero: la sezione informativa del rapporto con le righe della «Conferma l'albero» della C5 (D-R116 I);
// nil se gli attesi non ne hanno.
func righeConfermaAlbero(s *SezioniAttesi) *RigheInformative {
	if s == nil || len(s.RigheAlbero) == 0 {
		return nil
	}
	return &RigheInformative{Righe: len(s.RigheAlbero), Motivo: motivoRigheAlbero, Percorsi: append([]string(nil), s.RigheAlbero...)}
}

// ---- le letture attese e le chiavi booleane dei file (R-101) ----

// chiaviNonDirette: le chiavi della tabella dei casi che sul file intero non hanno un confronto diretto: vogliono il
// testo del caso, che è una sola unità (originale_conservato), o gli altri casi dello stesso profilo (forma_distinta).
var chiaviNonDirette = map[string]string{
	"originale_conservato": "vuole il testo di un caso di una sola unità, non un file",
	"forma_distinta":       "vuole gli altri casi dello stesso profilo, non un file",
}

// fileDelThread: l'interpretazione di un file nell'esito e se il suo thread è valutato.
type fileDelThread struct {
	fi       *valut.FileInterpretato
	valutato bool
	cliente  uuid.UUID
}

// controlloLetture (R-101; la regola delle chiavi accettate): per ogni voce risolta,
//   - baseline: le letture attese oltre a base (che è il controllo (2)), con la logica dei casi di A1a, cioè la regola
//     della chiave nella tabella dei casi sull'interpretazione del file; revisione_confrontata_separatamente contro la
//     riga di confronto del file (revisioni: l'indicatore di revisione accanto al badge, E-19, R31; T-B6c-01);
//   - reali: tutte le letture attese, base compresa, e target_presente con il predicato di R70 A sul thread del file;
//   - la C5, se l'atteso del file è una mappa di letture (T-B6c-07): tutte, come i reali.
//
// Una lettura che la regola dà fallita è una differenza; dove il confronto non è diretto la chiave è fra i nonFatti, con
// il percorso (lettureDelFile dice quando). Il file dei reali resta riservato nel gate (R21 c A): qui si verificano le
// letture. scritture_consentite dello scenario non è una lettura: la dichiara la voce del gate «zero scritture» (R-118).
//
// Le letture con l'interpretazione parziale restano obbligatorie (D-R116 O, decisa dall'utente l'08/10): si giudicano i
// valori realmente disponibili, cioè le chiavi con l'ambito di un'unità che si legge per intero (il nome), e il resto
// resta dichiarato non verificato; un controllo con parti non verificate non è mai passato.
func controlloLetture(s *SezioniAttesi, tr traduzione, file map[uuid.UUID]fileDelThread, esito *valut.Esito, ins *motorea.InsiemeRegole,
	cc contestoCorsa, revisioni map[uuid.UUID]confronto.IndicatoreRevisione) esitoControllo {
	e := esitoControllo{nome: ControlloLetture, eseguito: s != nil}
	if s == nil {
		e.motivo = "gli attesi non sono stati letti"
		return e
	}
	threadDi := map[uuid.UUID]*valut.EsitoThread{}
	if esito != nil {
		for i := range esito.Thread {
			threadDi[esito.Thread[i].ThreadID] = &esito.Thread[i]
		}
	}
	for _, vt := range tr.voci {
		if !vt.risolta {
			continue
		}
		v := vt.voce
		if v.Sezione != confronto.SezioneBaseline && v.Sezione != confronto.SezioneReali && v.Sezione != confronto.SezioneDaRivedere {
			continue
		}
		var letture []ChiaveAttesa
		for _, k := range v.Letture {
			if v.Sezione == confronto.SezioneBaseline && k.Chiave == "base" {
				continue
			}
			letture = append(letture, k)
		}
		d, nf, dedotti := lettureDelFileNellaSezione(v.Percorso, v.Sezione, letture, file[vt.file.AllegatoID], ins, cc.nonValutabili[vt.thread])
		e.differenze = append(e.differenze, d...)
		e.nonFatti = append(e.nonFatti, nf...)
		e.ambitiDedotti = append(e.ambitiDedotti, dedotti...)
		if atteso, ok := v.invariante("revisione_confrontata_separatamente"); ok {
			if x := revisioneSeparata(v.Percorso+".atteso.revisione_confrontata_separatamente", atteso, revisioni, vt.file.AllegatoID); x != "" {
				e.differenze = append(e.differenze, x)
			}
		}
		if atteso, ok := v.invariante("target_presente"); ok {
			p := v.Percorso + ".target_presente"
			et := threadDi[vt.thread]
			switch {
			case cc.nonValutabili[vt.thread] != "":
				e.nonFatti = append(e.nonFatti, p+": "+cc.nonValutabili[vt.thread])
			case et == nil || !et.Valutato:
				e.nonFatti = append(e.nonFatti, p+": il thread del file non è valutato")
			default:
				if x, n := predicatoConfermati(*et, atteso, cc.componentiAssenti, p); x != "" {
					e.differenze = append(e.differenze, x)
				} else if n != "" {
					e.nonFatti = append(e.nonFatti, n)
				}
			}
		}
	}
	sort.Strings(e.nonFatti)
	return e
}

// revisioneSeparata (T-B6c-01; E-19, R31; 6.4.9, la baseline (4): «indicatore di revisione, solo diagnosi»):
// revisione_confrontata_separatamente dice che la revisione del file si confronta a parte, nell'indicatore di revisione
// accanto al badge, e non nel badge. La verifica: la riga di confronto del file ha l'indicatore, con uno dei tre valori.
// Un file senza riga, o un indicatore senza un valore dell'elenco, è una differenza; un atteso falso contraddice il
// confronto, che la revisione la tiene sempre a parte (R31), ed è una differenza anche lui.
func revisioneSeparata(percorso string, atteso bool, revisioni map[uuid.UUID]confronto.IndicatoreRevisione, allegato uuid.UUID) string {
	r, ok := revisioni[allegato]
	switch {
	case !atteso:
		return percorso + ": falso, ma il confronto tiene sempre la revisione a parte, nell'indicatore accanto al badge (R31)"
	case !ok:
		return percorso + ": il file non ha una riga di confronto, e quindi nemmeno l'indicatore di revisione"
	case r.Valore != confronto.RevisioneUguale && r.Valore != confronto.RevisioneDiversa && r.Valore != confronto.RevisioneNonDeterminabile:
		return fmt.Sprintf("%s: l'indicatore di revisione del file ha il valore %q, fuori elenco", percorso, r.Valore)
	}
	return ""
}

// ambitiDellaSezione (D-R116 O, decisa dall'utente l'08/10: la verifica minima): le chiavi che in una sezione degli
// attesi parlano di un'unità precisa perché il piano lo dice di quella sezione. reali_archivi: la base è quella del nome
// dell'archivio. Il piano lo dice in tre punti: 4.6.10, la forma «nome-token» [base][«  »][token «00»] su nome_file, con
// la sezione come caso reale; 6.4.9, la tabella delle sezioni («base, token «00» conservato, revisione_dal_nome e
// revisione_da_token_base nulli; diagnosi archivio.non_estraibile sui .7z»); A1c-L4D-10, i due .7z senza fatti, con il
// solo esito. Il nome si legge sempre per intero (lettureDelFileConAmbiti), quindi la base si giudica sulle sole
// letture del nome. Non vale per le chiavi di unicità del file intero (numero_codici, letture_identita, basi…): restano
// sul file intero, e con l'interpretazione parziale non si giudicano.
//
// C-04 (09/10, restringe T-B6c-12): l'ambito della sezione vale solo per il file in cui il nome è tutto ciò che si
// legge (nomeETuttoIlFile). La sola presenza di una base nel nome non basta: un documento con un contenuto letto in
// parte potrebbe avere nel contenuto tagliato un'altra base, e la chiave (esatta) passerebbe su una lettura parziale.
// Negli altri casi la chiave torna alla regola del file intero: con l'interpretazione parziale resta non verificata,
// con il motivo di A1b-22.
var ambitiDellaSezione = map[string]map[string]evidenze.Contesto{
	confronto.SezioneReali: {"base": evidenze.ContestoNomeFile},
}

// nomeETuttoIlFile (C-04; D-R116 O, la verifica minima): "" se il nome è tutto ciò che del file si legge, cioè se ogni
// unità del documento è del nome, nessuna è troncata e nessun limite rende incompleta la lettura (né nei limiti della
// qualità della fonte né fra le diagnostiche dell'interpretazione, grammatica.CodiceLimiteSuperato). Allora giudicare
// sul nome equivale a giudicare sul file intero: è il caso degli archivi senza contenuto per costruzione (i .7z).
// Altrimenti il motivo, che il rapporto riporta accanto a quello di A1b-22.
func nomeETuttoIlFile(fi *valut.FileInterpretato) string {
	if fi == nil {
		return "il file non è interpretato"
	}
	if len(fi.Documento.Unita) == 0 {
		return "il documento non ha unità"
	}
	for _, u := range fi.Documento.Unita {
		switch {
		case u.Selettore.Contesto != evidenze.ContestoNomeFile:
			return "il documento ha anche l'unità " + string(u.Selettore.Contesto)
		case u.Qualita.Troncata:
			return "l'unità del nome è troncata"
		}
	}
	if len(fi.Documento.Qualita.Limiti) > 0 {
		return "la fonte dichiara un limite"
	}
	for _, d := range fi.Interpretazione.Diagnostiche {
		if d.Codice == grammatica.CodiceLimiteSuperato {
			return "un limite dell'indice è superato"
		}
	}
	return ""
}

// ambitoDi: l'ambito di una chiave delle letture in una sezione: quello della sezione (ambitiDellaSezione), se c'è,
// altrimenti quello della chiave (ambitoDellaChiave), con i sinonimi delle letture.
func ambitoDi(sezione, chiave string) (evidenze.Contesto, bool) {
	if a, ok := ambitiDellaSezione[sezione][chiave]; ok {
		return a, true
	}
	a, ok := ambitoDellaChiave[chiaveDellaRegola(chiave)]
	return a, ok
}

// ambitoDellaChiave (R-116, T-B6-131): le chiavi con un ambito, cioè che parlano di un'unità precisa, si giudicano solo
// sulle letture di quell'unità, mai sull'interpretazione del file intero (altrimenti la revisione del cartiglio farebbe
// passare revisione_dal_nome). Il valore è il contesto dell'unità; vuoto per l'ambito «questo campo», che una voce di
// file non dice. Le revisioni dal token e dal token della base sono del nome (i casi del nome li hanno, e i file reali
// li dichiarano sul nome dell'archivio) [T]; la nota è il testo del PDF (il caso che la definisce è su testo_pdf) [T].
var ambitoDellaChiave = map[string]evidenze.Contesto{
	"revisione_dal_nome":        evidenze.ContestoNomeFile,
	"decorazione_nome_file":     evidenze.ContestoNomeFile,
	"revisione_dal_token":       evidenze.ContestoNomeFile,
	"revisione_da_token_base":   evidenze.ContestoNomeFile,
	"identita_file_da_nota":     evidenze.ContestoTestoPDF,
	"revisione_da_questo_campo": "",
}

// chiaviDelToken (T-B6-200; O-1 della controprova delle risposte): le chiavi dei token della revisione non nominano
// un'unità, ma parlano del token di un'unità precisa. Sul file intero il token di un'altra unità (il cartiglio) farebbe
// passare una voce che parla del nome: un passato falso, contro la regola «verificata, oppure dichiarata non
// verificata». Si giudicano sul solo ambito che la voce degli attesi indica (ambitoDelToken); se l'ambito non si
// determina, sono fra i nonFatti con il motivo. Mai sul file intero.
var chiaviDelToken = map[string]bool{"token_revisione": true, "token_conservato": true, "identita_include_token": true}

// ambitoDelToken: l'ambito che la voce indica per le sue chiavi dei token, cioè quello delle altre chiavi della sua
// mappa «atteso» che hanno un ambito (ambitoDellaChiave), se è uno solo e dice un'unità [T]. Restituisce l'ambito, oppure
// il motivo per cui non si determina: nessuna chiave con un ambito, chiavi con ambiti diversi, l'ambito «questo campo»
// (che una voce di file non dice).
// Da B6c con la sezione: le chiavi sorelle prendono anche l'ambito che il piano dà loro in quella sezione
// (ambitiDellaSezione).
func ambitoDelToken(letture []ChiaveAttesa, sezione string) (evidenze.Contesto, string) {
	visti := map[evidenze.Contesto]bool{}
	var nomi []string
	for _, k := range letture {
		a, ok := ambitoDi(sezione, k.Chiave)
		if !ok || visti[a] {
			continue
		}
		visti[a] = true
		if a == "" {
			nomi = append(nomi, "«questo campo»")
		} else {
			nomi = append(nomi, string(a))
		}
	}
	sort.Strings(nomi)
	switch {
	case len(visti) == 0:
		return "", "la chiave del token non nomina un'unità e la voce non ne indica una (nessuna chiave con un ambito): sul file intero il confronto non è diretto (T-B6-200)"
	case len(visti) > 1:
		return "", "le chiavi con un ambito della voce indicano unità diverse (" + strings.Join(nomi, ", ") + "): l'ambito della chiave del token non si determina (T-B6-200)"
	case visti[""]:
		return "", "la voce indica l'ambito «questo campo», che una voce di file non dice: l'ambito della chiave del token non si determina (T-B6-200)"
	}
	return evidenze.Contesto(nomi[0]), ""
}

// lettureDelFile: le letture attese di un file contro la sua interpretazione, con le regole della tabella dei casi
// (A1a), sul motore del cliente del thread. Restituisce le differenze e le parti non verificabili, con il percorso.
//   - Il file non è valutato, o il thread non si valuta per un limite degli ingressi: ogni chiave è fra i nonFatti.
//   - Una chiave con un ambito (ambitoDellaChiave) si giudica sulle sole letture dell'unità che nomina. Il nome si legge
//     sempre per intero, anche quando il documento è parziale: le chiavi del nome si giudicano comunque. Un'altra unità
//     vuole l'interpretazione completa. Senza letture di quell'unità, o con l'ambito «questo campo», la chiave è fra i
//     nonFatti.
//   - Una chiave dei token (chiaviDelToken) prende l'ambito che la voce indica (ambitoDelToken), e poi si giudica come
//     una chiave con quell'ambito; se l'ambito non si determina, è fra i nonFatti con il motivo (T-B6-200). L'ambito è
//     dedotto, non dichiarato (T-B6-220, precisata con R-140): un'asserzione negativa (asserzioneNegativa) è fra i
//     nonFatti, perché su un'unità più stretta del file passerebbe a vuoto; un'asserzione positiva si giudica, e
//     l'ambito dedotto si scrive nel rapporto (dedotti).
//   - Una chiave senza ambito si giudica sul file intero, e vuole l'interpretazione completa: un risultato tagliato non
//     si giudica (A1b-22).
//   - Le chiavi che vogliono il testo di un caso o gli altri casi (chiaviNonDirette) e quelle che la regola non decide
//     sono fra i nonFatti.
func lettureDelFile(percorso string, letture []ChiaveAttesa, fd fileDelThread, ins *motorea.InsiemeRegole, nonValutabile string) (differenze, nonFatti []string) {
	differenze, nonFatti, _ = lettureDelFileConAmbiti(percorso, letture, fd, ins, nonValutabile)
	return differenze, nonFatti
}

// asserzioneNegativa: il valore atteso di una chiave dei token dice un'assenza (false, null, la lista vuota): su un
// ambito dedotto non si giudica (R-140), perché sull'unità dedotta l'assenza può essere vera e sul file falsa.
func asserzioneNegativa(v ValoreAtteso) bool {
	switch v.Tipo {
	case TipoNullo:
		return true
	case TipoBooleano:
		return v.Testo == "false"
	case TipoLista:
		return len(v.Elementi) == 0
	}
	return false
}

// lettureDelFileConAmbiti: lettureDelFile, con in più le chiavi giudicate su un ambito dedotto (le chiavi dei token),
// una riga per chiave con l'unità, per il rapporto (R-140).
func lettureDelFileConAmbiti(percorso string, letture []ChiaveAttesa, fd fileDelThread, ins *motorea.InsiemeRegole, nonValutabile string) (differenze, nonFatti, dedotti []string) {
	return lettureDelFileNellaSezione(percorso, "", letture, fd, ins, nonValutabile)
}

// lettureDelFileNellaSezione: lettureDelFileConAmbiti per le voci di una sezione degli attesi, con gli ambiti che il
// piano dà alle chiavi di quella sezione (ambitiDellaSezione: la base dei reali è quella del nome; D-R116 O) e i sinonimi
// delle letture (token_00_conservato è token_conservato: 6.4.9; C-30). Senza la sezione valgono le sole regole delle
// chiavi.
func lettureDelFileNellaSezione(percorso, sezione string, letture []ChiaveAttesa, fd fileDelThread, ins *motorea.InsiemeRegole, nonValutabile string) (differenze, nonFatti, dedotti []string) {
	if len(letture) == 0 {
		return nil, nil, nil
	}
	fermo := nonValutabile
	if fermo == "" && (fd.fi == nil || !fd.valutato || fd.fi.Motivo != "") {
		fermo = "il file non è valutato"
		if fd.fi != nil && fd.fi.Motivo != "" {
			fermo += " (" + fd.fi.Motivo + ")"
		}
	}
	if fermo != "" {
		for _, k := range letture {
			nonFatti = append(nonFatti, percorso+".atteso."+k.Chiave+": "+fermo)
		}
		return nil, nonFatti, nil
	}
	stato := fd.fi.Interpretazione.Stato
	completa := stato == motorea.StatoInterpretazioneCompleta
	// C-04: gli ambiti della sezione valgono solo se il nome è tutto il file; altrimenti le chiavi tornano alle regole
	// della chiave (sezioneAmbiti vuota), e motivoNome si scrive accanto al motivo di A1b-22.
	sezioneAmbiti, motivoNome := sezione, ""
	if len(ambitiDellaSezione[sezione]) > 0 {
		if motivoNome = nomeETuttoIlFile(fd.fi); motivoNome != "" {
			sezioneAmbiti = ""
		}
	}
	motore := motoreDi(ins, fd.cliente)
	scene := map[evidenze.Contesto]*scena{}
	scenaDi := func(ambito evidenze.Contesto, intera bool) *scena {
		chiave := ambito
		if intera {
			chiave = "*"
		}
		if sc := scene[chiave]; sc != nil {
			return sc
		}
		in := fd.fi.Interpretazione
		if !intera {
			in = interpretazioneDellUnita(fd.fi, ambito)
		}
		sc := nuovaScena(evidenze.Selettore{Contesto: ambito}, "", in, motore, fd.cliente)
		for _, k := range letture {
			sc.atteso[chiaveDellaRegola(k.Chiave)] = k.Valore
		}
		scene[chiave] = sc
		return sc
	}
	for _, k := range letture {
		p := percorso + ".atteso." + k.Chiave
		nomeRegola := chiaveDellaRegola(k.Chiave)
		regola, nota := tabellaChiavi[nomeRegola]
		switch {
		case !nota || regola.valuta == nil:
			m := regola.motivo
			if m == "" {
				m = "nessuna regola nella tabella dei casi"
			}
			nonFatti = append(nonFatti, p+": la chiave non si controlla ("+m+")")
			continue
		case chiaviNonDirette[nomeRegola] != "":
			nonFatti = append(nonFatti, p+": "+chiaviNonDirette[nomeRegola])
			continue
		}
		var sc *scena
		ambito, conAmbito := ambitoDi(sezioneAmbiti, k.Chiave)
		dedotto := false
		if chiaviDelToken[nomeRegola] {
			a, motivo := ambitoDelToken(letture, sezioneAmbiti)
			switch {
			case motivo != "":
				nonFatti = append(nonFatti, p+": "+motivo)
				continue
			case asserzioneNegativa(k.Valore):
				nonFatti = append(nonFatti, p+": ambito dedotto, asserzione negativa: sull'unità "+string(a)+
					", dedotta dalle chiavi sorelle, un'assenza può passare a vuoto (T-B6-220, R-140)")
				continue
			}
			ambito, conAmbito, dedotto = a, true, true
		}
		if conAmbito {
			switch {
			case ambito == "":
				nonFatti = append(nonFatti, p+": la chiave ha l'ambito «questo campo», che una voce di file non dice")
				continue
			case ambito != evidenze.ContestoNomeFile && !completa:
				nonFatti = append(nonFatti, p+": interpretazione "+stato+": l'unità "+string(ambito)+" può essere tagliata (A1b-22)")
				continue
			}
			sc = scenaDi(ambito, false)
			if len(sc.letture) == 0 && len(sc.menzioni) == 0 && len(sc.attributi) == 0 {
				nonFatti = append(nonFatti, p+": nessuna lettura sull'unità "+string(ambito)+": la chiave ha un ambito, e sul file intero il confronto non è diretto")
				continue
			}
		} else {
			if !completa {
				m := p + ": interpretazione " + stato + ": un risultato tagliato non si giudica (A1b-22)"
				if _, diSezione := ambitiDellaSezione[sezione][k.Chiave]; diSezione && motivoNome != "" {
					m += "; l'ambito del nome della sezione vale solo se il nome è tutto il file (D-R116 O, C-04): " + motivoNome
				}
				nonFatti = append(nonFatti, m)
				continue
			}
			sc = scenaDi("", true)
		}
		if dedotto {
			dedotti = append(dedotti, p+": giudicata sull'unità "+string(ambito)+", ambito dedotto dalle chiavi sorelle (T-B6-220)")
		}
		switch v := regola.valuta(sc, k.Valore); v.stato {
		case ChiavePassata:
		case ChiaveFallita:
			differenze = append(differenze, fmt.Sprintf("%s: atteso %s, ottenuto %s%s", p, k.Valore.String(), vuotoONo(v.ottenuto), conMotivo(v.motivo)))
		default:
			nonFatti = append(nonFatti, p+": "+v.stato+conMotivo(v.motivo))
		}
	}
	return differenze, nonFatti, dedotti
}

// interpretazioneDellUnita: l'interpretazione di un file ridotta alle letture e agli attributi delle unità con quel
// contesto. L'unità di una lettura è quella del documento del file (UnitaID); se il documento non la porta, il selettore
// della lettura. Un'unità che non si riconosce non entra: meglio una chiave non verificata che un passato falso.
func interpretazioneDellUnita(fi *valut.FileInterpretato, ambito evidenze.Contesto) motorea.Interpretazione {
	unita := map[string]evidenze.Contesto{}
	for _, u := range fi.Documento.Unita {
		unita[u.ID] = u.Selettore.Contesto
	}
	in := fi.Interpretazione
	in.Letture, in.Attributi = nil, nil
	for _, l := range fi.Interpretazione.Letture {
		c, ok := unita[l.UnitaID]
		if !ok {
			c = l.Forma.Selettore.Contesto
		}
		if c == ambito {
			in.Letture = append(in.Letture, l)
		}
	}
	for _, a := range fi.Interpretazione.Attributi {
		if c, ok := unita[a.UnitaID]; ok && c == ambito {
			in.Attributi = append(in.Attributi, a)
		}
	}
	return in
}

func conMotivo(m string) string {
	if m == "" {
		return ""
	}
	return " (" + m + ")"
}

// ---- i casi e la fotografia (R-103; T-B6-74, T-B6-112) ----

// controlloCasiNellaFoto: il file dei casi contro la fotografia, sempre (non solo per lo scenario, non solo con
// -attesi). Un thread di un caso che la fotografia non ha, un messaggio di un segmento che non c'è o è di un altro
// thread, un messaggio fuori RFQ di un caso che la copia non ha (mancantiCopia, da Carica): il file dei casi contraddice
// i dati, ed è una differenza (uscita 1). Con -thread un caso di un thread non scelto non si guarda. Con gli export i
// messaggi che mancano mancano per costruzione (solo i messaggi in entrata: mancantiExport, e i segmenti su un messaggio
// non esportato): sono parti non verificabili (uscita 3).
func controlloCasiNellaFoto(in valut.Ingressi, letti bool, ix indiceFoto, cc contestoCorsa, mancantiCopia, mancantiExport []uuid.UUID) esitoControllo {
	e := esitoControllo{nome: ControlloCasiFoto, eseguito: letti}
	if !letti {
		e.motivo = "il file dei casi non è stato letto"
		return e
	}
	casoDi := map[uuid.UUID]string{}
	for _, c := range in.Casi {
		if c.ThreadID == nil {
			for _, m := range c.Messaggi {
				casoDi[m] = c.ID
			}
			continue
		}
		th := *c.ThreadID
		if _, ok := ix.thread[th]; !ok {
			if cc.parziale && !cc.scelti[th] {
				continue
			}
			e.differenze = append(e.differenze, fmt.Sprintf("caso %s: il thread %s non è nella fotografia", c.ID, th))
			continue
		}
		for i, sg := range c.Segmenti {
			di, ok := ix.messaggi[sg.MessaggioID]
			switch {
			case !ok && cc.export:
				e.nonFatti = append(e.nonFatti, fmt.Sprintf("caso %s, segmenti[%d]: il messaggio non è negli export (solo i messaggi in entrata)", c.ID, i))
			case !ok:
				e.differenze = append(e.differenze, fmt.Sprintf("caso %s, segmenti[%d]: il messaggio %s non è nella fotografia", c.ID, i, sg.MessaggioID))
			case di != th:
				e.differenze = append(e.differenze, fmt.Sprintf("caso %s, segmenti[%d]: il messaggio %s non è del thread del caso", c.ID, i, sg.MessaggioID))
			}
		}
	}
	for _, m := range mancantiCopia {
		e.differenze = append(e.differenze, fmt.Sprintf("caso %s: il messaggio fuori RFQ %s non è nella copia", casoDi[m], m))
	}
	for _, m := range mancantiExport {
		e.nonFatti = append(e.nonFatti, fmt.Sprintf("caso %s: il messaggio fuori RFQ %s non è negli export (solo i messaggi in entrata)", casoDi[m], m))
	}
	return e
}

// ---- i clienti degli export (T-B6-104) ----

// threadSenzaCliente: con gli export, i thread il cui cliente non ha la ragione sociale nell'export dei clienti
// (l'export di prima della ricreazione del DB non ha gli UUID di oggi). Il motore non li valuta (ragione sociale
// discorde): è un limite degli ingressi, non del motore, e ciò che dipende dalla loro valutazione non si verifica.
func threadSenzaCliente(f fotorfq.Fotografia) map[uuid.UUID]string {
	if f.Origine != fotorfq.OrigineExport {
		return nil
	}
	conRS := map[uuid.UUID]bool{}
	for _, c := range f.Clienti {
		conRS[c.ID] = true
	}
	out := map[uuid.UUID]string{}
	for _, t := range f.Thread {
		if !conRS[t.ClienteID] {
			out[t.ID] = "il cliente del thread non ha la ragione sociale negli export: il thread non si valuta (T-B6-104)"
		}
	}
	return out
}

// controlloClientiExport: il controllo dei clienti degli export. NON ESEGUITO se un thread esportato ha un cliente che
// l'export dei clienti non contiene: quei thread non si valutano, e i controlli che ne dipendono hanno le loro parti fra
// i nonFatti (T-B6-104). R115 A, ratificata dall'utente il 07/10 con un vincolo: un export nuovo non si mescola al
// campione del 02/10, che resta con questo limite dichiarato.
//
// D-R115 A, decisa dall'utente l'08/10: il limite è accettato. Nel campione del 02/10 l'export dei clienti è di prima
// della ricreazione del DB del 01/10, con UUID diversi da quelli degli altri export e del dump; non è un difetto del
// motore A né delle migrazioni (la causa sta nella motivazione della classe, classi.go, con la fonte: il runner non la
// può osservare). Il motivo della corsa registra il fatto osservato, la non coerenza degli identificativi, con il numero
// dei thread; il controllo è fuori perimetro, ma lo stato resta NON ESEGUITO e l'uscita resta 3: l'esclusione motivata
// non trasforma il 3 in un successo. Gli export non si toccano, nessun collegamento fra UUID si crea, gli attesi non
// cambiano; gli altri controlli eseguibili restano quelli di prima, con le loro parti non verificate per quei thread.
func controlloClientiExport(f fotorfq.Fotografia, senza map[uuid.UUID]string) esitoControllo {
	e := esitoControllo{nome: ControlloClientiExport, eseguito: true}
	if len(senza) > 0 {
		e.eseguito = false
		e.motivo = fmt.Sprintf("identificativi dei clienti non coerenti fra gli export: %d thread esportati su %d hanno un cliente che l'export dei clienti non contiene, e non si valutano (T-B6-104); limite accettato del campione, fuori perimetro, l'uscita resta 3 (D-R115 A)", len(senza), len(f.Thread))
	}
	return e
}

// ---- la copia del manifest (R-106, R-107; T-B6-105) ----

// controllaCopia: la copia raggiunta contro la sezione copia del manifest (6.4.9, passo 3; 6.6.2). Le decisioni sono qui,
// pure, e le prova la L1:
//   - un ruolo, un database o uno schema diversi dal manifest: la copia non è quella del manifest, NON ESEGUITO, e la
//     corsa si ferma prima di Carica (fermo);
//   - un campo che il manifest non dichiara (ruolo, database, schema, tabelle escluse, sentinelle): una parte non
//     eseguita, con il motivo «il manifest non dichiara …»; la corsa continua, ma il controllo è NON ESEGUITO;
//   - le sentinelle dichiarate: il runner non ha una query per contarle (T-B0-36). Con R117 b, ratificata e ampliata
//     dall'utente il 07/10 (domande-a1c.md), le verifica A1c-L4D-01 (testutil.PoolDump) sull'ambiente della copia
//     intatta, con le impronte di contenuto oltre ai conteggi (B6b, lato QA: T-B6-219): la riga è «delegata», un
//     obbligatorio esterno con la prova e l'ambiente nel motivo, non NON ESEGUITO, e non decide l'uscita. La delega vale
//     solo per quell'ambiente: con un database diverso dal manifest la corsa si è già fermata; se il manifest non
//     dichiara il database, l'ambiente non si identifica e la riga resta NON ESEGUITA.
//
// ultimaMigrazione: la versione dell'ultima migrazione incorporata nel binario (-1 se non si legge).
func controllaCopia(attesa dataset.CopiaAttesa, col migrazioni.Collegamento, ultimaMigrazione int) (copia esitoControllo, fermo bool, sentinelle *esitoControllo) {
	copia = esitoControllo{nome: ControlloCopia, eseguito: true}
	var diversi []string
	switch {
	case attesa.Ruolo == "":
		copia.nonFatti = append(copia.nonFatti, "il manifest non dichiara il ruolo della copia")
	case attesa.Ruolo != col.Utente:
		diversi = append(diversi, "il ruolo non è quello del manifest")
	}
	switch {
	case attesa.Database == "":
		copia.nonFatti = append(copia.nonFatti, "il manifest non dichiara il database della copia")
	case attesa.Database != col.Database:
		diversi = append(diversi, "il database non è quello del manifest")
	}
	switch {
	case attesa.Schema == 0:
		copia.nonFatti = append(copia.nonFatti, "il manifest non dichiara lo schema della copia")
	case attesa.Schema != ultimaMigrazione:
		diversi = append(diversi, "lo schema del binario non è quello del manifest")
	}
	if len(attesa.Escluse) == 0 {
		copia.nonFatti = append(copia.nonFatti, "il manifest non dichiara le tabelle escluse: che non si leggano non si controlla")
	}
	switch {
	case len(attesa.Sentinelle) == 0:
		copia.nonFatti = append(copia.nonFatti, "il manifest non dichiara le sentinelle della copia")
	case attesa.Database == "":
		sentinelle = &esitoControllo{nome: ControlloSentinelle,
			motivo: "il manifest non dichiara il database della copia: la delega ad A1c-L4D-01 vale solo per l'ambiente della copia intatta (R117 b)"}
	default:
		sentinelle = &esitoControllo{nome: ControlloSentinelle, delegato: true,
			motivo: fmt.Sprintf("sentinelle e impronte di contenuto verificate fuori dal runner da A1c-L4D-01 (testutil.PoolDump), sull'ambiente della copia intatta: il database %q della sezione copia del manifest; il runner non le conta e non le calcola (T-B0-36: nessun SQL nuovo) e la riga non decide l'uscita (R117 b)", attesa.Database)}
	}
	if len(diversi) > 0 {
		return esitoControllo{nome: ControlloCopia, motivo: "la copia non è quella del manifest: " + strings.Join(diversi, "; ")}, true, nil
	}
	return copia, false, sentinelle
}

// solaLetturaDi: la sezione sola_lettura del rapporto dai controlli del collegamento. Le tabelle escluse non leggibili
// si contano solo se il controllo delle escluse c'è stato (ControllaSolaLettura si ferma al primo controllo che non
// passa, e le escluse sono l'ultimo): altrimenti il campo resta vuoto, «non controllate» (R-107).
func solaLetturaDi(col migrazioni.Collegamento, err error, escluse []string) *SolaLettura {
	s := &SolaLettura{Ruolo: col.Utente, Database: col.Database, ScritturaPossibile: col.Scrive, Escluse: append([]string(nil), escluse...)}
	if err == nil || len(col.EscluseLeggibili) > 0 {
		n := len(escluse) - len(col.EscluseLeggibili)
		s.EscluseNonLeggibili = &n
	}
	return s
}

// controlloConfronto: le impronte dei confronti dei thread; una che non è lunga 64 è una differenza (R-48).
func controlloConfronto(impronte []string) esitoControllo {
	e := esitoControllo{nome: "confronto", eseguito: true}
	for _, h := range impronte {
		if len(h) != 64 {
			e.differenze = append(e.differenze, "impronta del confronto non calcolata (R-48)")
		}
	}
	if len(e.differenze) > 0 {
		e.motivo = "impronta del confronto non calcolata (R-48)"
	}
	return e
}
