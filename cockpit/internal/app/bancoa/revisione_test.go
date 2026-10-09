package bancoa

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/estrazione/evidenze"
	"promatec/cockpit/internal/core/fotorfq"
	"promatec/cockpit/internal/core/inbox/classificazione/motorea"
	valut "promatec/cockpit/internal/core/valutazione"
	"promatec/cockpit/internal/platform/dataset"
	"promatec/cockpit/internal/platform/migrazioni"
)

// L1 — le correzioni della revisione di P9 (R-101…R-115) e i pareri dell'orchestratore che le accompagnano (T-B6-104,
// i due ingressi incoerenti, il n.3 nelle due direzioni, il gate senza -gate). La regola che le tiene insieme: una
// chiave degli attesi che il runner accetta è verificata, oppure è dichiarata non verificata fra i nonFatti del suo
// controllo, con il percorso; mai buttata. Ogni prova fissa una correzione: tolta la correzione, la prova cade (le
// mutazioni di ritorno sono nello scratchpad dell'implementatore, non nel repository).
//
// I clienti di questi test sono inventati: vedi scena_banco_test.go.

// corsaBanco: la scena ACME sugli export, con le mutazioni, gli attesi, il gate e i thread scelti.
func corsaBanco(t *testing.T, m mutaBanco, attesi, gate bool, thread ...uuid.UUID) RapportoBanco {
	t.Helper()
	o := preparaBanco(t, m).opzioniExport(attesi)
	o.Gate, o.Thread = gate, thread
	r, err := EseguiBanco(context.Background(), o, nil)
	if err != nil {
		t.Fatalf("EseguiBanco: %v", err)
	}
	return r
}

func attesiMutati(f func(string) string) mutaBanco {
	return mutaBanco{testi: map[string]func(string) string{"attesi": f}}
}

// sostituisci: una sostituzione che deve cambiare il testo, altrimenti la prova non proverebbe niente.
func sostituisci(t *testing.T, s, vecchio, nuovo string) string {
	t.Helper()
	if !strings.Contains(s, vecchio) {
		t.Fatalf("la sostituzione non trova %q: il caso non prova nulla", vecchio)
	}
	return strings.Replace(s, vecchio, nuovo, 1)
}

func dettaglio(r RapportoBanco, nome string) DettaglioControllo {
	for _, d := range r.Dettagli {
		if d.Nome == nome {
			return d
		}
	}
	return DettaglioControllo{Nome: nome}
}

// conPrefisso: un elemento comincia con il prefisso.
func conPrefisso(xs []string, prefisso string) bool {
	for _, x := range xs {
		if strings.HasPrefix(x, prefisso) {
			return true
		}
	}
	return false
}

// ---- R-101: le letture attese della baseline e dei reali ----

// TestR101LettureSullaScena: una revisione e un marcatore in più nella baseline, la base sbagliata nei reali. Prima
// erano lette e buttate, e la corsa era conforme. Sulla scena l'interpretazione dei file è parziale (gli export ACME non
// hanno fatti): ogni chiave senza ambito è dichiarata non verificata, una per una, e la corsa non è conforme. La base dei
// reali invece è quella del nome dell'archivio (D-R116 O, B6c): il nome si legge per intero, e la base sbagliata è una
// differenza.
func TestR101LettureSullaScena(t *testing.T) {
	r := corsaBanco(t, attesiMutati(func(a string) string {
		a = sostituisci(t, a, "0111\"\n    atteso:\n      base: 9123456\n", "0111\"\n    atteso:\n      base: 9123456\n      revisione: \"Z9\"\n      marcatore: \"Q\"\n")
		return sostituisci(t, a, "      base: 9123460\n", "      base: 9999999\n")
	}), true, false)
	d := dettaglio(r, ControlloLetture)
	for _, k := range []string{"baseline_decisioni[0].atteso.marcatore: interpretazione parziale",
		"baseline_decisioni[0].atteso.revisione: interpretazione parziale"} {
		if !conPrefisso(d.NonVerificate, k) {
			t.Errorf("la chiave non è dichiarata: %q in %q", k, d.NonVerificate)
		}
	}
	if !conPrefisso(d.Differenze, "reali_archivi[0].atteso.base: atteso 9999999, ottenuto 9123460") || conPrefisso(d.NonVerificate, "reali_archivi[0].atteso.base") {
		t.Errorf("la base dei reali si giudica sul nome: differenze %q, non verificate %q", d.Differenze, d.NonVerificate)
	}
	if conPrefisso(d.NonVerificate, "baseline_decisioni[0].atteso.base") {
		t.Errorf("la base della baseline è il controllo (2), non un'altra lettura: %q", d.NonVerificate)
	}
	if r.Esito == EsitoConforme {
		t.Fatalf("letture non verificate e corsa conforme: %s", r.PrimaRiga())
	}
}

// TestR101LettureControLInterpretazione: con un'interpretazione completa le letture attese si confrontano con le regole
// della tabella dei casi: una base sbagliata è una differenza, una revisione nulla attesa e nulla letta sul nome passa,
// una chiave che vuole il testo di un caso non si confronta sul file. Dove il confronto non è diretto (file non valutato,
// thread non valutabile) ogni chiave è fra i nonFatti; con l'interpretazione parziale le chiavi senza ambito sono fra i
// nonFatti, e quelle del nome si giudicano lo stesso, perché il nome si legge sempre per intero (R-116).
func TestR101LettureControLInterpretazione(t *testing.T) {
	ins := insiemeACME(t)
	interp := func(stato string) *valut.FileInterpretato {
		return &valut.FileInterpretato{AllegatoID: allArchivio, Interpretazione: motorea.Interpretazione{ClienteID: clienteACME, Stato: stato,
			Letture: []motorea.LetturaCodice{{UnitaID: "u:nome", Funzione: motorea.FunzIdentitaFile,
				Forma: motorea.LetturaForma{Famiglia: "acme-marcatore", Selettore: evidenze.Selettore{Contesto: evidenze.ContestoNomeFile},
					Base: motorea.BaseLetta{Normalizzata: "9123460", Completa: true}}}}}}
	}
	letture := func(base string) []ChiaveAttesa {
		return []ChiaveAttesa{{Chiave: "base", Valore: ValoreAtteso{Tipo: TipoIntero, Testo: base}},
			{Chiave: "revisione_dal_nome", Valore: ValoreAtteso{Tipo: TipoNullo}},
			{Chiave: "originale_conservato", Valore: ValoreAtteso{Tipo: TipoBooleano, Testo: "true"}}}
	}
	completo := fileDelThread{fi: interp(motorea.StatoInterpretazioneCompleta), valutato: true, cliente: clienteACME}

	d, nf := lettureDelFile("reali_archivi[0]", letture("9999999"), completo, &ins, "")
	if len(d) != 1 || !strings.HasPrefix(d[0], "reali_archivi[0].atteso.base: atteso 9999999, ottenuto 9123460") {
		t.Errorf("base sbagliata: differenze %q", d)
	}
	if len(nf) != 1 || !strings.HasPrefix(nf[0], "reali_archivi[0].atteso.originale_conservato: vuole il testo di un caso") {
		t.Errorf("la chiave senza confronto diretto: %q", nf)
	}
	if d, _ := lettureDelFile("reali_archivi[0]", letture("9123460"), completo, &ins, ""); len(d) != 0 {
		t.Errorf("base giusta e revisione nulla: %q", d)
	}
	// Con il documento parziale: la base (senza ambito) non si giudica, revisione_dal_nome sì, sul nome.
	parziale := fileDelThread{fi: interp(motorea.StatoInterpretazioneParziale), valutato: true, cliente: clienteACME}
	if d, nf := lettureDelFile("reali_archivi[0]", letture("9999999"), parziale, &ins, ""); len(d) != 0 || len(nf) != 2 ||
		!conPrefisso(nf, "reali_archivi[0].atteso.base: interpretazione parziale") || conPrefisso(nf, "reali_archivi[0].atteso.revisione_dal_nome") {
		t.Errorf("interpretazione parziale: differenze %q, non fatti %q", d, nf)
	}
	rev := []ChiaveAttesa{{Chiave: "revisione_dal_nome", Valore: ValoreAtteso{Tipo: TipoStringa, Testo: "B"}}}
	if d, _ := lettureDelFile("reali_archivi[0]", rev, parziale, &ins, ""); len(d) != 1 {
		t.Errorf("revisione_dal_nome «B» su un nome senza revisione, con il documento parziale: %q", d)
	}
	for nome, fd := range map[string]fileDelThread{
		"thread non valutato": {fi: interp(motorea.StatoInterpretazioneCompleta), cliente: clienteACME},
		"file non valutato":   {fi: &valut.FileInterpretato{AllegatoID: allArchivio, Motivo: valut.MotivoFileDocumentoNonLeggibile}, valutato: true},
		"file assente":        {},
	} {
		if d, nf := lettureDelFile("reali_archivi[0]", letture("9999999"), fd, &ins, ""); len(d) != 0 || len(nf) != 3 {
			t.Errorf("%s: differenze %q, non fatti %q", nome, d, nf)
		}
	}
	if d, nf := lettureDelFile("reali_archivi[0]", letture("9999999"), completo, &ins, "limite degli ingressi"); len(d) != 0 || len(nf) != 3 ||
		!strings.HasSuffix(nf[0], ": limite degli ingressi") {
		t.Errorf("thread non valutabile: differenze %q, non fatti %q", d, nf)
	}
}

