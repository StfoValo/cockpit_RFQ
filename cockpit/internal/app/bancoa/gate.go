package bancoa

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	valut "promatec/cockpit/internal/core/valutazione"
)

// gate.go: il gate del banco (piano 6.4.9, «Il gate nel rapporto»; v3 §6; ATT «gate»; R44, R58 B). Ogni voce ha il suo
// stato: superata | non_superata | non_eseguita. Una voce NON ESEGUITA rende il gate «incompleto», mai «superato».
// Con -gate l'esito del gate decide l'uscita: superato 0, non superato 1, incompleto 3 (1 prevale su 3).
//
// Sui dati veri gli assi del prodotto, lo stato e il fascicolo sono informazione, mai voci del gate (T-B0-16, R69 A):
// stanno nella sezione «prodotti» del rapporto. Le voci che il runner non può verificare da sé (gli esiti delle L4
// e di G1 e G5, scritti nel registro; le etichette di due profili che ancora mancano) restano NON ESEGUITE, con il
// motivo: il runner non le dà mai per superate.
//
// Ogni voce ha anche la classe e l'esito tri-stato (R116 B, precisata dall'utente il 07/10; classi.go): le voci 1, 2, 3
// e 5 sono obbligatorie del runner, le voci 4, 6 e 7 obbligatorie esterne, con la prova che le chiude. La classe non
// cambia l'esito del gate: un obbligatorio esterno non eseguito lo lascia incompleto, e con -gate l'uscita resta 3.

// Gli stati di una voce e gli esiti del gate.
const (
	VoceSuperata    = "superata"
	VoceNonSuperata = "non_superata"
	VoceNonEseguita = "non_eseguita"

	GateSuperato    = "superato"
	GateNonSuperato = "non_superato"
	GateIncompleto  = "incompleto"
)

// I nomi delle voci del gate (v3 §6; 6.4.9), neutri.
const (
	VoceGateFalseAssociazioni   = "zero_false_associazioni"
	VoceGateConteggiScenario    = "conteggi_scenario"
	VoceGateDecisioniPreservate = "decisioni_preservate"
	VoceGateZeroScritture       = "zero_scritture_di_dominio"
	VoceGateRiservati           = "nessun_riservato_contato_come_passato"
	VoceGateMotoreSenzaLLM      = "motore_senza_llm"
	VoceGateAltriProfili        = "forme_su_manifest_con_id_espliciti"
)

// VoceGate: una voce del gate con il suo stato e il motivo, la classe della tabella e l'esito derivato dallo stato
// (R116 B; classi.go).
type VoceGate struct {
	Nome   string          `json:"nome"`
	Stato  string          `json:"stato"`
	Motivo string          `json:"motivo,omitempty"`
	Classe ClasseControllo `json:"classe"`
	Esito  EsitoTriStato   `json:"esito"`
}

// CoperturaCliente: copertura, astensioni e bloccanti di un cliente, in numeri e non in percentuali (v3 §6 r.367).
type CoperturaCliente struct {
	ClienteID  uuid.UUID `json:"cliente_id"`
	Copertura  int       `json:"copertura"`
	Astensioni int       `json:"astensioni"`
	Bloccanti  int       `json:"bloccanti"`
}

// ConteggioScenario: un conteggio dello scenario, dichiarato dagli attesi e ottenuto dagli esiti del motore.
// InPiu, per i prodotti: i prodotti del motore che lo scenario non attende (non_coperto); uno basta a non superare la
// voce (R-108: un prodotto in più non è un prodotto giusto).
type ConteggioScenario struct {
	Nome       string `json:"nome"`
	Dichiarato int    `json:"dichiarato"`
	Ottenuto   int    `json:"ottenuto"`
	InPiu      int    `json:"in_piu,omitempty"`
}

