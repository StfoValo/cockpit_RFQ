package bancoa

import (
	"context"
	"strings"
	"testing"

	"promatec/cockpit/internal/core/registro/regole/grammatica"
	valut "promatec/cockpit/internal/core/valutazione"
)

// L1 — A1c-L1-24: i controlli del runner (piano 6.4.9, n.1–5) su attesi, casi, indice e fotografia sintetici:
//   - n.1: gli ingressi del caso dello scenario diversi dalla sezione dello scenario (thread senza caso, messaggio,
//     autorità, prodotti confermati con il predicato di R70 A: T-B0-15);
//   - n.2: un ID o uno sha256 degli attesi che non si risolve, un file in due sezioni (D7); con i soli thread scelti,
//     ciò che non si risolve non è una differenza ma una parte non verificabile;
//   - n.3: lo sha256 di un export che non è fra le fonti degli attesi;
//   - n.4: il file dei casi del manifest diverso da quello dell'indice, o un indice senza il puntatore;
//   - n.5: gli attesi con una chiave sconosciuta o con conteggi diversi dalle voci.
//
// Un controllo fallito è una differenza (uscita 1), mai un «mancante»; un ingresso che manca o ha un'impronta diversa
// dal manifest dà NON ESEGUITO (uscita 3).
//
// I clienti di questi test sono inventati: vedi scena_banco_test.go.

