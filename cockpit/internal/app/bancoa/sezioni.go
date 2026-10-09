package bancoa

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/fotorfq"
	"promatec/cockpit/internal/core/inbox/classificazione/motorea"
	valut "promatec/cockpit/internal/core/valutazione"
)

// sezioni.go: le sezioni degli attesi lette da LeggiSezioni (attesi.go) e la loro traduzione nei DTO di atteso di
// confronto (piano 6.4.9, «Traduzione degli attesi» e «Che cosa fa il runner con ogni sezione degli attesi»; R19 a-b,
// C-30, R25, R47 a, P-10). Lo fa il codice, con i nomi neutri: nessun valore degli attesi sta qui. Il motore non vede
// mai l'atteso (R2): lo vede solo confronto, nel banco.

// SezioniAttesi: le sezioni degli attesi che servono alle modalità dsn ed exports.
//   - Scenario: lo scenario della richiesta inoltrata (scenario_inoltro); nil se la sezione è vuota.
//   - Baseline, Reali, DaRivedere: le voci di file della baseline delle decisioni, dei file reali d'archivio e della C5.
//   - Albero, Integrazione: gli ID e gli sha256 della conferma dell'albero e dei casi d'integrazione (controllo n.2).
//   - ThreadDaRivedere, ClienteDaRivedere: il thread della sezione C5 e il suo cliente, se è scritto con l'UUID
//     (controllo n.2; T-B6c-08).
//   - DecisoDaRivedere: chi ha deciso la C5 della sezione, con la sigla o l'UUID di un utente (controllo n.2; C-05,
//     T-B6c-15). Campo facoltativo: assente, il JSON resta quello di prima.
//   - RigheAlbero: i percorsi delle righe della conferma dell'albero, che il runner non verifica: stanno nella sezione
//     informativa del rapporto righe_conferma_albero (R-109; D-R116 I, decisa dall'utente l'08/10).
//   - Fonti: gli sha256 delle fonti (controllo n.3, con -exports).
//   - Riportate: le sezioni descrittive e le note, riga per riga, per il rapporto.
//   - NonTradotte: le chiavi che il runner non conosce (controllo n.5); Errori: i valori che non si leggono.
//   - ChiaviLibere: nelle sezioni a forma libera (casi_integrazione, conferma_albero, fonti) le chiavi che non sono ID
//     né sha256, e le chiavi descrittive delle sezioni e delle voci (T-B6c-04, T-B6c-06, T-B6c-08), con il percorso: un
//     elenco informativo «chiavi libere non verificate», che non cambia l'uscita (R-109).
type SezioniAttesi struct {
	Scenario          *ScenarioAtteso `json:"scenario,omitempty"`
	Baseline          []VoceAttesa    `json:"baseline,omitempty"`
	Reali             []VoceAttesa    `json:"reali,omitempty"`
	DaRivedere        []VoceAttesa    `json:"da_rivedere,omitempty"`
	Albero            []IDAtteso      `json:"albero,omitempty"`
	Integrazione      []IDAtteso      `json:"integrazione,omitempty"`
	ThreadDaRivedere  *IDAtteso       `json:"thread_da_rivedere,omitempty"`
	ClienteDaRivedere *IDAtteso       `json:"cliente_da_rivedere,omitempty"`
	DecisoDaRivedere  *IDAtteso       `json:"deciso_da_rivedere,omitempty"`
	RigheAlbero       []string        `json:"righe_albero,omitempty"`
	Fonti             []string        `json:"fonti,omitempty"`
	Riportate         []RigaRiportata `json:"riportate,omitempty"`
	NonTradotte       []string        `json:"non_tradotte,omitempty"`
	Errori            []string        `json:"errori,omitempty"`
	ChiaviLibere      []string        `json:"chiavi_libere,omitempty"`
}