// Gate: le voci e i loro numeri (6.4.9; il campo dei conteggi si chiama ConteggiScenario, senza sigle: E-07).
//   - FalseAssociazioni: errato più falsa_associazione sui file dello scenario e della baseline, un esito per file;
//     PerCliente: copertura (corretti e fuori richiesta) e astensioni (ambigui e senza risposta).
//   - DecisioniPreservate su DecisioniBaseline: le voci della baseline che passano i controlli (1) e (2); accanto,
//     RevisioniBaseline: le differenze di revisione diagnosticate sui loro file (R113 B ratificata), che non tolgono la
//     preservazione.
//   - Riservati, DipendeDa: le voci riservate e quelle con una domanda aperta, separate; mai contate come passate.
//   - DaRivedere, NonCoperti, NonValutati: a parte, fuori dal gate.
//   - NonVerificabili: le voci dello scenario e della baseline che la corsa non può verificare (il thread non si valuta
//     per un limite degli ingressi: T-B6-104), con il motivo: non contano, e la voce non è mai superata.
type Gate struct {
	FalseAssociazioni   int                    `json:"false_associazioni"`
	PerCliente          []CoperturaCliente     `json:"per_cliente,omitempty"`
	ConteggiScenario    []ConteggioScenario    `json:"conteggi_scenario,omitempty"`
	DecisioniPreservate int                    `json:"decisioni_preservate"`
	DecisioniBaseline   int                    `json:"decisioni_baseline"`
	RevisioniBaseline   RevisioniDellaBaseline `json:"revisioni_baseline"`
	Riservati           []string               `json:"riservati,omitempty"`
	DipendeDa           []string               `json:"dipende_da,omitempty"`
	DaRivedere          int                    `json:"da_rivedere"`
	NonCoperti          int                    `json:"non_coperti"`
	NonValutati         []string               `json:"non_valutati,omitempty"`
	NonVerificabili     []string               `json:"non_verificabili,omitempty"`
	Voci                []VoceGate             `json:"voci"`
	Esito               string                 `json:"esito"`
}

// RevisioniDellaBaseline: l'indicatore di revisione dei file delle decisioni della baseline, contato per la voce «le
// decisioni preservate, con le differenze di revisione diagnosticate» (R113 B ratificata; E2 §3.4).
//   - Diagnosticate = Diverse + VecchieDiscordi: le revisioni diverse, e quelle non determinabili perché le due
//     revisioni vecchie, del codice e della colonna, non concordano (revisione_vecchia_discorde: il confronto non sceglie
//     e lo annota con confronto.revisione_non_confrontabile). Sono le differenze che il confronto diagnostica.
//   - Uguali; NonDeterminabili: le altre non determinabili (una revisione che non si legge da un lato, la colonna che non
//     si legge…), che non sono differenze; SenzaRiga: le voci il cui file non ha una riga di confronto.
//
// Nessuna toglie la preservazione: l'indicatore di revisione resta separato dalla correttezza dell'associazione (R113:
// «Mantieni separato l'indicatore di revisione dalla correttezza dell'associazione»), e la sola revisione non è mai un
// badge (v3 §6). La cautela sul vecchio motore non tocca le decisioni confermate: si conta, non si toglie.
type RevisioniDellaBaseline struct {
	Diagnosticate    int `json:"diagnosticate"`
	Diverse          int `json:"diverse"`
	VecchieDiscordi  int `json:"vecchie_discordi"`
	Uguali           int `json:"uguali"`
	NonDeterminabili int `json:"non_determinabili"`
	SenzaRiga        int `json:"senza_riga"`
}

// revisioniDellaBaseline: l'indicatore di revisione delle voci della baseline, contato.
func revisioniDellaBaseline(eb []esitoBaseline) RevisioniDellaBaseline {
	var r RevisioniDellaBaseline
	for _, b := range eb {
		switch {
		case b.Revisione == nil:
			r.SenzaRiga++
		case b.Revisione.Valore == confronto.RevisioneDiversa:
			r.Diverse++
		case b.Revisione.Valore == confronto.RevisioneUguale:
			r.Uguali++
		case b.Revisione.Motivo == valut.MotivoRevisioniVecchiaDiscorde:
			r.VecchieDiscordi++
		default:
			r.NonDeterminabili++
		}
	}
	r.Diagnosticate = r.Diverse + r.VecchieDiscordi
	return r
}