// TestR116ChiaviConAmbito: una chiave con un ambito si giudica solo sulle letture dell'unità che nomina. Il nome non ha
// revisioni, il cartiglio ha «B»: revisione_dal_nome «B» non passa (prima passava sulla revisione del cartiglio) e
// revisione_dal_nome null passa (prima era una differenza). Senza letture riconoscibili dell'unità (la prova del
// revisore: unità che il documento non porta e letture senza selettore) la chiave è fra i nonFatti, mai un passato o
// una differenza falsi; così l'ambito «questo campo», che una voce di file non dice.
func TestR116ChiaviConAmbito(t *testing.T) {
	ins := insiemeACME(t)
	lettura := func(unita string, ctx evidenze.Contesto, rev string) motorea.LetturaCodice {
		l := motorea.LetturaCodice{UnitaID: unita, Funzione: motorea.FunzIdentitaFile,
			Forma: motorea.LetturaForma{Famiglia: "acme-marcatore", Selettore: evidenze.Selettore{Contesto: ctx},
				Base: motorea.BaseLetta{Normalizzata: "9123460", Completa: true}}}
		if rev != "" {
			l.Forma.Revisione = &motorea.RevisioneLetta{Normalizzata: rev, Stato: motorea.StatoRevisioneLetta}
		}
		return l
	}
	file := func(letture ...motorea.LetturaCodice) fileDelThread {
		return fileDelThread{fi: &valut.FileInterpretato{AllegatoID: allBaseline, Interpretazione: motorea.Interpretazione{ClienteID: clienteACME,
			Stato: motorea.StatoInterpretazioneCompleta, Letture: letture}}, valutato: true, cliente: clienteACME}
	}
	chiave := func(k string, v ValoreAtteso) []ChiaveAttesa { return []ChiaveAttesa{{Chiave: k, Valore: v}} }
	b := ValoreAtteso{Tipo: TipoStringa, Testo: "B"}
	null := ValoreAtteso{Tipo: TipoNullo}

	// Con i selettori delle unità: il nome senza revisioni, il cartiglio con «B».
	fd := file(lettura("u:nome", evidenze.ContestoNomeFile, ""), lettura("u:pdf:cartiglio:1", evidenze.ContestoCartiglio, "B"))
	if d, nf := lettureDelFile("baseline_decisioni[0]", chiave("revisione_dal_nome", b), fd, &ins, ""); len(d) != 1 || len(nf) != 0 {
		t.Errorf("revisione_dal_nome «B» con la «B» solo nel cartiglio: differenze %q, non fatti %q", d, nf)
	}
	if d, nf := lettureDelFile("baseline_decisioni[0]", chiave("revisione_dal_nome", null), fd, &ins, ""); len(d) != 0 || len(nf) != 0 {
		t.Errorf("revisione_dal_nome null, il nome senza revisioni: differenze %q, non fatti %q", d, nf)
	}
	// L'unità di una lettura si riconosce anche dal documento del file (UnitaID), senza il selettore nella lettura.
	fd2 := file(lettura("u:nome", "", ""), lettura("u:pdf:cartiglio:1", "", "B"))
	fd2.fi.Documento.Unita = []evidenze.UnitaEvidenza{{ID: "u:nome", Selettore: evidenze.Selettore{Contesto: evidenze.ContestoNomeFile}},
		{ID: "u:pdf:cartiglio:1", Selettore: evidenze.Selettore{Contesto: evidenze.ContestoCartiglio}}}
	if d, nf := lettureDelFile("baseline_decisioni[0]", chiave("revisione_dal_nome", b), fd2, &ins, ""); len(d) != 1 || len(nf) != 0 {
		t.Errorf("l'unità dal documento: differenze %q, non fatti %q", d, nf)
	}
	// La prova del revisore (TestRev15): unità che il documento non porta, letture senza selettore.
	rev15 := file(lettura("u:nome_file", "", ""), lettura("u:cartiglio", "", "B"))
	for _, v := range []ValoreAtteso{b, null} {
		if d, nf := lettureDelFile("baseline_decisioni[0]", chiave("revisione_dal_nome", v), rev15, &ins, ""); len(d) != 0 || len(nf) != 1 ||
			!strings.Contains(nf[0], "nessuna lettura sull'unità nome_file") {
			t.Errorf("TestRev15 con %s: differenze %q, non fatti %q", v.String(), d, nf)
		}
	}
	// «Questo campo» su una voce di file; un'unità che non è il nome, con il documento parziale.
	if _, nf := lettureDelFile("baseline_decisioni[0]", chiave("revisione_da_questo_campo", null), fd, &ins, ""); len(nf) != 1 ||
		!strings.Contains(nf[0], "«questo campo»") {
		t.Errorf("revisione_da_questo_campo: %q", nf)
	}
	parziale := file(lettura("u:nome", evidenze.ContestoNomeFile, ""))
	parziale.fi.Interpretazione.Stato = motorea.StatoInterpretazioneParziale
	if _, nf := lettureDelFile("baseline_decisioni[0]", chiave("identita_file_da_nota", ValoreAtteso{Tipo: TipoBooleano, Testo: "false"}), parziale, &ins, ""); len(nf) != 1 ||
		!strings.Contains(nf[0], "l'unità testo_pdf può essere tagliata") {
		t.Errorf("identita_file_da_nota con il documento parziale: %q", nf)
	}
	// Sulla scena: l'archivio dei reali ha il documento parziale, e revisione_dal_nome null si giudica sul nome (passa).
	r := corsaBanco(t, mutaBanco{}, true, false)
	d := dettaglio(r, ControlloLetture)
	if conPrefisso(d.NonVerificate, "reali_archivi[0].atteso.revisione_dal_nome") || conPrefisso(d.Differenze, "reali_archivi[0].atteso.revisione_dal_nome") {
		t.Errorf("revisione_dal_nome dei reali non giudicata sul nome: %+v", d)
	}
	r = corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "      revisione_dal_nome: null\n", "      revisione_dal_nome: \"7\"\n")
	}), true, false)
	if c, _ := controlloBanco(r, ControlloLetture); c.Differenze != 1 || r.Esito.CodiceUscita() != UscitaConDifferenze {
		t.Errorf("revisione_dal_nome «7» sul nome dell'archivio, che non ne ha: %+v, %s", c, r.PrimaRiga())
	}
}