// ScenarioAtteso: la sezione dello scenario. ThreadID, MessaggioID, AutoritaTarget e ProdottiConfermatiDB sono gli
// ingressi che il controllo n.1 confronta con il file dei casi (T-B0-15; R29 a). FonteQuantita: quella della sezione,
// che vale per i prodotti attesi che non la scrivono (T-B6c-05).
type ScenarioAtteso struct {
	ThreadID             *uuid.UUID         `json:"thread_id,omitempty"`
	MessaggioID          *uuid.UUID         `json:"messaggio_id,omitempty"`
	AutoritaTarget       string             `json:"autorita_target,omitempty"`
	ProdottiConfermatiDB *bool              `json:"prodotti_confermati_db,omitempty"`
	FonteQuantita        string             `json:"fonte_quantita,omitempty"`
	Prodotti             []ProdottoScenario `json:"prodotti,omitempty"`
	Conteggi             []ConteggioAtteso  `json:"conteggi,omitempty"`
	File                 []VoceAttesa       `json:"file,omitempty"`
}

// ProdottoScenario: un prodotto atteso dello scenario, com'è scritto (6.4.9: originale, base, fase, quantità,
// evidenza in una cella).
type ProdottoScenario struct {
	Percorso      string `json:"percorso"`
	Originale     string `json:"originale,omitempty"`
	Base          string `json:"base"`
	Fase          string `json:"fase,omitempty"`
	Quantita      *int   `json:"quantita,omitempty"`
	FonteQuantita string `json:"fonte_quantita,omitempty"`
}

// ConteggioAtteso: un conteggio che gli attesi dichiarano, per nome.
type ConteggioAtteso struct {
	Nome   string `json:"nome"`
	Valore int    `json:"valore"`
}

// VoceAttesa: una voce di file di una sezione, com'è scritta, prima della traduzione.
//   - Sezione: il nome neutro (confronto.Sezione*); Percorso: dove sta negli attesi.
//   - AllegatoID, Sha256, NomeFile: il file (il legame si fa per AllegatoID; lo sha256 si controlla, n.2).
//   - ThreadID: il thread del file, se la voce lo scrive (il n.2 lo confronta con il thread dell'allegato).
//   - Atteso, AttesoStato, TargetBase, Radici, Autorita: la collocazione attesa dello scenario (radice | figlio |
//     fuori), lo stato atteso (candidato_scenario | fuori_scenario), la base del target, le basi delle radici,
//     l'autorità. TargetBaseScritto e RadiciScritte dicono se la chiave c'era. Per la C5, Atteso è l'esito atteso del
//     file, se la voce lo scrive come valore (T-B6c-07).
//   - StatoAtteso: definito | riservato; DipendeDa: la domanda aperta da cui il caso dipende.
//   - Letture: la mappa «atteso» delle letture del file (baseline, reali; la C5, se la scrive come mappa), con le
//     chiavi della tabella dei casi.
//   - Proposta, Documento, Export: le righe attese di proposta e di documento, e quella dell'export (la chiave
//     nell_export_proposte della C5), campo per campo.
//   - Invarianti: le chiavi booleane della voce; EsitoConTarget: l'esito con il target dei file reali, com'è scritto.
//   - Riportate: note, etichette, anomalie, l'esito con il target e il codice dei file dello scenario, solo per il
//     rapporto.
type VoceAttesa struct {
	Sezione               string             `json:"sezione"`
	Percorso              string             `json:"percorso"`
	ID                    string             `json:"id,omitempty"`
	AllegatoID            *uuid.UUID         `json:"allegato_id,omitempty"`
	ThreadID              *uuid.UUID         `json:"thread_id,omitempty"`
	Sha256                string             `json:"sha256,omitempty"`
	NomeFile              string             `json:"nome_file,omitempty"`
	Atteso                string             `json:"atteso,omitempty"`
	AttesoStato           string             `json:"atteso_stato,omitempty"`
	TargetBase            string             `json:"target_base,omitempty"`
	TargetBaseScritto     bool               `json:"target_base_scritto"`
	Radici                []string           `json:"radici,omitempty"`
	RadiciScritte         bool               `json:"radici_scritte"`
	Autorita              string             `json:"autorita,omitempty"`
	StatoAtteso           string             `json:"stato_atteso,omitempty"`
	DipendeDa             string             `json:"dipende_da,omitempty"`
	Letture               []ChiaveAttesa     `json:"letture,omitempty"`
	Proposta              []CampoAtteso      `json:"proposta,omitempty"`
	Documento             []CampoAtteso      `json:"documento,omitempty"`
	Export                []CampoAtteso      `json:"export,omitempty"`
	Invarianti            []InvarianteAttesa `json:"invarianti,omitempty"`
	EsitoConTarget        string             `json:"esito_con_target,omitempty"`
	EsitoConTargetScritto bool               `json:"esito_con_target_scritto"`
	Riportate             []string           `json:"riportate,omitempty"`
}