// testoRevisioni: le revisioni della baseline nel motivo della voce delle decisioni preservate.
func testoRevisioni(r RevisioniDellaBaseline) string {
	return fmt.Sprintf("differenze di revisione diagnosticate %d (diverse %d, revisioni vecchie discordi %d), uguali %d, non determinabili %d, senza riga %d: l'indicatore di revisione è separato dall'associazione e non toglie la preservazione (R113 B)",
		r.Diagnosticate, r.Diverse, r.VecchieDiscordi, r.Uguali, r.NonDeterminabili, r.SenzaRiga)
}

// esitoVoce: l'esito di una voce degli attesi contro il suo file, come lo legge il gate.
type esitoVoce struct {
	Percorso   string
	Sezione    string
	AllegatoID uuid.UUID
	Cliente    uuid.UUID
	Atteso     string // la collocazione attesa (scenario)
	Radici     int    // quante radici attese ha un figlio dello scenario (file_condivisi: più di una)
	Esito      confronto.EsitoAtteso
	Riservata  bool
	DipendeDa  string
	// NonVerificabile: il motivo per cui la corsa non può verificare l'esito (T-B6-104); vuoto se si verifica.
	NonVerificabile string
}

// ingressoGate: ciò che serve a calcolare il gate, già preparato dal runner.
//   - Voci: un esito per voce risolta degli attesi; NonCoperti: i file senza voci.
//   - Scenario, ProdottiScenario: lo scenario degli attesi e gli esiti dei suoi prodotti.
//   - Baseline: i controlli (1) e (2) della baseline.
//   - SolaLettura: i controlli di sola lettura del collegamento e della transazione (o «nessun database aperto»),
//     con il motivo; ok falso se uno non passa.
//   - CasiRiservatiNonRiservati: casi_contratto riservati negli attesi con un esito diverso da «riservato».
//   - NonValutati: i thread non valutati; Censimento: i messaggi fuori RFQ dei casi di censimento.
//   - VociNonRisolte, BaselineNonRisolte: le voci dello scenario e della baseline, e della sola baseline, che non si
//     risolvono nella fotografia o non si traducono (n.2, traduzione): una voce non vista non è una voce superata.
//   - ScrittureConsentite: le voci dello scenario con scritture_consentite, che la voce «zero scritture» dichiara non
//     verificate dal runner (R-118);
//   - ScenarioNonVerificabile: perché la corsa non può verificare lo scenario (il suo thread non è fra quelli scelti,
//     o non si valuta per un limite degli ingressi): allora i conteggi non sono eseguiti, mai «diversi» (R-104).
type ingressoGate struct {
	Voci                      []esitoVoce
	NonCoperti                int
	Scenario                  *ScenarioAtteso
	ProdottiScenario          []confronto.EsitoProdottoAtteso
	Baseline                  []esitoBaseline
	SolaLetturaOk             bool
	SolaLetturaMotivo         string
	CasiRiservatiNonRiservati int
	NonValutati               []string
	Censimento                int
	VociNonRisolte            int
	BaselineNonRisolte        int
	ScenarioNonVerificabile   string
	ScrittureConsentite       []string
}