// TestO1ChiaviDelTokenSullAmbitoDellaVoce (T-B6-200; O-1 della controprova delle risposte): token_revisione,
// token_conservato e identita_include_token si giudicano sul solo ambito che la voce degli attesi indica, con le sue
// chiavi che hanno un ambito; mai sul file intero. Il nome non ha token, il cartiglio ha «xx»:
//   - senza chiavi con un ambito nella voce, l'ambito non si determina: le tre chiavi sono fra i nonFatti, con il motivo
//     (prima token_conservato «xx» passava sul token del cartiglio: un passato falso);
//   - con una chiave del nome nella voce, la chiave del token si giudica sul nome: token_conservato «xx» è una differenza,
//     e con il token nel nome passa;
//   - con chiavi di unità diverse, o con l'ambito «questo campo», l'ambito non si determina: nonFatti.
func TestO1ChiaviDelTokenSullAmbitoDellaVoce(t *testing.T) {
	ins := insiemeACME(t)
	lettura := func(unita string, ctx evidenze.Contesto, token string) motorea.LetturaCodice {
		l := motorea.LetturaCodice{UnitaID: unita, Funzione: motorea.FunzIdentitaFile,
			Forma: motorea.LetturaForma{Famiglia: "acme-marcatore", Selettore: evidenze.Selettore{Contesto: ctx},
				Base: motorea.BaseLetta{Normalizzata: "9123460", Completa: true}}}
		if token != "" {
			l.Forma.Token = &motorea.ParteLetta{Valore: token, Originale: token}
			l.Forma.Revisione = &motorea.RevisioneLetta{Originale: token, Stato: motorea.StatoRevisioneNonInterpretabile,
				Token: &motorea.ParteLetta{Valore: token, Originale: token}}
		}
		return l
	}
	file := func(letture ...motorea.LetturaCodice) fileDelThread {
		return fileDelThread{fi: &valut.FileInterpretato{AllegatoID: allBaseline, Interpretazione: motorea.Interpretazione{ClienteID: clienteACME,
			Stato: motorea.StatoInterpretazioneCompleta, Letture: letture}}, valutato: true, cliente: clienteACME}
	}
	k := func(chiave string, v ValoreAtteso) ChiaveAttesa { return ChiaveAttesa{Chiave: chiave, Valore: v} }
	xx := ValoreAtteso{Tipo: TipoStringa, Testo: "xx"}
	null := ValoreAtteso{Tipo: TipoNullo}
	vero := ValoreAtteso{Tipo: TipoBooleano, Testo: "true"}
	falso := ValoreAtteso{Tipo: TipoBooleano, Testo: "false"}
	fd := file(lettura("u:nome", evidenze.ContestoNomeFile, ""), lettura("u:pdf:cartiglio:1", evidenze.ContestoCartiglio, "xx"))

	d, nf := lettureDelFile("baseline_decisioni[0]", []ChiaveAttesa{k("identita_include_token", vero), k("token_conservato", xx), k("token_revisione", xx)}, fd, &ins, "")
	if len(d) != 0 || len(nf) != 3 {
		t.Fatalf("senza un ambito nella voce: differenze %q, non fatti %q", d, nf)
	}
	for _, x := range nf {
		if !strings.Contains(x, "la chiave del token non nomina un'unità e la voce non ne indica una") || !strings.Contains(x, "T-B6-200") {
			t.Errorf("il motivo: %q", x)
		}
	}

	d, nf = lettureDelFile("baseline_decisioni[0]", []ChiaveAttesa{k("revisione_dal_nome", null), k("token_conservato", xx)}, fd, &ins, "")
	if len(nf) != 0 || len(d) != 1 || !strings.HasPrefix(d[0], "baseline_decisioni[0].atteso.token_conservato: atteso xx") {
		t.Errorf("con l'ambito del nome, il token del cartiglio non vale: differenze %q, non fatti %q", d, nf)
	}
	conToken := file(lettura("u:nome", evidenze.ContestoNomeFile, "xx"), lettura("u:pdf:cartiglio:1", evidenze.ContestoCartiglio, ""))
	if d, nf := lettureDelFile("baseline_decisioni[0]", []ChiaveAttesa{k("revisione_dal_token", null), k("token_conservato", xx),
		k("token_revisione", xx)}, conToken, &ins, ""); len(nf) != 0 || conPrefisso(d, "baseline_decisioni[0].atteso.token_") {
		t.Errorf("con il token nel nome: differenze %q, non fatti %q", d, nf)
	}

	for nome, c := range map[string]struct {
		letture []ChiaveAttesa
		motivo  string
	}{
		"unità diverse": {[]ChiaveAttesa{k("identita_file_da_nota", falso), k("revisione_dal_nome", null), k("token_revisione", xx)},
			"le chiavi con un ambito della voce indicano unità diverse (nome_file, testo_pdf)"},
		"questo campo": {[]ChiaveAttesa{k("revisione_da_questo_campo", null), k("token_revisione", xx)}, "l'ambito «questo campo»"},
	} {
		_, nf := lettureDelFile("baseline_decisioni[0]", c.letture, fd, &ins, "")
		token := ""
		for _, x := range nf {
			if strings.HasPrefix(x, "baseline_decisioni[0].atteso.token_revisione: ") {
				token = x
			}
		}
		if !strings.Contains(token, c.motivo) || !strings.Contains(token, "T-B6-200") {
			t.Errorf("%s: non fatti %q", nome, nf)
		}
	}
}

// TestR140AmbitoDedottoEAsserzioniNegative (R-140 della revisione di B6b; T-B6-220, precisata dall'orchestratore): lo
// scenario del revisore, la voce {revisione_dal_nome: null, token_conservato: false}, con il nome senza token e il token
// «xx» solo nel cartiglio. Prima la voce passava in silenzio sul nome, l'ambito dedotto dalla chiave sorella (HEAD dava
// una differenza sul file intero); ora un'asserzione negativa (false, null, la lista vuota) su un ambito dedotto è fra i
// nonFatti, con il motivo «ambito dedotto, asserzione negativa», per le tre chiavi dei token. Un valore positivo resta
// verificabile, e il rapporto scrive su quale unità la chiave è stata giudicata, anche nella corsa sulla scena.
func TestR140AmbitoDedottoEAsserzioniNegative(t *testing.T) {
	ins := insiemeACME(t)
	lettura := func(unita string, ctx evidenze.Contesto, token string) motorea.LetturaCodice {
		l := motorea.LetturaCodice{UnitaID: unita, Funzione: motorea.FunzIdentitaFile,
			Forma: motorea.LetturaForma{Famiglia: "acme-marcatore", Selettore: evidenze.Selettore{Contesto: ctx},
				Base: motorea.BaseLetta{Normalizzata: "9123460", Completa: true}}}
		if token != "" {
			l.Forma.Token = &motorea.ParteLetta{Valore: token, Originale: token}
		}
		return l
	}
	fd := fileDelThread{fi: &valut.FileInterpretato{AllegatoID: allBaseline, Interpretazione: motorea.Interpretazione{ClienteID: clienteACME,
		Stato: motorea.StatoInterpretazioneCompleta, Letture: []motorea.LetturaCodice{lettura("u:nome", evidenze.ContestoNomeFile, ""),
			lettura("u:pdf:cartiglio:1", evidenze.ContestoCartiglio, "xx")}}}, valutato: true, cliente: clienteACME}
	sorella := ChiaveAttesa{Chiave: "revisione_dal_nome", Valore: ValoreAtteso{Tipo: TipoNullo}}
	for _, k := range []ChiaveAttesa{
		{Chiave: "token_conservato", Valore: ValoreAtteso{Tipo: TipoBooleano, Testo: "false"}},
		{Chiave: "token_conservato", Valore: ValoreAtteso{Tipo: TipoNullo}},
		{Chiave: "identita_include_token", Valore: ValoreAtteso{Tipo: TipoBooleano, Testo: "false"}},
		{Chiave: "token_revisione", Valore: ValoreAtteso{Tipo: TipoLista}},
		{Chiave: "token_revisione", Valore: ValoreAtteso{Tipo: TipoNullo}},
	} {
		d, nf, dedotti := lettureDelFileConAmbiti("baseline_decisioni[0]", []ChiaveAttesa{sorella, k}, fd, &ins, "")
		if len(d) != 0 || len(dedotti) != 0 || len(nf) != 1 ||
			!strings.HasPrefix(nf[0], "baseline_decisioni[0].atteso."+k.Chiave+": ambito dedotto, asserzione negativa: sull'unità nome_file") {
			t.Errorf("%s %s: differenze %q, non fatti %q, ambiti dedotti %q", k.Chiave, k.Valore.String(), d, nf, dedotti)
		}
	}
	// Un valore positivo si giudica sull'ambito dedotto, e il rapporto lo scrive; la chiave sorella, che l'ambito lo
	// dichiara, non è fra gli ambiti dedotti.
	d, nf, dedotti := lettureDelFileConAmbiti("baseline_decisioni[0]", []ChiaveAttesa{sorella,
		{Chiave: "token_conservato", Valore: ValoreAtteso{Tipo: TipoStringa, Testo: "xx"}}}, fd, &ins, "")
	if len(d) != 1 || len(nf) != 0 || strings.Join(dedotti, "\n") !=
		"baseline_decisioni[0].atteso.token_conservato: giudicata sull'unità nome_file, ambito dedotto dalle chiavi sorelle (T-B6-220)" {
		t.Errorf("il valore positivo: differenze %q, non fatti %q, ambiti dedotti %q", d, nf, dedotti)
	}

	// Sulla scena: l'archivio dei reali ha revisione_dal_nome; con token_conservato «00» la chiave si giudica sul nome e
	// il rapporto lo dice; con false è un'asserzione negativa, fra le parti non verificate.
	r := corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "      revisione_dal_nome: null\n", "      revisione_dal_nome: null\n      token_conservato: \"00\"\n")
	}), true, false)
	dl := dettaglio(r, ControlloLetture)
	if !conPrefisso(dl.AmbitiDedotti, "reali_archivi[0].atteso.token_conservato: giudicata sull'unità nome_file") ||
		!strings.Contains(r.Testo(), "letture_e_invarianti_dei_file, ambito dedotto: reali_archivi[0].atteso.token_conservato: giudicata sull'unità nome_file") {
		t.Errorf("l'ambito dedotto nel rapporto: %+v", dl)
	}
	r = corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "      revisione_dal_nome: null\n", "      revisione_dal_nome: null\n      token_conservato: false\n")
	}), true, false)
	dl = dettaglio(r, ControlloLetture)
	if !conPrefisso(dl.NonVerificate, "reali_archivi[0].atteso.token_conservato: ambito dedotto, asserzione negativa") || len(dl.AmbitiDedotti) != 0 ||
		conPrefisso(dl.Differenze, "reali_archivi[0].atteso.token_conservato") {
		t.Errorf("l'asserzione negativa sulla scena: %+v", dl)
	}
}

