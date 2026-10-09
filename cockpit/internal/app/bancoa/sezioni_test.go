package bancoa

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/platform/dataset"
)

// L1 — A1c-L1-23: LeggiSezioni su un YAML sintetico con tutte le sezioni (testdata/attesi_acme.yaml, E-13), con i nomi
// neutri delle sezioni e delle chiavi (R47 a, M-21); una chiave che il runner non conosce non si ignora: va fra le non
// tradotte, con il percorso senza valori, e il controllo n.5 la conta; un valore che non si legge va fra gli errori; la
// tabella di traduzione (R19 a-b, C-30, R25): atteso radice | figlio | fuori, candidato_scenario, fuori_scenario,
// radici, l'autorità scenario_richiesta_inoltrata → scenario, la sezione con il nome neutro (P-10), T della baseline
// dalla decisione letta con la grammatica, i file reali riservati, la C5 da_rivedere; le contraddizioni di una voce
// (D8, R-51, una radice vuota) sono errori di traduzione, mai «nessun ancoraggio atteso». LeggiAttesi di A1a accetta
// lo stesso file.
//
// I clienti di questi test sono inventati: vedi scena_banco_test.go.

func sezioniACME(t *testing.T, muta func(string) string) SezioniAttesi {
	t.Helper()
	raw := leggiTestdata(t, "attesi_acme.yaml")
	if muta != nil {
		raw = muta(raw)
	}
	s, err := LeggiSezioni([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLeggiSezioniACME(t *testing.T) {
	if _, err := LeggiAttesi([]byte(leggiTestdata(t, "attesi_acme.yaml"))); err != nil {
		t.Fatalf("LeggiAttesi di A1a sullo stesso file: %v", err)
	}
	s := sezioniACME(t, nil)
	if len(s.NonTradotte) != 0 || len(s.Errori) != 0 {
		t.Fatalf("non tradotte %v, errori %v", s.NonTradotte, s.Errori)
	}
	sc := s.Scenario
	if sc == nil || *sc.ThreadID != threadScenario || *sc.MessaggioID != msgScenario || sc.AutoritaTarget != "scenario_richiesta_inoltrata" ||
		sc.ProdottiConfermatiDB == nil || *sc.ProdottiConfermatiDB {
		t.Fatalf("scenario: %+v", sc)
	}
	if len(sc.Prodotti) != 1 || sc.Prodotti[0].Base != "9123456" || sc.Prodotti[0].Originale != "P9123456" || *sc.Prodotti[0].Quantita != 5 ||
		sc.Prodotti[0].FonteQuantita != "cella" {
		t.Errorf("prodotti attesi: %+v", sc.Prodotti)
	}
	var nomi []string
	for _, c := range sc.Conteggi {
		nomi = append(nomi, c.Nome)
	}
	if strings.Join(nomi, " ") != "figlio file fuori_scenario prodotti radice" {
		t.Errorf("conteggi in ordine di nome: %v", nomi)
	}
	if len(sc.File) != 3 || sc.File[0].Atteso != "radice" || sc.File[0].AttesoStato != "candidato_scenario" || sc.File[0].Sezione != confronto.SezioneScenario ||
		sc.File[1].Atteso != "figlio" || strings.Join(sc.File[1].Radici, ",") != "9123456" ||
		sc.File[2].AttesoStato != "fuori_scenario" || !sc.File[2].TargetBaseScritto || sc.File[2].TargetBase != "" {
		t.Errorf("file dello scenario: %+v", sc.File)
	}
	if v, ok := sc.File[0].invariante("scritture_consentite"); !ok || v {
		t.Error("scritture_consentite falso, fra gli invarianti")
	}
	if len(s.Baseline) != 1 || s.Baseline[0].Sezione != confronto.SezioneBaseline || len(s.Baseline[0].Proposta) != 5 || len(s.Baseline[0].Documento) != 3 {
		t.Fatalf("baseline: %+v", s.Baseline)
	}
	if b, ok := s.Baseline[0].lettura("base"); !ok || b != "9123456" {
		t.Errorf("atteso.base della baseline: %q", b)
	}
	if c, ok := campo(s.Baseline[0].Documento, "sostituito_da"); !ok || !c.Nullo {
		t.Errorf("sostituito_da null: %+v", c)
	}
	if len(s.Reali) != 1 || s.Reali[0].Sezione != confronto.SezioneReali {
		t.Errorf("reali: %+v", s.Reali)
	}
	if len(s.DaRivedere) != 1 || len(s.DaRivedere[0].Export) != 2 || len(s.DaRivedere[0].Invarianti) != 5 || len(s.DaRivedere[0].Riportate) != 2 {
		t.Errorf("da rivedere: %+v", s.DaRivedere)
	}
	// Le righe riportate: la nota dello scenario e le due righe dei riservati non bloccanti.
	if len(s.Albero) != 2 || len(s.Integrazione) != 2 || len(s.Riportate) != 3 || len(s.Fonti) != 0 {
		t.Errorf("albero %v, integrazione %v, riportate %v, fonti %v", s.Albero, s.Integrazione, s.Riportate, s.Fonti)
	}
	if s.Riportate[0].Percorso != "scenario_inoltro.nota" {
		t.Errorf("la nota dello scenario fra le righe riportate: %+v", s.Riportate)
	}
	// Le righe della conferma dell'albero (R-109) e nessuna chiave libera: le voci dei casi d'integrazione hanno solo
	// id, allegato_id e sha256.
	if strings.Join(s.RigheAlbero, " ") != "decisioni_successive_da_rivedere.conferma_albero.documenti[0] decisioni_successive_da_rivedere.conferma_albero.allegati[0]" ||
		len(s.ChiaviLibere) != 0 {
		t.Errorf("righe dell'albero %v, chiavi libere %v", s.RigheAlbero, s.ChiaviLibere)
	}
}

// TestLeggiSezioniChiaviNonTradotte: una chiave nuova, a qualunque livello, va fra le non tradotte, una volta sola e
// con gli indici scritti «[]»; la lettura non si ferma.
func TestLeggiSezioniChiaviNonTradotte(t *testing.T) {
	s := sezioniACME(t, func(raw string) string {
		raw = strings.Replace(raw, "      atteso: figlio\n", "      atteso: figlio\n      colore: blu\n", 1)
		raw = strings.Replace(raw, "      atteso: fuori\n", "      atteso: fuori\n      colore: rosso\n", 1)
		raw = strings.Replace(raw, "0111\"\n    atteso:\n      base: 9123456\n", "0111\"\n    atteso:\n      base: 9123456\n      chiave_inventata: 1\n", 1)
		raw = strings.Replace(raw, "  conteggi:\n", "  formato: nuovo\n  conteggi:\n", 1)
		raw = strings.Replace(raw, "      nell_export_proposte:\n", "      nell_export_proposte:\n        colonna_nuova: x\n", 1)
		return raw + "sezione_nuova: []\n"
	})
	atteso := []string{"baseline_decisioni[].atteso.chiave_inventata",
		"decisioni_successive_da_rivedere.file_confermati[].nell_export_proposte.colonna_nuova",
		"scenario_inoltro.file[].colore", "scenario_inoltro.formato", "sezione_nuova"}
	if strings.Join(s.NonTradotte, "|") != strings.Join(atteso, "|") {
		t.Fatalf("non tradotte:\n%v\nattese:\n%v", s.NonTradotte, atteso)
	}
	if s.Scenario == nil || len(s.Scenario.File) != 3 {
		t.Error("la lettura non si ferma per una chiave non tradotta")
	}
	e := controlloAttesiInteri(true, "", &s)
	if c := e.controllo(); c.Stato != ControlloEseguito || c.Differenze != len(atteso) {
		t.Errorf("il controllo n.5 conta le chiavi non tradotte: %+v", c)
	}
}

func TestLeggiSezioniValoriNonLetti(t *testing.T) {
	s := sezioniACME(t, func(raw string) string {
		raw = strings.Replace(raw, "  prodotti_confermati_db: false\n", "  prodotti_confermati_db: \"no\"\n", 1)
		raw = strings.Replace(raw, "      quantita_richiesta: 5\n", "      quantita_richiesta: cinque\n", 1)
		raw = strings.Replace(raw, "allegato_id: 00000000-0000-4000-8000-000000000104", "allegato_id: non-un-uuid", 1)
		return raw
	})
	testo := strings.Join(s.Errori, "\n")
	for _, p := range []string{"scenario_inoltro.prodotti_confermati_db: serve un booleano",
		"scenario_inoltro.prodotti_attesi[0].quantita_richiesta: serve un intero", "reali_archivi[0].allegato_id: l'UUID non si legge"} {
		if !strings.Contains(testo, p) {
			t.Errorf("errore %q mancante in:\n%s", p, testo)
		}
	}
	if _, err := LeggiSezioni([]byte("- una\n- lista\n")); err == nil {
		t.Error("un documento senza una mappa in cima accettato")
	}
}

// TestChiaviDelleSezioniNeutre: le chiavi che il runner conosce sono snake_case, e le sezioni hanno i nomi neutri di
// M-21 (nessun nome di cliente: R47 a). I nomi veri delle sezioni non stanno nelle prove: qui si guarda solo la forma.
func TestChiaviDelleSezioniNeutre(t *testing.T) {
	snake := regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	for _, gruppo := range [][]string{chiaviVoce, chiaviScenario, chiaviBaseline, chiaviReali, chiaviDaRivedere, chiaviDecisione,
		invariantiBooleani, chiaviID, conteggiNoti, sezioniExport} {
		for _, k := range gruppo {
			if !snake.MatchString(k) {
				t.Errorf("chiave %q non snake_case", k)
			}
		}
	}
	for _, sez := range []string{confronto.SezioneScenario, confronto.SezioneBaseline, confronto.SezioneReali, confronto.SezioneDaRivedere} {
		if !snake.MatchString(sez) {
			t.Errorf("sezione %q", sez)
		}
	}
}

// fotoEIndice: la fotografia della scena e il suo indice, con le regole ACME compilate.
func fotoEIndice(t *testing.T) (scenaBanco, indiceFoto) {
	t.Helper()
	s := preparaBanco(t, mutaBanco{})
	return s, indicizza(fotoDellaScena(t, s))
}

func TestTraduzioneDelleSezioni(t *testing.T) {
	s, ix := fotoEIndice(t)
	f := fotoDellaScena(t, s)
	ins := insiemeACME(t)
	tr := traduci(sezioniACME(t, nil), f, ix, &ins)
	if len(tr.errori) != 0 || len(tr.nonRisolte) != 0 || len(tr.doppie) != 0 {
		t.Fatalf("errori %v, non risolte %v, doppie %v", tr.errori, tr.nonRisolte, tr.doppie)
	}
	scen, dec := tr.atteso(threadScenario), tr.atteso(threadDecisioni)
	if scen == nil || dec == nil || len(scen.File) != 4 || len(dec.File) != 2 {
		t.Fatalf("atteso per thread: %+v %+v", scen, dec)
	}
	per := map[string]confronto.FileAtteso{}
	for _, a := range append(append([]confronto.FileAtteso(nil), scen.File...), dec.File...) {
		per[a.Sezione+"/"+a.AllegatoID.String()] = a
	}
	r := per[confronto.SezioneScenario+"/"+allRadice.String()]
	if r.Atteso != confronto.AttesoRadice || r.StatoAtteso != confronto.StatoAttesoCandidatoScenario || r.TargetBase != "9123456" || r.Autorita != "scenario" {
		t.Errorf("radice dello scenario: %+v", r)
	}
	if fi := per[confronto.SezioneScenario+"/"+allFiglio.String()]; fi.Atteso != confronto.AttesoFiglio || strings.Join(fi.Radici, ",") != "9123456" {
		t.Errorf("figlio: %+v", fi)
	}
	if fu := per[confronto.SezioneScenario+"/"+allFuori.String()]; fu.StatoAtteso != confronto.StatoAttesoFuoriScenario || fu.TargetBase != "" {
		t.Errorf("fuori: %+v", fu)
	}
	b := per[confronto.SezioneBaseline+"/"+allBaseline.String()]
	if b.TargetBase != "9123456" || b.BaseLetta != "9123456" || b.Decisione == nil || b.Decisione.Codice != "9123456A" ||
		b.Decisione.ComponenteProposta == nil || *b.Decisione.ComponenteProposta != compBaseline || b.Decisione.Documento == nil || b.Invarianti["componente_id_proposta_vuoto_preservato"] {
		t.Errorf("baseline: %+v", b)
	}
	if re := per[confronto.SezioneReali+"/"+allArchivio.String()]; re.Riservato == "" {
		t.Errorf("i file reali restano riservati finché l'esito con il target non è scritto (R21 c A): %+v", re)
	}
	if c5 := per[confronto.SezioneDaRivedere+"/"+allDaRivedere.String()]; c5.Decisione == nil || c5.Invarianti["in_baseline"] {
		t.Errorf("C5: %+v", c5)
	}
	if len(scen.Prodotti) != 1 || scen.Prodotti[0].QuantitaDaCella == nil || !*scen.Prodotti[0].QuantitaDaCella || *scen.Prodotti[0].Quantita != 5 {
		t.Errorf("prodotti attesi: %+v", scen.Prodotti)
	}
}

// TestTraduzioneErrori (D8, R-51, la nota sulle radici vuote): una voce che si contraddice è un errore di traduzione
// (uscita 1), e non arriva a confronto.
func TestTraduzioneErrori(t *testing.T) {
	s, ix := fotoEIndice(t)
	f := fotoDellaScena(t, s)
	ins := insiemeACME(t)
	casi := map[string]struct {
		muta  func(string) string
		parte string
	}{
		"radice senza target": {func(r string) string {
			return strings.Replace(r, "      atteso_target_base: 9123456\n      autorita: scenario\n      scritture_consentite: false\n",
				"      autorita: scenario\n      scritture_consentite: false\n", 1)
		}, "atteso radice senza atteso_target_base"},
		"figlio senza radici": {func(r string) string {
			return strings.Replace(r, "      radici: [9123456]\n", "", 1)
		}, "atteso figlio senza radici"},
		"figlio con le radici vuote": {func(r string) string {
			return strings.Replace(r, "      radici: [9123456]\n", "      radici: []\n", 1)
		}, "atteso figlio senza radici"},
		"radice vuota": {func(r string) string {
			return strings.Replace(r, "      radici: [9123456]\n", "      radici: [9123456, \"\"]\n", 1)
		}, "radici[1] vuota"},
		"atteso fuori elenco": {func(r string) string {
			return strings.Replace(r, "      atteso: figlio\n", "      atteso: sopra\n", 1)
		}, "atteso \"sopra\" fuori elenco"},
		"autorità fuori elenco": {func(r string) string {
			return strings.Replace(r, "      atteso_target_base: 9123457\n      radici: [9123456]\n      autorita: scenario\n",
				"      atteso_target_base: 9123457\n      radici: [9123456]\n      autorita: ignota\n", 1)
		}, "autorita \"ignota\" fuori elenco"},
		"fonte della quantità non tradotta": {func(r string) string {
			return strings.Replace(r, "      fonte_quantita: cella\n", "      fonte_quantita: riga\n", 1)
		}, "fonte_quantita \"riga\" non tradotta"},
		"stato atteso fuori elenco": {func(r string) string {
			return strings.Replace(r, "  - id: reale-acme-01\n", "  - id: reale-acme-01\n    stato_atteso: provvisorio\n", 1)
		}, "stato_atteso \"provvisorio\" fuori elenco"},
	}
	for nome, c := range casi {
		t.Run(nome, func(t *testing.T) {
			sez := sezioniACME(t, c.muta)
			tr := traduci(sez, f, ix, &ins)
			if !strings.Contains(strings.Join(tr.errori, "\n"), c.parte) {
				t.Fatalf("errori %v, atteso %q", tr.errori, c.parte)
			}
			e := controlloTraduzione(&sez, tr).controllo()
			if e.Stato != ControlloEseguito || e.Differenze == 0 {
				t.Errorf("il controllo della traduzione è una differenza: %+v", e)
			}
		})
	}
}

// manifestDellaScena: il manifest letto, per le prove che chiamano le funzioni del runner da sole.
func manifestDellaScena(t *testing.T, s scenaBanco) dataset.Manifest {
	t.Helper()
	raw, err := os.ReadFile(s.manifest)
	if err != nil {
		t.Fatal(err)
	}
	m, err := dataset.Leggi(raw, s.dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