// calcolaGate: le voci del gate, una per una (6.4.9), e l'esito.
func calcolaGate(in ingressoGate) Gate {
	g := Gate{NonCoperti: in.NonCoperti, NonValutati: append([]string(nil), in.NonValutati...)}

	// Un esito per file (D7): un file in due sezioni conta una volta sola, con la prima sezione nell'ordine del gate.
	ordine := map[string]int{confronto.SezioneScenario: 0, confronto.SezioneBaseline: 1, confronto.SezioneReali: 2, confronto.SezioneDaRivedere: 3}
	voci := append([]esitoVoce(nil), in.Voci...)
	sort.SliceStable(voci, func(i, j int) bool {
		if c := bytes.Compare(voci[i].AllegatoID[:], voci[j].AllegatoID[:]); c != 0 {
			return c < 0
		}
		return ordine[voci[i].Sezione] < ordine[voci[j].Sezione]
	})
	visti := map[uuid.UUID]bool{}
	perCliente := map[uuid.UUID]*CoperturaCliente{}
	nelGate, riservatiPassati := 0, 0
	for _, v := range voci {
		if v.Sezione == confronto.SezioneDaRivedere {
			g.DaRivedere++
		}
		if v.Riservata {
			g.Riservati = append(g.Riservati, v.Percorso)
			// La C5 è fuori dal gate: la sua voce è da_rivedere anche se riservata (R-110), e non è «passata».
			if v.Esito != confronto.EsitoRiservato && v.Esito != confronto.EsitoDaRivedere {
				riservatiPassati++
			}
		}
		if v.DipendeDa != "" {
			g.DipendeDa = append(g.DipendeDa, v.Percorso+" ("+v.DipendeDa+")")
		}
		if visti[v.AllegatoID] {
			continue
		}
		visti[v.AllegatoID] = true
		if v.Sezione != confronto.SezioneScenario && v.Sezione != confronto.SezioneBaseline {
			continue
		}
		if v.NonVerificabile != "" {
			g.NonVerificabili = append(g.NonVerificabili, v.Percorso+" ("+v.NonVerificabile+")")
			continue
		}
		pc := perCliente[v.Cliente]
		if pc == nil {
			pc = &CoperturaCliente{ClienteID: v.Cliente}
			perCliente[v.Cliente] = pc
		}
		switch confronto.PesoNelGate(v.Esito) {
		case confronto.PesoCopertura:
			pc.Copertura++
			nelGate++
		case confronto.PesoAstensione:
			pc.Astensioni++
			nelGate++
		case confronto.PesoBloccante:
			pc.Bloccanti++
			g.FalseAssociazioni++
			nelGate++
		}
	}
	for _, pc := range perCliente {
		g.PerCliente = append(g.PerCliente, *pc)
	}
	sort.Slice(g.PerCliente, func(i, j int) bool {
		return bytes.Compare(g.PerCliente[i].ClienteID[:], g.PerCliente[j].ClienteID[:]) < 0
	})
	sort.Strings(g.Riservati)
	sort.Strings(g.DipendeDa)
	sort.Strings(g.NonVerificabili)

	// 1. zero false associazioni, con la copertura misurata. Una falsa associazione prevale; poi una voce non verificata
	// o non risolta rende la voce non eseguita: zero false associazioni su una parte dei file non è «zero».
	switch {
	case g.FalseAssociazioni > 0:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateFalseAssociazioni, Stato: VoceNonSuperata,
			Motivo: fmt.Sprintf("%d esiti errato o falsa_associazione", g.FalseAssociazioni)})
	case len(g.NonVerificabili) > 0 || in.VociNonRisolte > 0:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateFalseAssociazioni, Stato: VoceNonEseguita,
			Motivo: fmt.Sprintf("%d voci dello scenario o della baseline non verificabili e %d non risolte", len(g.NonVerificabili), in.VociNonRisolte)})
	case nelGate == 0:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateFalseAssociazioni, Stato: VoceNonEseguita,
			Motivo: "nessun file dello scenario o della baseline con un esito nel gate"})
	default:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateFalseAssociazioni, Stato: VoceSuperata})
	}

	// 2. i conteggi dello scenario: quelli che gli attesi dichiarano, contro gli esiti.
	g.ConteggiScenario = conteggiOttenuti(in.Scenario, voci, in.ProdottiScenario)
	switch {
	case len(g.ConteggiScenario) == 0:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateConteggiScenario, Stato: VoceNonEseguita,
			Motivo: "lo scenario degli attesi non dichiara conteggi che il runner sappia contare"})
	case in.ScenarioNonVerificabile != "":
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateConteggiScenario, Stato: VoceNonEseguita,
			Motivo: "lo scenario non si verifica in questa corsa: " + in.ScenarioNonVerificabile})
	default:
		diversi := 0
		for _, c := range g.ConteggiScenario {
			if c.Dichiarato != c.Ottenuto || c.InPiu > 0 {
				diversi++
			}
		}
		if diversi > 0 {
			g.Voci = append(g.Voci, VoceGate{Nome: VoceGateConteggiScenario, Stato: VoceNonSuperata,
				Motivo: fmt.Sprintf("%d conteggi diversi da quelli dichiarati", diversi)})
		} else {
			g.Voci = append(g.Voci, VoceGate{Nome: VoceGateConteggiScenario, Stato: VoceSuperata})
		}
	}

	// 3. le decisioni preservate: (1) e (2) della baseline. Una decisione con una differenza non è preservata; una con
	// una parte non verificata (R-102: deciso_da con gli export; T-B6-104: il file non si valuta) non lo è nemmeno, ma
	// rende la voce non eseguita, mai superata; così una voce della baseline che non si risolve. Le differenze di
	// revisione diagnosticate sui file della baseline stanno nel motivo, accanto (R113 B ratificata; E2 §3.4): si contano,
	// e non cambiano lo stato della voce.
	g.DecisioniBaseline = len(in.Baseline)
	g.RevisioniBaseline = revisioniDellaBaseline(in.Baseline)
	revisioni := testoRevisioni(g.RevisioniBaseline)
	conDifferenze, nonVerificate := 0, 0
	for _, b := range in.Baseline {
		switch {
		case len(b.Differenze) > 0:
			conDifferenze++
		case len(b.NonVerificate) > 0:
			nonVerificate++
		case b.Righe && b.Base:
			g.DecisioniPreservate++
		default:
			conDifferenze++
		}
	}
	switch {
	case conDifferenze > 0:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateDecisioniPreservate, Stato: VoceNonSuperata,
			Motivo: fmt.Sprintf("%d su %d; %d con differenze; %s", g.DecisioniPreservate, g.DecisioniBaseline, conDifferenze, revisioni)})
	case nonVerificate > 0 || in.BaselineNonRisolte > 0:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateDecisioniPreservate, Stato: VoceNonEseguita,
			Motivo: fmt.Sprintf("%d su %d; %d con parti non verificate, %d voci non risolte; %s", g.DecisioniPreservate, g.DecisioniBaseline,
				nonVerificate, in.BaselineNonRisolte, revisioni)})
	case g.DecisioniBaseline == 0:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateDecisioniPreservate, Stato: VoceNonEseguita,
			Motivo: "nessuna voce della baseline risolta"})
	default:
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateDecisioniPreservate, Stato: VoceSuperata,
			Motivo: fmt.Sprintf("%d su %d; %s", g.DecisioniPreservate, g.DecisioniBaseline, revisioni)})
	}

	// 4. zero scritture di dominio: la parte del runner, più gli esiti delle L4 che stanno nel registro, e le voci dello
	// scenario con scritture_consentite, un invariante del prodotto che il runner non verifica (R-118).
	scritture := ""
	if len(in.ScrittureConsentite) > 0 {
		scritture = fmt.Sprintf("e %d voci dello scenario con scritture_consentite (invariante del prodotto, nessuna proiezione su un target di scenario: non le verifica il runner): %s",
			len(in.ScrittureConsentite), strings.Join(in.ScrittureConsentite, ", "))
	}
	if in.SolaLetturaOk {
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateZeroScritture, Stato: VoceNonEseguita,
			Motivo: unisciMotivi(unisciMotivi(in.SolaLetturaMotivo, "manca l'esito di A1c-L4D-12 e A1c-L4S-09, che sta nel registro e non nel runner"), scritture)})
	} else {
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateZeroScritture, Stato: VoceNonSuperata, Motivo: unisciMotivi(in.SolaLetturaMotivo, scritture)})
	}

	// 5. nessun riservato contato come passato.
	if riservatiPassati+in.CasiRiservatiNonRiservati > 0 {
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateRiservati, Stato: VoceNonSuperata,
			Motivo: fmt.Sprintf("%d voci riservate con un esito diverso da riservato", riservatiPassati+in.CasiRiservatiNonRiservati)})
	} else {
		g.Voci = append(g.Voci, VoceGate{Nome: VoceGateRiservati, Stato: VoceSuperata,
			Motivo: fmt.Sprintf("%d riservati e %d con dipende_da, separati", len(g.Riservati), len(g.DipendeDa))})
	}

	// 6. motore senza LLM: gli esiti di A1c-L4S-08 e A1c-L4T-01, G1 e G5 non sono del runner.
	g.Voci = append(g.Voci, VoceGate{Nome: VoceGateMotoreSenzaLLM, Stato: VoceNonEseguita,
		Motivo: "gli esiti di A1c-L4S-08, A1c-L4T-01 (con la copia _run), G1 e G5 stanno nel registro (R58 B; 6.9)"})

	// 7. le forme dei due profili ancora senza etichette (R34 c): solo il censimento, finché le etichette mancano.
	g.Voci = append(g.Voci, VoceGate{Nome: VoceGateAltriProfili, Stato: VoceNonEseguita,
		Motivo: fmt.Sprintf("mancano le etichette negli attesi: c'è solo il censimento (%d messaggi fuori RFQ)", in.Censimento)})

	classificaVoci(g.Voci)
	g.Esito = GateSuperato
	for _, v := range g.Voci {
		switch {
		case v.Stato == VoceNonSuperata:
			g.Esito = GateNonSuperato
		case v.Stato == VoceNonEseguita && g.Esito == GateSuperato:
			g.Esito = GateIncompleto
		}
	}
	return g
}