// ---- R-102: le parti della riga della baseline che non si confrontano ----

// TestR102DecisoDaNegliExport: deciso_da non è negli export. La baseline lo dichiara fra le parti non verificate (NON
// ESEGUITO, come la C5), e nel gate quella decisione non è preservata: la voce è non eseguita, mai superata.
func TestR102DecisoDaNegliExport(t *testing.T) {
	r := corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "      deciso_il: \"2026-10-01T09:00:00.000Z\"\n    decisione_documento:",
			"      deciso_il: \"2026-10-01T09:00:00.000Z\"\n      deciso_da: 00000000-0000-4000-8000-000000000051\n    decisione_documento:")
	}), true, true)
	c, _ := controlloBanco(r, ControlloBaselineRighe)
	if c.Stato != ControlloNonEseguito || c.Differenze != 0 ||
		!conPrefisso(dettaglio(r, ControlloBaselineRighe).NonVerificate, "baseline_decisioni[0]: proposta.deciso_da (non è negli export)") {
		t.Errorf("baseline con deciso_da: %+v, %+v", c, dettaglio(r, ControlloBaselineRighe))
	}
	if v := voce(*r.Gate, VoceGateDecisioniPreservate); v.Stato != VoceNonEseguita || r.Gate.DecisioniPreservate != 0 {
		t.Errorf("decisioni preservate con una parte non verificata: %+v, %d", v, r.Gate.DecisioniPreservate)
	}
}

// TestR102CampoCheLaRigaNonHa: confermato_il e sostituito_da sono del documento, non della proposta: prima passavano in
// silenzio; ora sono errori di traduzione (uscita 1), e la voce non arriva ai controlli.
func TestR102CampoCheLaRigaNonHa(t *testing.T) {
	r := corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "      deciso_il: \"2026-10-01T09:00:00.000Z\"\n    decisione_documento:",
			"      deciso_il: \"2026-10-01T09:00:00.000Z\"\n      confermato_il: \"2030-01-01T00:00:00.000Z\"\n      sostituito_da: 00000000-0000-4000-8000-000000000999\n    decisione_documento:")
	}), true, true)
	c, _ := controlloBanco(r, ControlloTraduzione)
	d := dettaglio(r, ControlloTraduzione).Differenze
	if c.Differenze != 2 || !conPrefisso(d, "baseline_decisioni[0]: decisione_proposta.sostituito_da: quella riga della fotografia non ha il campo") ||
		!conPrefisso(d, "baseline_decisioni[0]: decisione_proposta.confermato_il:") || r.Esito.CodiceUscita() != UscitaConDifferenze {
		t.Errorf("campi che la proposta non ha: %+v %q, %s", c, d, r.PrimaRiga())
	}
	if v := voce(*r.Gate, VoceGateDecisioniPreservate); v.Stato == VoceSuperata {
		t.Errorf("una voce della baseline non tradotta non lascia superata la voce: %+v", v)
	}
	// Lo stesso per un campo della proposta nel documento, e per la riga dell'export della C5.
	for _, v := range []VoceAttesa{
		{Documento: []CampoAtteso{{Nome: "deciso_il", Valore: "x"}}},
		{Export: []CampoAtteso{{Nome: "nome_file", Valore: "x"}}},
	} {
		if e := campiDellaRiga(v); len(e) != 1 {
			t.Errorf("%+v: %q", v, e)
		}
	}
	// Una voce della baseline senza atteso.base: il controllo (2) non avrebbe un valore.
	if _, e := fileAttesoDi(VoceAttesa{Sezione: confronto.SezioneBaseline, Percorso: "b"}, allBaseline, "9123456"); !conPrefisso(e, "la voce non dice atteso.base") {
		t.Errorf("baseline senza atteso.base: %q", e)
	}
}

// ---- R-103: il thread di un caso che la fotografia non ha ----

// casoInventato: un caso del file dei casi per un thread che la fotografia non ha.
func casoInventato(c string) string {
	return strings.Replace(c, `    {
      "id": "ACME-ELENCO",`, `    {
      "id": "ACME-ALTRO",
      "thread_id": "00000000-0000-4000-8000-00000000007e",
      "cliente_id": "00000000-0000-4000-8000-00000000ac01",
      "segmenti": [
        {
          "messaggio_id": "00000000-0000-4000-8000-00000000008e",
          "segmento_id": "s:corrente",
          "uso": "pertinente",
          "origine": "scenario",
          "motivo": "un caso inventato"
        }
      ],
      "autorita": "scenario"
    },
    {
      "id": "ACME-ELENCO",`, 1)
}

// TestR103CasoConIlThreadAssente: un caso che nomina un thread che la fotografia non ha: il file dei casi contraddice i
// dati, uscita 1, con e senza -attesi (prima era 0). Con -thread, un caso di un thread non scelto non si guarda.
func TestR103CasoConIlThreadAssente(t *testing.T) {
	if casoInventato(leggiTestdata(t, "regole/casi_acme.v1.json")) == leggiTestdata(t, "regole/casi_acme.v1.json") {
		t.Fatal("la sostituzione non ha cambiato niente: il caso non prova nulla")
	}
	m := mutaBanco{testi: map[string]func(string) string{"casi": casoInventato}}
	for _, attesi := range []bool{false, true} {
		r := corsaBanco(t, m, attesi, false)
		c, _ := controlloBanco(r, ControlloCasiFoto)
		if c.Differenze != 1 || !conPrefisso(dettaglio(r, ControlloCasiFoto).Differenze, "caso ACME-ALTRO: il thread 00000000-0000-4000-8000-00000000007e non è nella fotografia") ||
			r.Esito.CodiceUscita() != UscitaConDifferenze {
			t.Errorf("attesi %t: %+v, %+v, %s", attesi, c, dettaglio(r, ControlloCasiFoto), r.PrimaRiga())
		}
	}
	r := corsaBanco(t, m, false, false, threadDecisioni)
	if c, _ := controlloBanco(r, ControlloCasiFoto); c.Stato != ControlloEseguito || c.Differenze != 0 {
		t.Errorf("con -thread un caso di un altro thread non si guarda: %+v", c)
	}
	// Un segmento su un messaggio di un altro thread: differenza.
	in := ingressiACME(t)
	s, ix := fotoEIndice(t)
	_ = s
	in.Casi[0].Segmenti[0].MessaggioID = msgDecisioni
	if e := controlloCasiNellaFoto(in, true, ix, contestoCorsa{}, nil, nil); len(e.differenze) != 1 || !strings.Contains(e.differenze[0], "non è del thread del caso") {
		t.Errorf("segmento di un altro thread: %q", e.differenze)
	}
}

// ---- R-104: la corsa parziale senza il thread dello scenario ----