// CampoAtteso: un campo di una riga attesa, com'è scritto; Nullo: il valore è null.
type CampoAtteso struct {
	Nome   string `json:"nome"`
	Valore string `json:"valore,omitempty"`
	Nullo  bool   `json:"nullo"`
}

// InvarianteAttesa: una chiave booleana di una voce.
type InvarianteAttesa struct {
	Nome   string `json:"nome"`
	Valore bool   `json:"valore"`
}

// IDAtteso: un ID o uno sha256 di una sezione letta senza interpretarla, con la chiave e il percorso.
type IDAtteso struct {
	Percorso string `json:"percorso"`
	Chiave   string `json:"chiave"`
	Valore   string `json:"valore"`
}

// RigaRiportata: una riga di una sezione descrittiva.
type RigaRiportata struct {
	Percorso string `json:"percorso"`
	Valore   string `json:"valore"`
}

// invariante: il valore di una chiave booleana della voce, e se c'è.
func (v VoceAttesa) invariante(nome string) (bool, bool) {
	for _, x := range v.Invarianti {
		if x.Nome == nome {
			return x.Valore, true
		}
	}
	return false, false
}

// lettura: il valore di una chiave della mappa delle letture, come testo, e se c'è.
func (v VoceAttesa) lettura(nome string) (string, bool) {
	for _, k := range v.Letture {
		if k.Chiave == nome {
			return k.Valore.Testo, true
		}
	}
	return "", false
}

// campo: il campo di una riga attesa, e se c'è.
func campo(riga []CampoAtteso, nome string) (CampoAtteso, bool) {
	for _, c := range riga {
		if c.Nome == nome {
			return c, true
		}
	}
	return CampoAtteso{}, false
}

// ---- l'indice della fotografia, per legare gli attesi ai file ----

// rifAllegato: dove sta un allegato della fotografia.
type rifAllegato struct {
	allegato fotorfq.Allegato
	thread   uuid.UUID // uuid.Nil per un allegato di un messaggio fuori RFQ
	fuori    bool
}

// indiceFoto: gli allegati, i messaggi, i componenti, i documenti e gli sha256 della fotografia, per ID.
type indiceFoto struct {
	allegati   map[uuid.UUID]rifAllegato
	perSha     map[string][]uuid.UUID
	thread     map[uuid.UUID]int // indice in Fotografia.Thread
	messaggi   map[uuid.UUID]uuid.UUID
	componenti map[uuid.UUID]bool
	documenti  map[uuid.UUID]bool
	sha        map[string]bool
}

func indicizza(f fotorfq.Fotografia) indiceFoto {
	ix := indiceFoto{allegati: map[uuid.UUID]rifAllegato{}, perSha: map[string][]uuid.UUID{}, thread: map[uuid.UUID]int{},
		messaggi: map[uuid.UUID]uuid.UUID{}, componenti: map[uuid.UUID]bool{}, documenti: map[uuid.UUID]bool{}, sha: map[string]bool{}}
	allegato := func(a fotorfq.Allegato, thread uuid.UUID, fuori bool) {
		ix.allegati[a.ID] = rifAllegato{allegato: a, thread: thread, fuori: fuori}
		if a.Sha256 != nil {
			s := strings.ToLower(*a.Sha256)
			ix.perSha[s] = append(ix.perSha[s], a.ID)
			ix.sha[s] = true
		}
	}
	for i, t := range f.Thread {
		ix.thread[t.ID] = i
		for _, m := range t.Messaggi {
			ix.messaggi[m.ID] = t.ID
		}
		for _, a := range t.Allegati {
			allegato(a, t.ID, false)
		}
		for _, c := range t.Componenti {
			ix.componenti[c.ID] = true
		}
		for _, d := range t.Documenti {
			ix.documenti[d.ID] = true
			ix.sha[strings.ToLower(d.Sha256)] = true
		}
		for s := range t.Fatti {
			ix.sha[strings.ToLower(s)] = true
		}
	}
	for _, m := range f.FuoriRFQ {
		ix.messaggi[m.Messaggio.ID] = uuid.Nil
		for _, a := range m.Allegati {
			allegato(a, uuid.Nil, true)
		}
	}
	return ix
}