// conteggiOttenuti: per ogni conteggio dichiarato dallo scenario che il runner sa contare dagli esiti, il valore
// dichiarato e quello ottenuto:
//   - radice e figlio (e candidati_radice, candidati_figlio, i nomi degli attesi: T-B6c-09): i file dello scenario con
//     quella collocazione attesa e l'esito corretto;
//   - file_condivisi: i figli con più di una radice attesa e l'esito corretto (che per un figlio vuole le radici
//     raggiungibili uguali a quelle attese);
//   - fuori_scenario: i file dello scenario con l'esito fuori_richiesta;
//   - prodotti: i prodotti dello scenario con l'esito corretto, e in InPiu quelli del motore che lo scenario non attende
//     (non_coperto: confronto lascia i conteggi al runner).
//
// «file» e «allegati» contano le voci degli attesi, non gli esiti: li controlla il n.5, non il gate; «hash_distinti» lo
// conta il n.2 sulla fotografia.
func conteggiOttenuti(s *ScenarioAtteso, voci []esitoVoce, prodotti []confronto.EsitoProdottoAtteso) []ConteggioScenario {
	if s == nil {
		return nil
	}
	var out []ConteggioScenario
	for _, c := range s.Conteggi {
		n, inPiu := 0, 0
		coll, perCollocazione := collocazioneDelConteggio(c.Nome)
		switch {
		case perCollocazione:
			for _, v := range voci {
				if v.Sezione == confronto.SezioneScenario && v.Atteso == coll && v.Esito == confronto.EsitoCorretto {
					n++
				}
			}
		case c.Nome == "file_condivisi":
			for _, v := range voci {
				if v.Sezione == confronto.SezioneScenario && v.Atteso == confronto.AttesoFiglio && v.Radici > 1 && v.Esito == confronto.EsitoCorretto {
					n++
				}
			}
		case c.Nome == "fuori_scenario":
			for _, v := range voci {
				if v.Sezione == confronto.SezioneScenario && v.Esito == confronto.EsitoFuoriRichiesta {
					n++
				}
			}
		case c.Nome == "prodotti":
			for _, p := range prodotti {
				switch p.Esito {
				case confronto.EsitoCorretto:
					n++
				case confronto.EsitoNonCoperto:
					inPiu++
				}
			}
		default:
			continue
		}
		out = append(out, ConteggioScenario{Nome: c.Nome, Dichiarato: c.Valore, Ottenuto: n, InPiu: inPiu})
	}
	return out
}

// esitoUscitaGate: il gate come voce dei controlli, per -gate (R44): non superato è una differenza per ogni voce non
// superata; incompleto è NON ESEGUITO, con le voci che mancano; superato non cambia niente.
func esitoUscitaGate(g Gate) esitoControllo {
	e := esitoControllo{nome: "gate", eseguito: true, motivo: g.Esito}
	for _, v := range g.Voci {
		switch v.Stato {
		case VoceNonSuperata:
			e.differenze = append(e.differenze, v.Nome)
		case VoceNonEseguita:
			e.nonFatti = append(e.nonFatti, v.Nome)
		}
	}
	return e
}