// TestR104ParzialeSenzaScenario: con -thread e il solo thread delle decisioni, lo scenario non si verifica: il n.1 lo
// dichiara (non è una differenza), i conteggi del gate non sono eseguiti, e l'uscita è 3, non 1.
func TestR104ParzialeSenzaScenario(t *testing.T) {
	r := corsaBanco(t, mutaBanco{}, true, true, threadDecisioni)
	c, _ := controlloBanco(r, ControlloScenario)
	if c.Stato != ControlloNonEseguito || c.Differenze != 0 ||
		!conPrefisso(dettaglio(r, ControlloScenario).NonVerificate, "scenario_inoltro.prodotti_confermati_db: il thread dello scenario non è fra quelli scelti") {
		t.Errorf("n.1 nella corsa parziale: %+v, %+v", c, dettaglio(r, ControlloScenario))
	}
	if v := voce(*r.Gate, VoceGateConteggiScenario); v.Stato != VoceNonEseguita {
		t.Errorf("conteggi dello scenario nella corsa parziale: %+v", v)
	}
	if v := voce(*r.Gate, VoceGateFalseAssociazioni); v.Stato != VoceNonEseguita {
		t.Errorf("false associazioni con le voci dello scenario non risolte: %+v", v)
	}
	if r.Differenze != 0 || r.Esito.CodiceUscita() != UscitaNonEseguito {
		t.Fatalf("la corsa parziale legittima esce con 3: %s\n%s", r.PrimaRiga(), r.Testo())
	}
}

// ---- R-105: il predicato dei target confermati senza i componenti ----

// TestR105PredicatoSenzaComponenti: senza la sezione dei componenti (gli export) un thread senza target confermati
// dagli identificativi può averne uno da un finito: il predicato non si decide (parte non verificabile), sia che gli
// attesi dicano vero sia che dicano falso. Un target confermato trovato vale anche senza i componenti.
func TestR105PredicatoSenzaComponenti(t *testing.T) {
	confermato := valut.ProdottoValutato{Rif: "identificativo:9123456", Identita: valut.IdentitaConfermata, Autorita: "confermata"}
	senza := valut.EsitoThread{ThreadID: threadScenario, Valutato: true}
	con := valut.EsitoThread{ThreadID: threadScenario, Valutato: true, ProdottiValutati: []valut.ProdottoValutato{confermato}}
	for _, c := range []struct {
		nome              string
		et                valut.EsitoThread
		atteso, assenti   bool
		differenza, nonFa bool
	}{
		{"nessuno, atteso falso, senza componenti", senza, false, true, false, true},
		{"nessuno, atteso vero, senza componenti", senza, true, true, false, true},
		{"uno, atteso vero, senza componenti", con, true, true, false, false},
		{"uno, atteso falso, senza componenti", con, false, true, true, false},
		{"nessuno, atteso falso, con i componenti", senza, false, false, false, false},
		{"nessuno, atteso vero, con i componenti", senza, true, false, true, false},
	} {
		d, nf := predicatoConfermati(c.et, c.atteso, c.assenti, "p")
		if (d != "") != c.differenza || (nf != "") != c.nonFa {
			t.Errorf("%s: differenza %q, non fatto %q", c.nome, d, nf)
		}
	}
	// Nella corsa sugli export: prodotti_confermati_db vero, e lo scenario senza target confermati: il predicato non si
	// decide, e la parte è fuori perimetro (D-R116 F, B6c): non una differenza, e non una parte che rende il n.1 NON
	// ESEGUITO.
	r := corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "  prodotti_confermati_db: false\n", "  prodotti_confermati_db: true\n")
	}), true, false)
	if c, _ := controlloBanco(r, ControlloScenario); c.Stato != ControlloEseguito || c.Differenze != 0 ||
		!conPrefisso(dettaglio(r, ControlloScenario).FuoriPerimetro, "scenario_inoltro.prodotti_confermati_db: i componenti non sono nella fotografia") {
		t.Errorf("n.1 con gli export: %+v, %+v", c, dettaglio(r, ControlloScenario))
	}
}

// ---- R-106, R-107: la copia del manifest e le tabelle escluse ----

// TestR106ControllaCopia: le decisioni del ramo della copia, una per una.
func TestR106ControllaCopia(t *testing.T) {
	col := migrazioni.Collegamento{Utente: "ruolo_acme_lettura", Database: "copia_acme"}
	piena := dataset.CopiaAttesa{Ruolo: "ruolo_acme_lettura", Database: "copia_acme", Schema: 21, Escluse: []string{"tabella_esclusa_acme"},
		Sentinelle: map[string]int64{"thread": 2}}
	c, fermo, sent := controllaCopia(piena, col, 21)
	if fermo || !c.eseguito || len(c.nonFatti) != 0 || c.controllo().Stato != ControlloEseguito {
		t.Errorf("copia del manifest: %+v (fermo %t)", c, fermo)
	}
	// Riscritta per R117 b (ratificata e ampliata dall'utente il 07/10): le sentinelle dichiarate le verifica A1c-L4D-01
	// sull'ambiente della copia intatta. La riga è delegata, non NON ESEGUITA, con la prova e il database nel motivo.
	if sent == nil || sent.controllo().Stato != ControlloDelegato || sent.controllo().Differenze != 0 ||
		!strings.Contains(sent.motivo, "A1c-L4D-01") || !strings.Contains(sent.motivo, `"copia_acme"`) {
		t.Errorf("sentinelle dichiarate: delegate ad A1c-L4D-01, sull'ambiente della copia del manifest (R117 b): %+v", sent)
	}
	// La delega vale solo per l'ambiente della copia intatta: se il manifest non dichiara il database, l'ambiente non si
	// identifica, e la riga resta NON ESEGUITA.
	senzaDB := piena
	senzaDB.Database = ""
	if _, fermo, sent := controllaCopia(senzaDB, col, 21); fermo || sent == nil || sent.controllo().Stato != ControlloNonEseguito {
		t.Errorf("sentinelle senza il database del manifest: %+v (fermo %t)", sent, fermo)
	}
	// La sezione copia vuota: ogni campo che manca è una parte non eseguita, e la corsa continua.
	c, fermo, sent = controllaCopia(dataset.CopiaAttesa{}, col, 21)
	if fermo || len(c.nonFatti) != 5 || c.controllo().Stato != ControlloNonEseguito || sent != nil {
		t.Errorf("sezione copia vuota: %+v (fermo %t), sentinelle %+v", c, fermo, sent)
	}
	for _, x := range []string{"il manifest non dichiara il ruolo", "il manifest non dichiara il database", "il manifest non dichiara lo schema",
		"il manifest non dichiara le tabelle escluse", "il manifest non dichiara le sentinelle"} {
		if !conPrefisso(c.nonFatti, x) {
			t.Errorf("manca %q in %q", x, c.nonFatti)
		}
	}
	// Ruolo, database o schema diversi: la copia non è quella del manifest, e la corsa si ferma.
	for nome, ultima := range map[string]int{"ruolo": 21, "database": 21, "schema": 20} {
		a := piena
		switch nome {
		case "ruolo":
			a.Ruolo = "altro_ruolo_acme"
		case "database":
			a.Database = "altra_copia_acme"
		}
		c, fermo, _ := controllaCopia(a, col, ultima)
		if !fermo || c.controllo().Stato != ControlloNonEseguito || !strings.Contains(c.motivo, "la copia non è quella del manifest") {
			t.Errorf("%s diverso: %+v (fermo %t)", nome, c, fermo)
		}
	}
}

// TestR107EscluseNonControllate: le tabelle escluse non leggibili si contano solo se il loro controllo c'è stato.
func TestR107EscluseNonControllate(t *testing.T) {
	escluse := []string{"a_acme", "b_acme"}
	if s := solaLetturaDi(migrazioni.Collegamento{Utente: "u"}, errors.New("sola lettura: il ruolo ha privilegi di scrittura"), escluse); s.EscluseNonLeggibili != nil {
		t.Errorf("fermo prima delle escluse: %d", *s.EscluseNonLeggibili)
	}
	if s := solaLetturaDi(migrazioni.Collegamento{Utente: "u"}, nil, escluse); s.EscluseNonLeggibili == nil || *s.EscluseNonLeggibili != 2 {
		t.Errorf("tutto passato: %+v", s)
	}
	if s := solaLetturaDi(migrazioni.Collegamento{EscluseLeggibili: []string{"a_acme"}}, errors.New("sola lettura: escluse leggibili"), escluse); s.EscluseNonLeggibili == nil || *s.EscluseNonLeggibili != 1 {
		t.Errorf("una esclusa leggibile: %+v", s)
	}
	r := RapportoBanco{SolaLettura: solaLetturaDi(migrazioni.Collegamento{Utente: "u"}, errors.New("x"), escluse)}
	if !strings.Contains(r.Testo(), "tabelle escluse non leggibili: non controllate") {
		t.Errorf("il riepilogo: %s", r.Testo())
	}
}