// ---- la traduzione ----

// voceTradotta: una voce degli attesi legata al suo file e al suo thread, con il DTO di confronto.
type voceTradotta struct {
	voce     VoceAttesa
	thread   uuid.UUID
	cliente  uuid.UUID
	file     confronto.FileAtteso
	risolta  bool   // il file c'è nella fotografia
	motivoNR string // perché non si risolve
}

// traduzione: le voci tradotte, gli Atteso per thread, gli errori di traduzione (D8, R-51, le radici vuote, i campi di
// una riga che quella riga non ha) e, per il n.2, le voci che non si risolvono nella fotografia, i file in due sezioni e
// i nomi dei file diversi da quelli della fotografia.
type traduzione struct {
	voci       []voceTradotta
	perThread  map[uuid.UUID]*confronto.Atteso
	errori     []string
	nonRisolte []string
	doppie     []string
	nomi       []string
}

// traduci: le sezioni degli attesi nei DTO di confronto, thread per thread (6.4.9, «Traduzione degli attesi»):
//   - atteso radice | figlio | fuori → la collocazione attesa; candidato_scenario → candidato_unico con l'autorità
//     scenario; fuori_scenario → fuori richiesta (confronto.StatoAtteso*); radici → le basi delle radici raggiungibili;
//     l'autorità del target dello scenario scritta «scenario_richiesta_inoltrata» → scenario (traduciAutorita);
//   - la sezione va in FileAtteso.Sezione con il nome neutro (P-10);
//   - per la baseline T è la base del componente deciso (il controllo (3) della baseline), letta dalla riga attesa con
//     la grammatica del cliente; senza una riga che si legga, la base attesa delle letture;
//   - i file reali sono riservati finché l'esito con il target non è scritto e tradotto (R21 c A; M-11, PA-07);
//   - la C5 è da_rivedere (P-10).
//
// Gli errori di traduzione (una voce radice o figlio con T vuoto, un figlio senza radici, una radice vuota, un valore
// fuori elenco: D8, R-51) non producono la voce per confronto: sono differenze del runner, mai «nessun ancoraggio
// atteso». Una voce il cui file non è nella fotografia, o un file in due sezioni, va al controllo n.2.
func traduci(s SezioniAttesi, f fotorfq.Fotografia, ix indiceFoto, ins *motorea.InsiemeRegole) traduzione {
	tr := traduzione{perThread: map[uuid.UUID]*confronto.Atteso{}}
	var tutte []VoceAttesa
	if s.Scenario != nil {
		tutte = append(tutte, s.Scenario.File...)
	}
	tutte = append(tutte, s.Baseline...)
	tutte = append(tutte, s.Reali...)
	tutte = append(tutte, s.DaRivedere...)

	sezioniDi := map[uuid.UUID][]string{}
	for _, v := range tutte {
		vt := voceTradotta{voce: v}
		id, motivo := risolviFile(v, ix)
		if motivo != "" {
			vt.motivoNR = motivo
			tr.nonRisolte = append(tr.nonRisolte, v.Percorso+": "+motivo)
		} else {
			ra := ix.allegati[id]
			switch {
			case ra.fuori:
				vt.motivoNR = "il file è di un messaggio fuori RFQ: non ha un thread da confrontare"
				tr.nonRisolte = append(tr.nonRisolte, v.Percorso+": "+vt.motivoNR)
			case v.Sezione == confronto.SezioneScenario && s.Scenario.ThreadID != nil && ra.thread != *s.Scenario.ThreadID:
				vt.motivoNR = "il file non è del thread dello scenario"
				tr.nonRisolte = append(tr.nonRisolte, v.Percorso+": "+vt.motivoNR)
			default:
				vt.risolta, vt.thread = true, ra.thread
				vt.cliente = f.Thread[ix.thread[ra.thread]].ClienteID
				sezioniDi[id] = append(sezioniDi[id], v.Sezione+" ("+v.Percorso+")")
			}
			// nome_file si verifica: il legame è per allegato, ma un nome diverso dice che gli attesi e i dati non
			// parlano dello stesso file (la regola delle chiavi accettate). Così thread_id della voce (B6c): è il thread
			// dell'allegato nella fotografia, non solo un thread che si risolve (un allegato fuori RFQ non ne ha;
			// T-B6c-14).
			if v.NomeFile != "" && ra.allegato.NomeFile != v.NomeFile {
				tr.nomi = append(tr.nomi, v.Percorso+".nome_file: non è il nome dell'allegato nella fotografia")
			}
			if v.ThreadID != nil && (ra.fuori || ra.thread != *v.ThreadID) {
				tr.nomi = append(tr.nomi, v.Percorso+".thread_id: non è il thread dell'allegato nella fotografia")
			}
		}
		var tb string
		if v.Sezione == confronto.SezioneBaseline {
			tb = baseDelDeciso(v, motoreDi(ins, vt.cliente))
		}
		fa, errori := fileAttesoDi(v, id, tb)
		vt.file = fa
		for _, e := range errori {
			tr.errori = append(tr.errori, v.Percorso+": "+e)
		}
		if len(errori) > 0 {
			vt.risolta = false
			if vt.motivoNR == "" {
				vt.motivoNR = "errore di traduzione"
			}
		}
		tr.voci = append(tr.voci, vt)
	}
	ids := make([]uuid.UUID, 0, len(sezioniDi))
	for id := range sezioniDi {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	for _, id := range ids {
		if len(sezioniDi[id]) > 1 {
			tr.doppie = append(tr.doppie, fmt.Sprintf("allegato %s in %s", id, strings.Join(sezioniDi[id], ", ")))
		}
	}
	for _, vt := range tr.voci {
		if !vt.risolta {
			continue
		}
		a := tr.perThread[vt.thread]
		if a == nil {
			a = &confronto.Atteso{ThreadID: vt.thread}
			tr.perThread[vt.thread] = a
		}
		a.File = append(a.File, vt.file)
	}
	if sc := s.Scenario; sc != nil && sc.ThreadID != nil {
		if _, ok := ix.thread[*sc.ThreadID]; ok {
			a := tr.perThread[*sc.ThreadID]
			if a == nil {
				a = &confronto.Atteso{ThreadID: *sc.ThreadID}
				tr.perThread[*sc.ThreadID] = a
			}
			for _, p := range sc.Prodotti {
				pa, errore := prodottoAttesoDi(p)
				if errore != "" {
					tr.errori = append(tr.errori, p.Percorso+": "+errore)
					continue
				}
				a.Prodotti = append(a.Prodotti, pa)
			}
		}
	}
	return tr
}

// risolviFile: l'allegato di una voce. Per AllegatoID, con lo sha256 controllato se la voce lo scrive; con il solo
// sha256, se un allegato solo della fotografia ha quel contenuto.
func risolviFile(v VoceAttesa, ix indiceFoto) (uuid.UUID, string) {
	if v.AllegatoID != nil {
		ra, ok := ix.allegati[*v.AllegatoID]
		if !ok {
			return *v.AllegatoID, "l'allegato non è nella fotografia"
		}
		if v.Sha256 != "" && (ra.allegato.Sha256 == nil || strings.ToLower(*ra.allegato.Sha256) != v.Sha256) {
			return *v.AllegatoID, "lo sha256 della voce non è quello dell'allegato"
		}
		return *v.AllegatoID, ""
	}
	if v.Sha256 == "" {
		return uuid.Nil, "la voce non dice né l'allegato né lo sha256"
	}
	switch ids := ix.perSha[v.Sha256]; len(ids) {
	case 0:
		return uuid.Nil, "nessun allegato della fotografia ha lo sha256 della voce"
	case 1:
		return ids[0], ""
	default:
		return uuid.Nil, fmt.Sprintf("lo sha256 della voce è di %d allegati: senza l'allegato la voce non si lega", len(ids))
	}
}

// motoreDi: il motore della grammatica di un cliente, se c'è (senza il controllo della ragione sociale: serve solo a
// leggere il codice deciso degli attesi, come fa valutazione con LeggiCodiceRegistrato).
func motoreDi(ins *motorea.InsiemeRegole, cliente uuid.UUID) *motorea.Motore {
	if ins == nil {
		return nil
	}
	return ins.Motori[cliente]
}

// baseDelDeciso: T per una voce della baseline (il controllo (3)): la base del codice deciso della riga attesa del
// documento, o della proposta, letta con la grammatica del cliente (valutazione.LeggiCodiceRegistrato, R31 c); se non
// si legge, la base attesa delle letture del file.
func baseDelDeciso(v VoceAttesa, m *motorea.Motore) string {
	for _, riga := range [][]CampoAtteso{v.Documento, v.Proposta} {
		if c, ok := campo(riga, "codice"); ok && !c.Nullo && c.Valore != "" && m != nil {
			if l := valut.LeggiCodiceRegistrato(m, c.Valore); l.Leggibile && l.Base != "" {
				return l.Base
			}
		}
	}
	b, _ := v.lettura("base")
	return b
}

// traduciAutorita: l'autorità attesa nei valori del motore (C-20; 6.4.9, controllo n.1: «scenario_richiesta_inoltrata
// → scenario»). Un valore fuori elenco non si traduce.
func traduciAutorita(s string) (string, bool) {
	switch s {
	case "":
		return "", true
	case "scenario_richiesta_inoltrata", "scenario":
		return "scenario", true
	case "proposta", "confermata":
		return s, true
	}
	return "", false
}

// fileAttesoDi: una voce come confronto.FileAtteso, con gli errori di traduzione.
func fileAttesoDi(v VoceAttesa, allegato uuid.UUID, targetBaseline string) (confronto.FileAtteso, []string) {
	var errori []string
	fa := confronto.FileAtteso{AllegatoID: allegato, Sha256: v.Sha256, Sezione: v.Sezione, DipendeDa: v.DipendeDa}
	switch v.StatoAtteso {
	case "", StatoAttesoDefinito:
	case StatoAttesoRiservato:
		fa.Riservato = "stato_atteso riservato negli attesi"
	default:
		errori = append(errori, fmt.Sprintf("stato_atteso %q fuori elenco (%s, %s)", v.StatoAtteso, StatoAttesoDefinito, StatoAttesoRiservato))
	}
	if len(v.Invarianti) > 0 {
		fa.Invarianti = map[string]bool{}
		for _, x := range v.Invarianti {
			fa.Invarianti[x.Nome] = x.Valore
		}
	}
	fa.BaseLetta, _ = v.lettura("base")
	errori = append(errori, campiDellaRiga(v)...)
	if d, err := decisioneDi(v); err != "" {
		errori = append(errori, err)
	} else {
		fa.Decisione = d
	}
	switch v.Sezione {
	case confronto.SezioneScenario:
		switch v.Atteso {
		case confronto.AttesoRadice, confronto.AttesoFiglio, confronto.AttesoFuori:
		case "":
			if v.AttesoStato != confronto.StatoAttesoFuoriScenario {
				errori = append(errori, "la voce non dice la collocazione attesa (atteso)")
			}
		default:
			errori = append(errori, fmt.Sprintf("atteso %q fuori elenco (radice, figlio, fuori)", v.Atteso))
		}
		fa.Atteso = v.Atteso
		switch v.AttesoStato {
		case "", confronto.StatoAttesoCandidatoScenario, confronto.StatoAttesoFuoriScenario:
			fa.StatoAtteso = v.AttesoStato
		default:
			errori = append(errori, fmt.Sprintf("atteso_stato %q fuori elenco (candidato_scenario, fuori_scenario)", v.AttesoStato))
		}
		if a, ok := traduciAutorita(v.Autorita); ok {
			fa.Autorita = a
		} else {
			errori = append(errori, fmt.Sprintf("autorita %q fuori elenco", v.Autorita))
		}
		fa.TargetBase = v.TargetBase
		fa.Radici = append([]string(nil), v.Radici...)
		errori = append(errori, controllaCollocazione(v)...)
	case confronto.SezioneBaseline:
		fa.TargetBase = targetBaseline
		if fa.TargetBase == "" {
			errori = append(errori, "la base del componente deciso non si ricava: né dalla riga attesa né da atteso.base")
		}
		if fa.BaseLetta == "" {
			errori = append(errori, "la voce non dice atteso.base: il controllo (2) della baseline non ha un valore")
		}
		// decisioni_invariate è il controllo (1) della baseline (6.4.9): vero, lo verificano le righe; falso, la voce si
		// contraddice, perché la baseline è fatta di decisioni preservate (T-B6c-01).
		if b, ok := v.invariante("decisioni_invariate"); ok && !b {
			errori = append(errori, "atteso.decisioni_invariate falso: una voce della baseline è una decisione preservata (6.4.9, controllo (1))")
		}
	case confronto.SezioneReali:
		if fa.Riservato == "" {
			if v.EsitoConTargetScritto {
				fa.Riservato = "esito_con_target scritto ma non tradotto dal runner: resta riservato (R21 c A)"
			} else {
				fa.Riservato = "i target ci sono: l'esito con il target lo scrive l'utente (R21 c A; M-11, PA-07)"
			}
		}
	case confronto.SezioneDaRivedere:
		// L'atteso della C5 scritto come valore è l'esito del file: la C5 è da_rivedere (P-10; 6.4.9, la C5 nel
		// runner), e un altro valore contraddice la sezione (T-B6c-07).
		if v.Atteso != "" && v.Atteso != string(confronto.EsitoDaRivedere) {
			errori = append(errori, fmt.Sprintf("atteso %q: un file della C5 è %s (P-10)", v.Atteso, confronto.EsitoDaRivedere))
		}
	}
	return fa, errori
}

// I campi che una riga attesa può portare, perché la riga della fotografia li ha (fotorfq.PropostaAttuale e
// fotorfq.DocumentoConfermato): la proposta (anche la riga dell'export della C5) e il documento. Da B6c (T-B6c-02) la
// proposta ha anche il suo ID; il documento il thread, l'estensione e chi l'ha confermato, e la provenienza: l'allegato
// (uno di DocumentoConfermato.Allegati) e il messaggio (quello di un allegato del documento nella fotografia).
var (
	campiProposta  = []string{"codice", "rev", "componente_id", "stato", "fonte", "tipo", "tipo_proposto", "deciso_il", "deciso_da", "proposta_id"}
	campiDocumento = []string{"codice", "rev", "componente_id", "documento_id", "tipo", "sostituito_da", "confermato_il", "sha256", "nome_file",
		"allegato_id", "messaggio_id", "thread_id", "estensione", "confermato_da"}
)

// campiDellaRiga (R-102): un campo che la riga attesa non può avere, perché quella riga della fotografia non lo porta
// (per esempio confermato_il o sostituito_da in una proposta), è un errore di traduzione: altrimenti non si confronta
// con niente e passa in silenzio.
func campiDellaRiga(v VoceAttesa) []string {
	var errori []string
	for _, r := range []struct {
		nome    string
		riga    []CampoAtteso
		ammessi []string
	}{{"decisione_proposta", v.Proposta, campiProposta}, {"nell_export_proposte", v.Export, campiProposta}, {"decisione_documento", v.Documento, campiDocumento}} {
		for _, c := range r.riga {
			if !dentro(c.Nome, r.ammessi) {
				errori = append(errori, fmt.Sprintf("%s.%s: quella riga della fotografia non ha il campo", r.nome, c.Nome))
			}
		}
	}
	return errori
}

// controllaCollocazione: le contraddizioni di una voce dello scenario (D8, R-51 e la nota sulle radici vuote): una
// voce radice o figlio senza la base del target; un figlio senza radici (in confronto «vuoto» vuol dire «nessuna
// radice», e darebbe un falso bloccante); una radice attesa vuota, che non è una base.
func controllaCollocazione(v VoceAttesa) []string {
	var errori []string
	if (v.Atteso == confronto.AttesoRadice || v.Atteso == confronto.AttesoFiglio) && strings.TrimSpace(v.TargetBase) == "" {
		errori = append(errori, fmt.Sprintf("atteso %s senza atteso_target_base: la voce si contraddice (D8)", v.Atteso))
	}
	if v.Atteso == confronto.AttesoFiglio && len(v.Radici) == 0 {
		errori = append(errori, "atteso figlio senza radici: in confronto vuoto vuol dire «nessuna radice» (R-51)")
	}
	for i, r := range v.Radici {
		if strings.TrimSpace(r) == "" {
			errori = append(errori, fmt.Sprintf("radici[%d] vuota: una radice attesa vuota non è una base", i))
		}
	}
	return errori
}

// decisioneDi: le righe attese di proposta e di documento nella forma del vecchio (confronto.FileAtteso.Decisione),
// per il rapporto; nil se la voce non ne ha.
func decisioneDi(v VoceAttesa) (*confronto.Vecchio, string) {
	if len(v.Proposta) == 0 && len(v.Documento) == 0 {
		return nil, ""
	}
	d := &confronto.Vecchio{}
	testo := func(riga []CampoAtteso, nome string) string {
		c, _ := campo(riga, nome)
		return c.Valore
	}
	d.Stato, d.Fonte = testo(v.Proposta, "stato"), testo(v.Proposta, "fonte")
	d.Codice, d.Rev = testo(v.Proposta, "codice"), testo(v.Proposta, "rev")
	if c := testo(v.Documento, "codice"); c != "" {
		d.Codice = c
	}
	if r := testo(v.Documento, "rev"); r != "" {
		d.Rev = r
	}
	var errore string
	id := func(riga []CampoAtteso, nome string) *uuid.UUID {
		c, ok := campo(riga, nome)
		if !ok || c.Nullo || c.Valore == "" {
			return nil
		}
		u, err := uuid.Parse(c.Valore)
		if err != nil {
			errore = nome + ": l'UUID non si legge"
			return nil
		}
		return &u
	}
	d.ComponenteProposta = id(v.Proposta, "componente_id")
	d.Componente = id(v.Documento, "componente_id")
	d.Documento = id(v.Documento, "documento_id")
	d.SostituitoDa = id(v.Documento, "sostituito_da")
	if c, ok := campo(v.Proposta, "deciso_il"); ok && !c.Nullo && c.Valore != "" {
		t, err := tempoAtteso(c.Valore)
		if err != nil {
			errore = "deciso_il: " + err.Error()
		} else {
			d.DecisoIl = &t
		}
	}
	return d, errore
}

// tempoAtteso: un istante degli attesi, in RFC 3339, in UTC e al millisecondo (come la fotografia: 6.4.1).
func tempoAtteso(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("l'istante %q non è RFC 3339", s)
	}
	return t.UTC().Truncate(time.Millisecond), nil
}