// ingressiACME: il file dei casi della scena.
func ingressiACME(t *testing.T) valut.Ingressi {
	t.Helper()
	in, err := valut.LeggiIngressi([]byte(leggiTestdata(t, "regole/casi_acme.v1.json")))
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// esitoConProdotti: un esito con il thread dello scenario e i suoi prodotti valutati.
func esitoConProdotti(pv ...valut.ProdottoValutato) *valut.Esito {
	return &valut.Esito{Thread: []valut.EsitoThread{{ThreadID: threadScenario, Valutato: true, ProdottiValutati: pv}}}
}

func TestControlloScenario(t *testing.T) {
	in := ingressiACME(t)
	base := func() *ScenarioAtteso { return sezioniACME(t, nil).Scenario }
	confermato := valut.ProdottoValutato{Rif: "identificativo:9123456", Identita: valut.IdentitaConfermata, Autorita: "confermata"}
	scenario := valut.ProdottoValutato{Rif: "scenario:9123456", Identita: valut.IdentitaDaConfermare, Autorita: "scenario"}

	if e := controlloScenario(base(), in, true, esitoConProdotti(scenario), contestoCorsa{}).controllo(); e.Stato != ControlloEseguito || e.Differenze != 0 {
		t.Fatalf("ingressi coincidenti: %+v", e)
	}
	diversi := map[string]struct {
		s     func() *ScenarioAtteso
		in    valut.Ingressi
		esito *valut.Esito
	}{
		"thread senza caso": {func() *ScenarioAtteso { s := base(); u := uidACME(0x79); s.ThreadID = &u; return s }, in, esitoConProdotti()},
		"messaggio non del caso": {func() *ScenarioAtteso {
			s := base()
			u := msgDecisioni
			s.MessaggioID = &u
			return s
		}, in, esitoConProdotti()},
		"autorità diversa":             {func() *ScenarioAtteso { s := base(); s.AutoritaTarget = "confermata"; return s }, in, esitoConProdotti()},
		"autorità non tradotta":        {func() *ScenarioAtteso { s := base(); s.AutoritaTarget = "inventata"; return s }, in, esitoConProdotti()},
		"prodotti confermati":          {base, in, esitoConProdotti(confermato)},
		"confermati attesi":            {func() *ScenarioAtteso { s := base(); v := true; s.ProdottiConfermatiDB = &v; return s }, in, esitoConProdotti(scenario)},
		"senza prodotti_confermati_db": {func() *ScenarioAtteso { s := base(); s.ProdottiConfermatiDB = nil; return s }, in, esitoConProdotti()},
		"thread con un errore della valutazione": {base, in, &valut.Esito{Thread: []valut.EsitoThread{{ThreadID: threadScenario,
			Motivo: valut.MotivoThreadErroreValutazione}}}},
		"thread fuori dalla fotografia": {base, in, &valut.Esito{}},
	}
	for nome, c := range diversi {
		t.Run(nome, func(t *testing.T) {
			e := controlloScenario(c.s(), c.in, true, c.esito, contestoCorsa{}).controllo()
			if e.Stato != ControlloEseguito || e.Differenze == 0 {
				t.Fatalf("uscita 1, mai «mancante»: %+v", e)
			}
		})
	}
	if e := controlloScenario(base(), in, false, nil, contestoCorsa{}).controllo(); e.Stato != ControlloNonEseguito {
		t.Errorf("senza il file dei casi: %+v", e)
	}
	if e := controlloScenario(nil, in, true, nil, contestoCorsa{}).controllo(); e.Stato != ControlloEseguito || e.Differenze != 0 {
		t.Errorf("senza scenario negli attesi: %+v", e)
	}
}

func TestControlloRisoluzione(t *testing.T) {
	s, ix := fotoEIndice(t)
	f := fotoDellaScena(t, s)
	ins := insiemeACME(t)
	esegui := func(muta func(string) string, parziale bool) Controllo {
		sez := sezioniACME(t, muta)
		return controlloRisoluzione(sez, traduci(sez, f, ix, &ins), ix, f, parziale).controllo()
	}
	if c := esegui(nil, false); c.Stato != ControlloEseguito || c.Differenze != 0 {
		t.Fatalf("tutto si risolve: %+v", c)
	}
	differenze := map[string]func(string) string{
		"allegato assente": func(r string) string {
			return strings.Replace(r, "allegato_id: 00000000-0000-4000-8000-000000000103", "allegato_id: 00000000-0000-4000-8000-000000000177", 1)
		},
		"sha256 diverso": func(r string) string {
			return strings.Replace(r, "sha256: \"0000000000000000000000000000000000000000000000000000000000000111\"", "sha256: \""+shaACME(0x112)+"\"", 1)
		},
		"file in due sezioni": func(r string) string {
			return strings.Replace(r, "allegato_id: 00000000-0000-4000-8000-000000000104", "allegato_id: 00000000-0000-4000-8000-000000000103", 1)
		},
		"file dello scenario in un altro thread": func(r string) string {
			return strings.Replace(r, "allegato_id: 00000000-0000-4000-8000-000000000103", "allegato_id: 00000000-0000-4000-8000-000000000111", 1)
		},
		"documento dell'albero assente": func(r string) string {
			return strings.Replace(r, "documento_id: 00000000-0000-4000-8000-000000000222\n    allegati:", "documento_id: 00000000-0000-4000-8000-000000000277\n    allegati:", 1)
		},
		"sha256 dell'integrazione sconosciuto": func(r string) string {
			return strings.Replace(r, "0000000000000000000000000000000000000000000000000000000000000101\"\nriservati", shaACME(0x1777)+"\"\nriservati", 1)
		},
		"messaggio dello scenario non del thread": func(r string) string {
			return strings.Replace(r, "  messaggio_id: 00000000-0000-4000-8000-000000000081", "  messaggio_id: 00000000-0000-4000-8000-000000000082", 1)
		},
	}
	for nome, muta := range differenze {
		t.Run(nome, func(t *testing.T) {
			if muta(leggiTestdata(t, "attesi_acme.yaml")) == leggiTestdata(t, "attesi_acme.yaml") {
				t.Fatal("la sostituzione non ha cambiato niente: il caso non prova nulla")
			}
			if c := esegui(muta, false); c.Stato != ControlloEseguito || c.Differenze == 0 {
				t.Fatalf("uscita 1, mai «mancante»: %+v", c)
			}
		})
	}
	// Con i soli thread scelti, un allegato che non c'è può essere di un altro thread: non verificabile, non differenza.
	c := esegui(differenze["allegato assente"], true)
	if c.Stato != ControlloNonEseguito || c.Differenze != 0 {
		t.Errorf("parziale: %+v", c)
	}
	// I componenti non si verificano se la sezione è assente (gli export).
	sez := sezioniACME(t, func(r string) string {
		return strings.Replace(r, "    documenti:\n      - documento_id:", "    componenti:\n      - componente_id: 00000000-0000-4000-8000-000000000031\n    documenti:\n      - documento_id:", 1)
	})
	e := controlloRisoluzione(sez, traduci(sez, f, ix, &ins), ix, f, false)
	if len(e.nonFatti) != 1 || len(e.differenze) != 0 {
		t.Errorf("componente con la sezione assente: non fatti %v, differenze %v", e.nonFatti, e.differenze)
	}
}

func TestControlloFontiCasiEAttesi(t *testing.T) {
	// n.3
	s := sezioniACME(t, nil)
	s.Fonti = []string{shaACME(1), shaACME(2)}
	if c := controlloFonti(&s, []string{shaACME(1), shaACME(2)}).controllo(); c.Differenze != 0 || c.Stato != ControlloEseguito {
		t.Errorf("fonti coincidenti: %+v", c)
	}
	// Un export fuori dalle fonti, e una fonte fuori dal manifest: le due direzioni (6.4.9: «coincidono»).
	if c := controlloFonti(&s, []string{shaACME(1), shaACME(3)}).controllo(); c.Differenze != 2 {
		t.Errorf("un export fuori dalle fonti e una fonte fuori dagli export: %+v", c)
	}
	if c := controlloFonti(nil, []string{shaACME(1)}).controllo(); c.Stato != ControlloNonEseguito {
		t.Errorf("attesi non letti: %+v", c)
	}
	// n.4
	ix := &grammatica.IndiceRegole{Casi: &grammatica.FileIndice{File: "casi_acme.v1.json", Sha256: strings.ToUpper(shaACME(7))}}
	if c := controlloCasiIndice(shaACME(7), ix).controllo(); c.Differenze != 0 || c.Stato != ControlloEseguito {
		t.Errorf("stesso file dei casi: %+v", c)
	}
	if c := controlloCasiIndice(shaACME(8), ix).controllo(); c.Differenze != 1 {
		t.Errorf("file dei casi diverso: %+v", c)
	}
	if c := controlloCasiIndice(shaACME(7), &grammatica.IndiceRegole{}).controllo(); c.Differenze != 1 {
		t.Errorf("indice senza il puntatore ai casi: %+v", c)
	}
	if c := controlloCasiIndice("", ix).controllo(); c.Stato != ControlloNonEseguito {
		t.Errorf("casi non letti: %+v", c)
	}
	// n.5
	if c := controlloAttesiInteri(true, "", &s).controllo(); c.Differenze != 0 || c.Stato != ControlloEseguito {
		t.Errorf("attesi interi: %+v", c)
	}
	conteggi := sezioniACME(t, func(r string) string {
		return strings.Replace(strings.Replace(r, "    radice: 1\n", "    radice: 2\n", 1), "    prodotti: 1\n", "    prodotti: 1\n    gruppi: 4\n", 1)
	})
	if c := controlloAttesiInteri(true, "", &conteggi).controllo(); c.Differenze != 2 {
		t.Errorf("un conteggio diverso e uno che il runner non sa contare: %+v", c)
	}
	if c := controlloAttesiInteri(false, "file mancante", nil).controllo(); c.Stato != ControlloNonEseguito {
		t.Errorf("attesi assenti: %+v", c)
	}
	if c := controlloAttesiInteri(false, "chiave sconosciuta", &s).controllo(); c.Differenze != 1 {
		t.Errorf("attesi che LeggiAttesi rifiuta: %+v", c)
	}
}

// nonVerificateACME: le parti che la scena ACME sugli export non può verificare, controllo per controllo (la regola
// delle chiavi accettate: dichiarate, mai buttate). Sono limiti della modalità, non differenze:
//   - letture e invarianti: target_presente (i componenti mancano). La base dell'archivio dei reali è quella del nome,
//     che si legge per intero anche con l'interpretazione parziale, e si giudica (D-R116 O, B6c), come
//     revisione_dal_nome (R-116). scritture_consentite sta nella voce del gate «zero scritture» (R-118).
//
// Dopo le decisioni dell'08/10 (B6c) non sono più qui: il n.1 senza i finiti, fuori perimetro con gli export (D-R116 F:
// fra le parti fuori perimetro dei dettagli); le righe della conferma dell'albero, nella sezione informativa del rapporto
// (D-R116 I). La decisione del dump della C5 non si applica agli export (R-117: nonApplicabiliACME).
var nonVerificateACME = map[string][]string{
	ControlloLetture: {
		"reali_archivi[0].target_presente: i componenti non sono nella fotografia",
	},
}

// nonApplicabiliACME: le parti che sugli export non si applicano (R-117): la decisione del dump della C5.
var nonApplicabiliACME = []string{
	"decisioni_successive_da_rivedere.file_confermati[0].decisione_proposta: si verifica con -dsn",
	"decisioni_successive_da_rivedere.file_confermati[0].decisione_documento: si verifica con -dsn",
}

// controllaNonVerificate: i dettagli dei controlli coincidono con le parti attese, una per una, nell'ordine.
func controllaNonVerificate(t *testing.T, r RapportoBanco, attese map[string][]string) {
	t.Helper()
	visti := map[string]bool{}
	for _, d := range r.Dettagli {
		visti[d.Nome] = true
		if len(d.Differenze) > 0 {
			t.Errorf("%s: differenze %v", d.Nome, d.Differenze)
		}
		a := attese[d.Nome]
		if len(a) != len(d.NonVerificate) {
			t.Errorf("%s: non verificate %q, attese %q", d.Nome, d.NonVerificate, a)
			continue
		}
		for i := range a {
			if !strings.HasPrefix(d.NonVerificate[i], a[i]) {
				t.Errorf("%s[%d]: %q, attesa %q…", d.Nome, i, d.NonVerificate[i], a[i])
			}
		}
	}
	for nome := range attese {
		if !visti[nome] {
			t.Errorf("%s: le parti non verificate attese mancano dal rapporto", nome)
		}
	}
}

// TestControlliDelRunnerNelBanco: i controlli nella corsa vera sugli export: sulla scena nessun controllo ha
// differenze, e l'esito è NON ESEGUITO per le sole parti che gli export non permettono di verificare (dichiarate una
// per una nei dettagli); una differenza del runner dà 1 (mai un «mancante»), un file del manifest cambiato dà 3.
func TestControlliDelRunnerNelBanco(t *testing.T) {
	r, err := EseguiBanco(context.Background(), preparaBanco(t, mutaBanco{}).opzioniExport(true), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, nome := range []string{ControlloScenario, ControlloRisoluzione, ControlloFonti, ControlloCasiIndice, ControlloAttesiInteri,
		ControlloTraduzione, ControlloBaselineRighe, ControlloInvariantiC5, ControlloLetture, ControlloCasiFoto, ControlloClientiExport} {
		c, ok := controlloBanco(r, nome)
		stato := ControlloEseguito
		if nonVerificateACME[nome] != nil {
			stato = ControlloNonEseguito
		}
		if !ok || c.Stato != stato || c.Differenze != 0 {
			t.Errorf("controllo %s: %+v (presente %t), atteso %s senza differenze", nome, c, ok, stato)
		}
	}
	controllaNonVerificate(t, r, nonVerificateACME)
	if na := dettaglio(r, ControlloInvariantiC5).NonApplicabili; strings.Join(na, "|") != strings.Join(nonApplicabiliACME, "|") {
		t.Errorf("parti non applicabili della C5: %q", na)
	}
	if r.Esito != EsitoNonEseguito || r.Differenze != 0 {
		t.Fatalf("la scena non ha differenze, e gli export lasciano parti non verificate: %s\n%s", r.PrimaRiga(), r.Testo())
	}

	// n.4: l'indice che punta a un altro file dei casi.
	s := preparaBanco(t, mutaBanco{testi: map[string]func(string) string{"indice": func(ix string) string {
		i := strings.LastIndex(ix, `"sha256": "`)
		return ix[:i] + `"sha256": "` + shaACME(0xbad) + ix[i+len(`"sha256": "`)+64:]
	}}})
	r, err = EseguiBanco(context.Background(), s.opzioniExport(true), nil)
	if c, _ := controlloBanco(r, ControlloCasiIndice); err != nil || c.Differenze != 1 || r.Esito.CodiceUscita() != UscitaConDifferenze {
		t.Errorf("n.4: %+v, %s (%v)", c, r.PrimaRiga(), err)
	}

	// Un file dei casi cambiato dopo il manifest: NON ESEGUITO.
	s = preparaBanco(t, mutaBanco{testi: map[string]func(string) string{"manifest": func(m string) string {
		return strings.Replace(m, `"percorso": "regole/casi_acme.v1.json",`, `"percorso": "regole/casi_acme.v1.json",`+"\n      ", 1)
	}}})
	// (il manifest cambia solo nella forma: lo sha256 del file dei casi resta giusto) — poi il file si cambia sul disco.
	writeFile(t, s.percorso("regole/casi_acme.v1.json"), leggiTestdata(t, "regole/casi_acme.v1.json")+" ")
	r, err = EseguiBanco(context.Background(), s.opzioniExport(true), nil)
	if c, _ := controlloBanco(r, VoceCasi); err != nil || c.Stato != ControlloNonEseguito || r.Esito.CodiceUscita() != UscitaNonEseguito {
		t.Errorf("file dei casi cambiato: %+v, %s (%v)", c, r.PrimaRiga(), err)
	}
	if c, _ := controlloBanco(r, ControlloScenario); c.Stato != ControlloNonEseguito {
		t.Errorf("senza il file dei casi il n.1 non si esegue: %+v", c)
	}
}

// TestBaselineEC5NelBanco (6.4.9: i controlli (1) e (2) della baseline e gli invarianti della C5): una riga attesa che
// non coincide con la fotografia, una base letta diversa da atteso.base, un invariante della C5 che non torna sono
// differenze; con gli export la C5 si confronta con la riga dell'export.
func TestBaselineEC5NelBanco(t *testing.T) {
	casi := map[string]struct {
		muta      func(string) string
		controllo string
	}{
		"codice della proposta della baseline": {func(a string) string {
			return strings.Replace(a, "      codice: 9123456A\n      rev: \"2\"\n      stato: confermata\n", "      codice: 9123499A\n      rev: \"2\"\n      stato: confermata\n", 1)
		}, ControlloBaselineRighe},
		"sostituito_da della baseline": {func(a string) string {
			return strings.Replace(a, "      sostituito_da: null\n", "      sostituito_da: 00000000-0000-4000-8000-000000000999\n", 1)
		}, ControlloBaselineRighe},
		"base letta della baseline": {func(a string) string {
			return strings.Replace(a, "0111\"\n    atteso:\n      base: 9123456\n", "0111\"\n    atteso:\n      base: 9123499\n", 1)
		}, ControlloBaselineRighe},
		"componente della proposta preservato": {func(a string) string {
			return strings.Replace(a, "    componente_id_proposta_vuoto_preservato: false\nreali_archivi:", "    componente_id_proposta_vuoto_preservato: true\nreali_archivi:", 1)
		}, ControlloBaselineRighe},
		"C5 nella baseline": {func(a string) string {
			return strings.Replace(a, "      in_baseline: false\n", "      in_baseline: true\n", 1)
		}, ControlloInvariantiC5},
		"C5 nel gate": {func(a string) string {
			return strings.Replace(a, "      conta_nel_gate: false\n", "      conta_nel_gate: true\n", 1)
		}, ControlloInvariantiC5},
		"C5 sovrascritta": {func(a string) string {
			return strings.Replace(a, "      sovrascritta_dal_motore: false\n", "      sovrascritta_dal_motore: true\n", 1)
		}, ControlloInvariantiC5},
		"C5 diversa dall'export": {func(a string) string {
			return strings.Replace(a, "        stato: confermata\n        codice: 9123459A\n", "        stato: aperta\n        codice: 9123459A\n", 1)
		}, ControlloInvariantiC5},
	}
	for nome, c := range casi {
		t.Run(nome, func(t *testing.T) {
			if c.muta(leggiTestdata(t, "attesi_acme.yaml")) == leggiTestdata(t, "attesi_acme.yaml") {
				t.Fatal("la sostituzione non ha cambiato niente: il caso non prova nulla")
			}
			s := preparaBanco(t, mutaBanco{testi: map[string]func(string) string{"attesi": c.muta}})
			r, err := EseguiBanco(context.Background(), s.opzioniExport(true), nil)
			if err != nil {
				t.Fatal(err)
			}
			if x, _ := controlloBanco(r, c.controllo); x.Differenze == 0 || r.Esito.CodiceUscita() != UscitaConDifferenze {
				t.Fatalf("%s: %+v, %s", c.controllo, x, r.PrimaRiga())
			}
			if c.controllo == ControlloBaselineRighe && (r.Gate == nil || r.Gate.DecisioniPreservate != 0) {
				t.Errorf("la decisione non è preservata nel gate: %+v", r.Gate)
			}
		})
	}
}