// ---- R-108, R-110: il gate ----

// TestR108ProdottoInPiu: un prodotto del motore che lo scenario non attende non è un prodotto giusto: la voce dei
// conteggi non è superata.
func TestR108ProdottoInPiu(t *testing.T) {
	in := ingressoGateACME()
	in.ProdottiScenario = append(in.ProdottiScenario, confronto.EsitoProdottoAtteso{Base: "9123499", Esito: confronto.EsitoNonCoperto})
	g := calcolaGate(in)
	if v := voce(g, VoceGateConteggiScenario); v.Stato != VoceNonSuperata {
		t.Errorf("un prodotto in più: %+v, %+v", v, g.ConteggiScenario)
	}
	for _, c := range g.ConteggiScenario {
		if c.Nome == "prodotti" && (c.InPiu != 1 || c.Ottenuto != 1) {
			t.Errorf("il conteggio dei prodotti: %+v", c)
		}
	}
}

// TestR110C5Riservata: una voce della C5 con stato_atteso riservato ha l'esito da_rivedere (la C5 è fuori dal gate):
// non è un riservato «passato».
func TestR110C5Riservata(t *testing.T) {
	in := ingressoGateACME()
	in.Voci[5].Riservata = true
	if v := voce(calcolaGate(in), VoceGateRiservati); v.Stato != VoceSuperata {
		t.Errorf("C5 riservata: %+v", v)
	}
	in.Voci[4].Esito = confronto.EsitoCorretto // un reale riservato con un esito diverso da riservato: resta non superata
	if v := voce(calcolaGate(in), VoceGateRiservati); v.Stato != VoceNonSuperata {
		t.Errorf("reale riservato passato: %+v", v)
	}
}

// ---- R-109: le sezioni a forma libera ----

// TestR109SezioniAFormaLibera: le chiavi che non sono ID nei casi d'integrazione, nella conferma dell'albero e nelle
// fonti vanno nell'elenco informativo delle chiavi libere; le righe della conferma dell'albero si dichiarano non
// verificate negli invarianti della C5. Le chiavi libere non cambiano l'uscita.
func TestR109SezioniAFormaLibera(t *testing.T) {
	s, err := LeggiSezioni([]byte("versione_attesi: 1\ncasi_contratto: []\n" +
		"casi_integrazione:\n  - id: x\n    allegato: 00000000-0000-4000-8000-000000000177\n    nota: una nota\n" +
		"fonti:\n  - file: export_acme.json\n    sha256: " + shaACME(1) + "\n" +
		"decisioni_successive_da_rivedere:\n  conferma_albero:\n    relazioni:\n      - padre_id: 00000000-0000-4000-8000-000000000031\n        qta: 3\n      - padre: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.ChiaviLibere, " ") != "casi_integrazione[].allegato decisioni_successive_da_rivedere.conferma_albero.relazioni[].padre decisioni_successive_da_rivedere.conferma_albero.relazioni[].qta fonti[].file" {
		t.Errorf("chiavi libere: %q", s.ChiaviLibere)
	}
	if strings.Join(s.RigheAlbero, " ") != "decisioni_successive_da_rivedere.conferma_albero.relazioni[0] decisioni_successive_da_rivedere.conferma_albero.relazioni[1]" {
		t.Errorf("righe dell'albero: %q", s.RigheAlbero)
	}
	if len(s.NonTradotte) != 0 {
		t.Errorf("le chiavi libere non sono non tradotte: %q", s.NonTradotte)
	}
	// Sulla scena: le chiavi libere (fonti[].file) nel rapporto, nessuna differenza per loro.
	r := corsaBanco(t, mutaBanco{}, true, false)
	if r.Attesi == nil || strings.Join(r.Attesi.ChiaviLibere, " ") != "fonti[].file" || r.Differenze != 0 {
		t.Errorf("chiavi libere nel rapporto: %+v, differenze %d", r.Attesi, r.Differenze)
	}
}

// ---- R-111: la regola dello STEP ----

// TestR111RegolaDelloSTEP: eSTEP con gli stessi casi della regola di valutazione: l'estensione dichiarata vuota ricade
// sul nome, un allegato inline non conta, le maiuscole e il punto non contano.
func TestR111RegolaDelloSTEP(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, c := range []struct {
		a    fotorfq.Allegato
		step bool
	}{
		{fotorfq.Allegato{NomeFile: "9123457A_1.stp", Estensione: str("")}, true},
		{fotorfq.Allegato{NomeFile: "9123457A_1.stp", Estensione: str("stp"), Natura: "inline"}, false},
		{fotorfq.Allegato{NomeFile: "9123457A_1.STEP"}, true},
		{fotorfq.Allegato{NomeFile: "9123457A_1.pdf", Estensione: str(".STP")}, true},
		{fotorfq.Allegato{NomeFile: "9123457A_1.stp", Estensione: str("pdf")}, false},
		{fotorfq.Allegato{NomeFile: "9123457A_1.igs", Natura: "file"}, false},
	} {
		if eSTEP(c.a) != c.step {
			t.Errorf("%+v: %t, atteso %t", c.a, !c.step, c.step)
		}
	}
}

// TestR111ParitaConValutazione: la regola ricopiata è quella del sorgente di valutazione (fonte.go): le stesse
// estensioni, la stessa natura inline saltata, l'estensione vuota che ricade sul nome. Se valutazione cambia la regola,
// questa prova cade (T-B6-118); in B7 valutazione potrà esportare il predicato.
func TestR111ParitaConValutazione(t *testing.T) {
	p := filepath.Join("..", "..", "core", "valutazione", "fonte.go")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var estensioni []string
	inline, estensioneDi := "", ""
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ValueSpec:
			for i, nome := range x.Names {
				if i >= len(x.Values) {
					continue
				}
				switch nome.Name {
				case "estensioniSTEP":
					if cl, ok := x.Values[i].(*ast.CompositeLit); ok {
						for _, el := range cl.Elts {
							if kv, ok := el.(*ast.KeyValueExpr); ok {
								if bl, ok := kv.Key.(*ast.BasicLit); ok {
									s, _ := strconv.Unquote(bl.Value)
									estensioni = append(estensioni, s)
								}
							}
						}
					}
				case "naturaInline":
					if bl, ok := x.Values[i].(*ast.BasicLit); ok {
						inline, _ = strconv.Unquote(bl.Value)
					}
				}
			}
		case *ast.FuncDecl:
			if x.Name.Name == "estensioneDi" {
				estensioneDi = string(src[fset.Position(x.Body.Pos()).Offset:fset.Position(x.Body.End()).Offset])
			}
		}
		return true
	})
	if len(estensioni) != len(estensioniSTEP) {
		t.Errorf("le estensioni dello STEP di valutazione: %v; qui %v", estensioni, estensioniSTEP)
	}
	for _, e := range estensioni {
		if !estensioniSTEP[e] {
			t.Errorf("l'estensione %q di valutazione manca qui", e)
		}
	}
	if inline != naturaInline {
		t.Errorf("la natura inline di valutazione: %q; qui %q", inline, naturaInline)
	}
	if !strings.Contains(estensioneDi, `*a.Estensione != ""`) || !strings.Contains(estensioneDi, "path.Ext(a.NomeFile)") {
		t.Errorf("estensioneDi di valutazione non ricade più sul nome con l'estensione vuota:\n%s", estensioneDi)
	}
	if !strings.Contains(strings.ReplaceAll(string(src), "\r\n", "\n"), "if a.Natura == naturaInline {\n\t\t\tcontinue") {
		t.Errorf("valutazione non salta più gli allegati inline fra gli STEP")
	}
}

// ---- R-113: i tre buchi di prova ----