// prodottoAttesoDi: un prodotto atteso dello scenario come confronto.ProdottoAtteso. Il campo dell'evidenza in una
// cella (fonte_quantita) si traduce solo per il valore «cella» [T]: un altro valore non si inventa, ed è un errore di
// traduzione (va aggiunto qui quando la prima corsa lo mostra).
func prodottoAttesoDi(p ProdottoScenario) (confronto.ProdottoAtteso, string) {
	pa := confronto.ProdottoAtteso{CodiceRichiesto: p.Originale, Base: p.Base, Fase: p.Fase}
	if p.Base == "" {
		return pa, "prodotto atteso senza base"
	}
	if p.Quantita != nil {
		q := *p.Quantita
		pa.Quantita = &q
	}
	switch p.FonteQuantita {
	case "":
	case "cella":
		v := true
		pa.QuantitaDaCella = &v
	default:
		return pa, fmt.Sprintf("fonte_quantita %q non tradotta", p.FonteQuantita)
	}
	return pa, ""
}

// atteso: l'Atteso di un thread, in una copia; nil se gli attesi non ne hanno.
func (tr traduzione) atteso(thread uuid.UUID) *confronto.Atteso {
	a := tr.perThread[thread]
	if a == nil {
		return nil
	}
	copia := *a
	return &copia
}