// TestR113ParzialeShaSbagliato: nella corsa parziale uno sha256 sbagliato resta una differenza (T-B6-111): solo ciò
// che può essere di un altro thread è non verificabile.
func TestR113ParzialeShaSbagliato(t *testing.T) {
	s, ix := fotoEIndice(t)
	f := fotoDellaScena(t, s)
	ins := insiemeACME(t)
	sez := sezioniACME(t, func(r string) string {
		return sostituisci(t, r, "sha256: \"0000000000000000000000000000000000000000000000000000000000000111\"", "sha256: \""+shaACME(0x112)+"\"")
	})
	if c := controlloRisoluzione(sez, traduci(sez, f, ix, &ins), ix, f, true).controllo(); c.Differenze != 1 {
		t.Errorf("sha256 sbagliato nella corsa parziale: %+v", c)
	}
}

// TestR113RiservatiNelGate: le voci riservate della scena (il file reale) arrivano al gate come riservate.
func TestR113RiservatiNelGate(t *testing.T) {
	r := corsaBanco(t, mutaBanco{}, true, false)
	if r.Gate == nil || strings.Join(r.Gate.Riservati, " ") != "reali_archivi[0]" {
		t.Errorf("riservati nel gate: %+v", r.Gate)
	}
}

// TestR113ImprontaDelConfronto: un'impronta di confronto che non è lunga 64 è una differenza (R-48).
func TestR113ImprontaDelConfronto(t *testing.T) {
	if c := controlloConfronto([]string{strings.Repeat("a", 64), ""}).controllo(); c.Differenze != 1 || c.Stato != ControlloEseguito {
		t.Errorf("impronta vuota: %+v", c)
	}
	if c := controlloConfronto([]string{strings.Repeat("a", 64)}).controllo(); c.Differenze != 0 {
		t.Errorf("impronta giusta: %+v", c)
	}
}

// ---- R-114: il flag -thread ----

// TestR114FlagThread: il flag.Value del runner per -thread: ripetibile, un UUID per volta.
func TestR114FlagThread(t *testing.T) {
	var dest []uuid.UUID
	v := FlagThread(&dest)
	if err := v.Set(threadScenario.String()); err != nil {
		t.Fatal(err)
	}
	if err := v.Set(threadDecisioni.String()); err != nil {
		t.Fatal(err)
	}
	if len(dest) != 2 || dest[1] != threadDecisioni || v.String() != threadScenario.String()+","+threadDecisioni.String() {
		t.Errorf("due valori: %v, %q", dest, v.String())
	}
	if err := v.Set("non-un-uuid"); err == nil || len(dest) != 2 {
		t.Errorf("un valore che non è un UUID: %v, %v", err, dest)
	}
}

// ---- R-115: il motivo per un file non valutato ----

// TestR115FileNonValutato: il file della baseline che il motore non valuta (qui un allegato inline): la differenza dice
// «file non valutato», con il motivo del motore, non «la base è diversa».
func TestR115FileNonValutato(t *testing.T) {
	r := corsaBanco(t, mutaBanco{righe: func(r map[string][]riga) {
		for _, a := range r[ExportAllegati] {
			if a["allegato_id"] == allBaseline.String() {
				a["natura"] = "inline"
			}
		}
	}}, true, false)
	d := dettaglio(r, ControlloBaselineRighe).Differenze
	if !conPrefisso(d, "baseline_decisioni[0]: file non valutato: "+valut.MotivoFileNaturaNonFile) || conPrefisso(d, "baseline_decisioni[0]: la base letta nel file non è atteso.base") {
		t.Errorf("differenze della baseline: %q", d)
	}
}

// ---- T-B6-104: gli export con i clienti di un altro DB ----

// TestClientiDegliExport: nessun cliente dei thread ha la ragione sociale negli export: il controllo dei clienti è NON
// ESEGUITO, i thread non si valutano, e ciò che dipende dalla loro valutazione è fra le parti non verificate, mai fra le
// differenze. L'uscita è 3, anche con -gate (prima era 1, per la ragione sbagliata).
func TestClientiDegliExport(t *testing.T) {
	altro := mutaBanco{righe: func(r map[string][]riga) {
		for _, c := range r[ExportClienti] {
			if c["cliente_id"] == clienteACME.String() {
				c["cliente_id"] = uidACME(0xac08).String()
			}
		}
	}}
	r := corsaBanco(t, altro, true, true)
	if c, _ := controlloBanco(r, ControlloClientiExport); c.Stato != ControlloNonEseguito || !strings.Contains(c.Motivo, "2 thread esportati su 2") {
		t.Errorf("controllo dei clienti: %+v", c)
	}
	if !conPrefisso(dettaglio(r, ControlloBaselineRighe).NonVerificate, "baseline_decisioni[0]: atteso.base: il cliente del thread non ha la ragione sociale") {
		t.Errorf("baseline (2): %+v", dettaglio(r, ControlloBaselineRighe))
	}
	if !conPrefisso(dettaglio(r, ControlloScenario).NonVerificate, "scenario_inoltro.prodotti_confermati_db: il cliente del thread") {
		t.Errorf("n.1: %+v", dettaglio(r, ControlloScenario))
	}
	for _, nome := range []string{VoceGateFalseAssociazioni, VoceGateConteggiScenario, VoceGateDecisioniPreservate} {
		if v := voce(*r.Gate, nome); v.Stato != VoceNonEseguita {
			t.Errorf("voce %s: %+v", nome, v)
		}
	}
	if r.Differenze != 0 || r.Esito.CodiceUscita() != UscitaNonEseguito {
		t.Fatalf("i clienti degli export di un altro DB danno 3, non 1: %s\n%s", r.PrimaRiga(), r.Testo())
	}
	// Con l'export dei clienti giusto il controllo è eseguito.
	if c, _ := controlloBanco(corsaBanco(t, mutaBanco{}, false, false), ControlloClientiExport); c.Stato != ControlloEseguito {
		t.Errorf("clienti giusti: %+v", c)
	}
}

// ---- i due ingressi incoerenti dello stesso tipo (T-B6-74, T-B6-112) ----

// TestMessaggioDelCasoNellaCopia: con -dsn un messaggio fuori RFQ dei casi che la copia non ha è una differenza: Carica
// lo dice inesistente, il banco lo toglie e ricarica, e il controllo dei casi lo conta. Un altro errore di Carica resta
// un errore. Con gli export lo stesso messaggio è una parte non verificabile (solo i messaggi in entrata).
func TestMessaggioDelCasoNellaCopia(t *testing.T) {
	chieste := [][]uuid.UUID{}
	carica := func(m []uuid.UUID) (fotorfq.Fotografia, error) {
		chieste = append(chieste, append([]uuid.UUID(nil), m...))
		for _, id := range m {
			if id == msgNonEsportato {
				return fotorfq.Fotografia{}, fmt.Errorf("caricatore: il messaggio %s non esiste", id)
			}
		}
		return fotorfq.Fotografia{Origine: fotorfq.OrigineDSN}, nil
	}
	f, mancanti, err := caricaSenzaIMancanti(carica, []uuid.UUID{msgFuori1, msgNonEsportato})
	if err != nil || f.Origine != fotorfq.OrigineDSN || len(mancanti) != 1 || mancanti[0] != msgNonEsportato || len(chieste) != 2 || len(chieste[1]) != 1 {
		t.Errorf("messaggio inesistente: %v, %v, %v", err, mancanti, chieste)
	}
	_, _, err = caricaSenzaIMancanti(func([]uuid.UUID) (fotorfq.Fotografia, error) {
		return fotorfq.Fotografia{}, errors.New("caricatore: RFQ x, fatti: connessione chiusa")
	}, []uuid.UUID{msgFuori1})
	if err == nil {
		t.Error("un altro errore di Carica resta un errore")
	}
	in := ingressiACME(t)
	_, ix := fotoEIndice(t)
	if e := controlloCasiNellaFoto(in, true, ix, contestoCorsa{}, []uuid.UUID{msgFuori2}, nil); len(e.differenze) != 1 || len(e.nonFatti) != 0 {
		t.Errorf("con -dsn: %+v", e)
	}
	if e := controlloCasiNellaFoto(in, true, ix, contestoCorsa{export: true}, nil, []uuid.UUID{msgFuori2}); len(e.differenze) != 0 || len(e.nonFatti) != 1 {
		t.Errorf("con gli export: %+v", e)
	}
}

// TestFormatoDelMessaggioInesistente: il testo che il banco riconosce è quello del caricatore (che non ha un errore
// sentinella): se il caricatore lo cambia, questa prova cade.
func TestFormatoDelMessaggioInesistente(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "core", "fotorfq", "caricatore", "caricatore.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), strconv.Quote(formatoMessaggioInesistente)) {
		t.Errorf("il caricatore non scrive più %q", formatoMessaggioInesistente)
	}
}

// ---- il n.3 nelle due direzioni, nome_file, il badge della C5, le righe riportate ----

// TestFontiNelleDueDirezioni: un export che le fonti elencano ma che il manifest non ha è una differenza del n.3.
func TestFontiNelleDueDirezioni(t *testing.T) {
	r := corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "fonti:\n", "fonti:\n  - file: export_assente_acme.json\n    sha256: "+shaACME(0xf0)+"\n")
	}), true, false)
	if c, _ := controlloBanco(r, ControlloFonti); c.Differenze != 1 ||
		!conPrefisso(dettaglio(r, ControlloFonti).Differenze, "uno sha256 delle fonti degli attesi non è fra gli export del manifest") {
		t.Errorf("fonte senza export: %+v, %+v", c, dettaglio(r, ControlloFonti))
	}
}

// TestNomeFileVerificato: nome_file si verifica contro l'allegato (n.2).
func TestNomeFileVerificato(t *testing.T) {
	r := corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "      nome_file: 9123456A_2.pdf\n", "      nome_file: 9123456A_3.pdf\n")
	}), true, false)
	if c, _ := controlloBanco(r, ControlloRisoluzione); c.Differenze != 1 ||
		!conPrefisso(dettaglio(r, ControlloRisoluzione).Differenze, "scenario_inoltro.file[0].nome_file: non è il nome dell'allegato") {
		t.Errorf("nome_file diverso: %+v", c)
	}
}

// TestBadgeDellaC5: mostrata_con_badge_di_confronto si verifica: il file della C5 ha la sua riga di confronto.
func TestBadgeDellaC5(t *testing.T) {
	r := corsaBanco(t, attesiMutati(func(a string) string {
		return sostituisci(t, a, "      mostrata_con_badge_di_confronto: true\n", "      mostrata_con_badge_di_confronto: false\n")
	}), true, false)
	if c, _ := controlloBanco(r, ControlloInvariantiC5); c.Differenze != 1 {
		t.Errorf("badge atteso falso: %+v", c)
	}
}

// TestC5LaRigaDellAltraModalita: la C5 confronta con -dsn la decisione, con -exports la riga dell'export; la riga
// dell'altra modalità non si applica a questa corsa (R-117: un elenco informativo, non le parti non verificate). Le righe
// della conferma dell'albero non sono più fra le parti non verificate: stanno nella sezione informativa del rapporto
// (D-R116 I, B6c). La fotografia della scena serve per tutte e due: qui conta che cosa il controllo confronta e che cosa
// dichiara.
func TestC5LaRigaDellAltraModalita(t *testing.T) {
	s, ix := fotoEIndice(t)
	f := fotoDellaScena(t, s)
	ins := insiemeACME(t)
	sez := sezioniACME(t, nil)
	tr := traduci(sez, f, ix, &ins)
	badge := map[uuid.UUID]confronto.Badge{allDaRivedere: confronto.BadgeUguale}
	dsn := controlloInvariantiC5(&sez, tr, f, ix, contestoCorsa{}, badge)
	if len(dsn.differenze) != 0 || len(dsn.nonFatti) != 0 || len(dsn.nonApplicabili) != 1 ||
		!conPrefisso(dsn.nonApplicabili, "decisioni_successive_da_rivedere.file_confermati[0].nell_export_proposte: si verifica con -exports") {
		t.Errorf("con -dsn: differenze %q, non fatti %q, non applicabili %q", dsn.differenze, dsn.nonFatti, dsn.nonApplicabili)
	}
	exp := controlloInvariantiC5(&sez, tr, f, ix, contestoCorsa{export: true}, badge)
	if len(exp.differenze) != 0 || len(exp.nonFatti) != 0 || len(exp.nonApplicabili) != 2 ||
		!conPrefisso(exp.nonApplicabili, "decisioni_successive_da_rivedere.file_confermati[0].decisione_proposta: si verifica con -dsn") {
		t.Errorf("con gli export: differenze %q, non fatti %q, non applicabili %q", exp.differenze, exp.nonFatti, exp.nonApplicabili)
	}
	// Le parti non applicabili non cambiano lo stato: senza le righe dell'albero il controllo è eseguito.
	senzaAlbero := sez
	senzaAlbero.RigheAlbero = nil
	if c := controlloInvariantiC5(&senzaAlbero, tr, f, ix, contestoCorsa{export: true}, badge).controllo(); c.Stato != ControlloEseguito || c.Differenze != 0 {
		t.Errorf("solo le parti non applicabili: %+v", c)
	}
	// Senza la riga di confronto del file, il badge atteso non c'è: una differenza.
	if e := controlloInvariantiC5(&sez, tr, f, ix, contestoCorsa{}, nil); len(e.differenze) != 1 {
		t.Errorf("badge assente: %q", e.differenze)
	}
}

// TestRighePortateAccantoAgliEsiti: l'etichetta e le anomalie della C5 stanno nel rapporto, accanto all'esito del file;
// l'id della voce degli attesi sta accanto al percorso, negli esiti, nella baseline e nell'indice delle voci (R-119).
func TestRighePortateAccantoAgliEsiti(t *testing.T) {
	r := corsaBanco(t, mutaBanco{}, true, false)
	ids := map[string]string{}
	for _, th := range r.Thread {
		for _, f := range th.File {
			for _, e := range f.Esiti {
				if e.Sezione == confronto.SezioneDaRivedere && strings.Join(e.Riportate, "; ") != "etichetta = da rivedere; anomalia = A-ACME-1" {
					t.Errorf("righe riportate della C5: %q", e.Riportate)
				}
				if e.Percorso != "" {
					ids[e.Percorso] = e.ID
				}
			}
		}
	}
	for p, id := range map[string]string{"scenario_inoltro.file[0]": "file-acme-01", "baseline_decisioni[0]": "baseline-acme-01",
		"reali_archivi[0]": "reale-acme-01", "decisioni_successive_da_rivedere.file_confermati[0]": "rivedere-acme-01"} {
		if ids[p] != id {
			t.Errorf("l'id accanto al percorso %s negli esiti: %q, atteso %q", p, ids[p], id)
		}
	}
	if r.Attesi == nil || len(r.Attesi.Baseline) != 1 || r.Attesi.Baseline[0].ID != "baseline-acme-01" || len(r.Attesi.IDVoci) != 6 ||
		r.Attesi.IDVoci[0] != (IDVoce{Percorso: "scenario_inoltro.file[0]", ID: "file-acme-01"}) {
		t.Errorf("l'id nella baseline e nell'indice delle voci: %+v", r.Attesi)
	}
}

// TestR118ScrittureConsentiteNelGate: scritture_consentite dello scenario non è una lettura: la dichiara la voce del gate
// «zero scritture di dominio», che aspetta già il registro, e non il controllo delle letture (R-118).
func TestR118ScrittureConsentiteNelGate(t *testing.T) {
	r := corsaBanco(t, mutaBanco{}, true, false)
	if conPrefisso(dettaglio(r, ControlloLetture).NonVerificate, "scenario_inoltro.file[0].scritture_consentite") {
		t.Errorf("scritture_consentite fra le letture: %+v", dettaglio(r, ControlloLetture))
	}
	if v := voce(*r.Gate, VoceGateZeroScritture); v.Stato != VoceNonEseguita ||
		!strings.Contains(v.Motivo, "1 voci dello scenario con scritture_consentite") || !strings.Contains(v.Motivo, "scenario_inoltro.file[0]") {
		t.Errorf("la voce del gate: %+v", v)
	}
	in := ingressoGateACME()
	in.SolaLetturaOk, in.SolaLetturaMotivo, in.ScrittureConsentite = false, "il collegamento può scrivere", []string{"s[0]"}
	if v := voce(calcolaGate(in), VoceGateZeroScritture); v.Stato != VoceNonSuperata || !strings.Contains(v.Motivo, "scritture_consentite") {
		t.Errorf("con il collegamento che scrive: %+v", v)
	}
}
