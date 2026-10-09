package bancoa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"promatec/cockpit/internal/platform/dataset"
	"promatec/cockpit/internal/platform/jsoncanonico"
	"promatec/cockpit/internal/platform/migrazioni"
)

// L1 — il banco dopo le risposte dell'utente del 07/10 (domande-a1c.md; revisione d'impatto, R114, R116, R117):
//   - R116 B, precisata: ogni controllo e ogni voce del gate del rapporto v3 ha una classe e un esito tri-stato; la
//     tabella delle classi è completa e segue la revisione d'impatto (R116 §3), con le decisioni dell'utente dell'08/10
//     (D-R116 I, O, F; D-R115 A: B6c); la chiusura distingue la parte obbligatoria del runner dall'incompletezza del
//     rapporto; le uscite non cambiano (il 3 non diventa 0); il JSON del rapporto di A1a è identico, byte per byte;
//   - R117 b, ratificata e ampliata: le sentinelle sono una riga delegata ad A1c-L4D-01, non NON ESEGUITA, e l'uscita
//     non dipende da lei;
//   - R114, precisata: nella corsa sugli export la misura dichiara il denominatore e gli esclusi, invece di dare zero.
//
// I clienti di questi test sono inventati: vedi scena_banco_test.go.

// esitoAtteso: l'esito di R44 dai soli stati e differenze dei controlli, senza le classi: una differenza dà «con
// differenze», un controllo non eseguito «non eseguito», altrimenti conforme. Un controllo delegato non è non eseguito.
func esitoAtteso(cc []Controllo) Esito {
	nonEseguiti := false
	for _, c := range cc {
		if c.Differenze > 0 {
			return EsitoConDifferenze
		}
		if c.Stato == ControlloNonEseguito {
			nonEseguiti = true
		}
	}
	if nonEseguiti {
		return EsitoNonEseguito
	}
	return EsitoConforme
}

// nellaTabella: la voce sta nella tabella delle classi (non è la classe per difetto di una voce sconosciuta).
func nellaTabella(sede, nome string) bool {
	for _, v := range tabellaClassi {
		if v.Sede == sede && v.Nome == nome {
			return true
		}
	}
	return false
}

// TestRispostaR116LaTabellaDelleClassi: ogni riga ha una classe valida, la motivazione e la fonte; la prova c'è solo, e
// sempre, per gli obbligatori esterni; nessuna voce due volte. La classificazione è quella della revisione d'impatto
// (R116 §3), con le decisioni dell'utente dell'08/10 (B6c): D-R116 I, O, F e D-R115 A, ognuna con la sua fonte; nessuna
// nota «classe da decidere» resta. Le sigle sono quelle di domande-a1c.md (R-132 della revisione delle risposte):
// nessuna D1, D2, D3 nei testi della tabella.
func TestRispostaR116LaTabellaDelleClassi(t *testing.T) {
	sedi := map[string]ClasseControllo{SedeControllo: "", SedeGate: "", SedeRapporto: ClasseInformativo, SedeFuori: ClasseFuoriPerimetro}
	sigleVecchie := regexp.MustCompile(`\bD[1-3]\b`)
	visti := map[[2]string]bool{}
	for _, v := range tabellaClassi {
		if m := sigleVecchie.FindString(v.Motivazione + " " + v.Fonte + " " + v.Prova + " " + v.Nota); m != "" {
			t.Errorf("%v: la sigla %s non è quella di domande-a1c.md", [2]string{v.Sede, v.Nome}, m)
		}
		k := [2]string{v.Sede, v.Nome}
		if visti[k] {
			t.Errorf("%v due volte nella tabella", k)
		}
		visti[k] = true
		switch v.Classe {
		case ClasseObbligatorio, ClasseObbligatorioEsterno, ClasseInformativo, ClasseFuoriPerimetro:
		default:
			t.Errorf("%v: classe %q", k, v.Classe)
		}
		if v.Motivazione == "" || v.Fonte == "" {
			t.Errorf("%v: senza motivazione o senza fonte", k)
		}
		if (v.Classe == ClasseObbligatorioEsterno) != (v.Prova != "") {
			t.Errorf("%v: la prova che lo chiude c'è solo, e sempre, per un obbligatorio esterno (%q, %q)", k, v.Classe, v.Prova)
		}
		attesa, ok := sedi[v.Sede]
		if !ok || (attesa != "" && v.Classe != attesa) {
			t.Errorf("%v: sede %q con la classe %q", k, v.Sede, v.Classe)
		}
		if v.Nota != "" || strings.Contains(v.Fonte, "aperta") {
			t.Errorf("%v: dopo le decisioni dell'08/10 nessuna classe resta da decidere: nota %q, fonte %q", k, v.Nota, v.Fonte)
		}
	}
	for k, classe := range map[[2]string]ClasseControllo{
		{SedeControllo, ControlloSentinelle}:             ClasseObbligatorioEsterno,
		{SedeControllo, ControlloClientiExport}:          ClasseFuoriPerimetro,
		{SedeControllo, ControlloScenario}:               ClasseObbligatorio,
		{SedeControllo, ControlloInvariantiC5}:           ClasseObbligatorio,
		{SedeControllo, ControlloLetture}:                ClasseObbligatorio,
		{SedeControllo, "casi_contratto"}:                ClasseObbligatorio,
		{SedeGate, VoceGateFalseAssociazioni}:            ClasseObbligatorio,
		{SedeGate, VoceGateConteggiScenario}:             ClasseObbligatorio,
		{SedeGate, VoceGateDecisioniPreservate}:          ClasseObbligatorio,
		{SedeGate, VoceGateRiservati}:                    ClasseObbligatorio,
		{SedeGate, VoceGateZeroScritture}:                ClasseObbligatorioEsterno,
		{SedeGate, VoceGateMotoreSenzaLLM}:               ClasseObbligatorioEsterno,
		{SedeGate, VoceGateAltriProfili}:                 ClasseObbligatorioEsterno,
		{SedeRapporto, SezioneInformativaProdotti}:       ClasseInformativo,
		{SedeRapporto, SezioneInformativaCorrezioni}:     ClasseInformativo,
		{SedeRapporto, SezioneInformativaProfilo}:        ClasseInformativo,
		{SedeRapporto, SezioneInformativaCensimento}:     ClasseInformativo,
		{SedeRapporto, SezioneInformativaNonApplicabili}: ClasseInformativo,
		{SedeRapporto, SezioneInformativaChiaviLibere}:   ClasseInformativo,
		{SedeRapporto, SezioneInformativaConfermaAlbero}: ClasseInformativo,
		{SedeFuori, "PO-30"}:                             ClasseFuoriPerimetro,
		{SedeFuori, ParteFinitiConGliExport}:             ClasseFuoriPerimetro,
	} {
		if !nellaTabella(k[0], k[1]) || classeDi(k[0], k[1]).Classe != classe {
			t.Errorf("%v: classe %q, attesa %q (R116 §3, le decisioni dell'08/10)", k, classeDi(k[0], k[1]).Classe, classe)
		}
	}
	if s := classeDi(SedeControllo, ControlloSentinelle); s.Prova != "A1c-L4D-01" || !strings.Contains(s.Fonte, "R117 b") {
		t.Errorf("sentinelle: %+v (R117 b: la prova è A1c-L4D-01)", s)
	}
	// Le decisioni dell'08/10, ognuna nella fonte della sua riga.
	for k, fonte := range map[[2]string]string{
		{SedeControllo, ControlloClientiExport}:          "D-R115 A, decisa dall'utente l'08/10",
		{SedeControllo, ControlloScenario}:               "D-R116 F, decisa dall'utente l'08/10",
		{SedeControllo, ControlloInvariantiC5}:           "D-R116 I, decisa dall'utente l'08/10",
		{SedeControllo, ControlloLetture}:                "D-R116 O, decisa dall'utente l'08/10",
		{SedeRapporto, SezioneInformativaConfermaAlbero}: "D-R116 I, decisa dall'utente l'08/10",
		{SedeRapporto, SezioneInformativaCorrezioni}:     "D-R114 B, decisa dall'utente l'08/10",
		{SedeFuori, ParteFinitiConGliExport}:             "D-R116 F, decisa dall'utente l'08/10, coerente con D-R115 A",
	} {
		if c := classeDi(k[0], k[1]); !strings.Contains(c.Fonte, fonte) {
			t.Errorf("%v: fonte %q, attesa «%s»", k, c.Fonte, fonte)
		}
	}
	if c := classeDi(SedeControllo, ControlloClientiExport); !strings.Contains(c.Motivazione, "non sono coerenti") || !strings.Contains(c.Motivazione, "uscita 3") {
		t.Errorf("clienti_degli_export: la motivazione dice la non coerenza e l'uscita che resta 3: %+v", c)
	}
	if partiDelControllo[ParteFinitiConGliExport] != ControlloScenario {
		t.Errorf("la parte dei finiti è del n.1: %v", partiDelControllo)
	}
}

// TestRispostaR116TabellaCompletaSulSorgente: ogni nome di controllo e di voce del gate che il banco scrive sta nella
// tabella, così nessuna voce prende la classe per difetto. I nomi si leggono dal sorgente dei file di A1c del pacchetto
// (le chiamate b.controllo, i letterali esitoControllo e VoceGate); le tre voci del manifest le aggiunge leggiDataset
// dalla lettura di A1a. Una costante che la prova non conosce la fa cadere: va aggiunta qui e nella tabella.
func TestRispostaR116TabellaCompletaSulSorgente(t *testing.T) {
	costanti := map[string]string{
		"VoceAttesi": VoceAttesi, "VoceIndice": VoceIndice, "VoceCasi": VoceCasi,
		"ControlloScenario": ControlloScenario, "ControlloRisoluzione": ControlloRisoluzione, "ControlloFonti": ControlloFonti,
		"ControlloCasiIndice": ControlloCasiIndice, "ControlloAttesiInteri": ControlloAttesiInteri, "ControlloTraduzione": ControlloTraduzione,
		"ControlloInvariantiC5": ControlloInvariantiC5, "ControlloBaselineRighe": ControlloBaselineRighe, "ControlloLetture": ControlloLetture,
		"ControlloCasiFoto": ControlloCasiFoto, "ControlloClientiExport": ControlloClientiExport, "ControlloCopia": ControlloCopia,
		"ControlloSentinelle":       ControlloSentinelle,
		"VoceGateFalseAssociazioni": VoceGateFalseAssociazioni, "VoceGateConteggiScenario": VoceGateConteggiScenario,
		"VoceGateDecisioniPreservate": VoceGateDecisioniPreservate, "VoceGateZeroScritture": VoceGateZeroScritture,
		"VoceGateRiservati": VoceGateRiservati, "VoceGateMotoreSenzaLLM": VoceGateMotoreSenzaLLM, "VoceGateAltriProfili": VoceGateAltriProfili,
	}
	controlli := regexp.MustCompile(`(?:b\.controllo\(|esitoControllo\{nome:\s*)(?:"([^"]+)"|([A-Za-z]\w*))`)
	voci := regexp.MustCompile(`VoceGate\{Nome:\s*(?:"([^"]+)"|([A-Za-z]\w*))`)
	nomi := map[[2]string]bool{{SedeControllo, "manifest"}: true, {SedeControllo, VoceIndice}: true, {SedeControllo, VoceAttesi}: true}
	for _, f := range []string{"banco.go", "controlli.go", "export.go", "gate.go", "prodotti.go", "sezioni.go", "passaggio.go", "rapporto_banco.go", "classi.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for sede, re := range map[string]*regexp.Regexp{SedeControllo: controlli, SedeGate: voci} {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				nome := m[1]
				if nome == "" {
					v, ok := costanti[m[2]]
					if !ok {
						t.Errorf("%s: la costante %s non è nella prova: aggiungila, e la sua voce alla tabella", f, m[2])
						continue
					}
					nome = v
				}
				nomi[[2]string{sede, nome}] = true
			}
		}
	}
	if len(nomi) < 30 {
		t.Fatalf("dal sorgente si leggono solo %d nomi: la lettura non funziona", len(nomi))
	}
	for k := range nomi {
		if !nellaTabella(k[0], k[1]) {
			t.Errorf("%v: il banco lo scrive, ma la tabella delle classi non lo conosce", k)
		}
	}
}

// TestRispostaR116OgniVoceHaClasseEdEsito (R116 §4, 1): in ogni rapporto v3 ogni controllo e ogni voce del gate ha la
// classe della tabella e l'esito derivato dallo stato, anche nel JSON; il rapporto ha la chiusura; l'esito della corsa
// è quello dei soli stati (le classi non lo cambiano).
func TestRispostaR116OgniVoceHaClasseEdEsito(t *testing.T) {
	rapporti := map[string]RapportoBanco{
		"export con -attesi e -gate": corsaBanco(t, mutaBanco{}, true, true),
		"export con -attesi":         corsaBanco(t, mutaBanco{}, true, false),
		"export senza -attesi":       corsaBanco(t, mutaBanco{}, false, false),
		"un thread non esportato":    corsaBanco(t, mutaBanco{}, true, false, uidACME(0x79)),
	}
	interrotto, ferma := context.WithCancel(context.Background())
	ferma()
	r, err := EseguiBanco(interrotto, preparaBanco(t, mutaBanco{}).opzioniExport(true), nil)
	if err != nil {
		t.Fatal(err)
	}
	rapporti["corsa interrotta"] = r
	if v, ok := os.LookupEnv("PGPASSWORD"); ok {
		os.Unsetenv("PGPASSWORD")
		t.Cleanup(func() { os.Setenv("PGPASSWORD", v) })
	}
	s := preparaBanco(t, mutaBanco{})
	r, err = EseguiBanco(context.Background(), Opzioni{Modalita: ModalitaDSN, Dataset: s.manifest, Uscita: s.uscita, Tutti: true,
		Migrazioni: fstest.MapFS{"migrations/0001_acme.sql": &fstest.MapFile{Data: []byte("select 1;")}},
		DSN:        "postgres://acme_banco@127.0.0.1:1/acme_prova?connect_timeout=2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rapporti["copia non raggiungibile"] = r

	nomi := map[string]bool{}
	for nome, r := range rapporti {
		if len(r.Controlli) == 0 || r.Chiusura == nil {
			t.Fatalf("%s: controlli %d, chiusura %+v", nome, len(r.Controlli), r.Chiusura)
		}
		for _, c := range r.Controlli {
			nomi[c.Nome] = true
			passato := c.Stato == ControlloEseguito && c.Differenze == 0
			switch {
			case !nellaTabella(SedeControllo, c.Nome):
				t.Errorf("%s: il controllo %s non è nella tabella delle classi", nome, c.Nome)
			case c.Classe != classeDi(SedeControllo, c.Nome).Classe || c.Esito == "":
				t.Errorf("%s: %s senza la classe della tabella o senza esito: %+v", nome, c.Nome, c)
			case (c.Esito == TriStatoPassato) != passato || (c.Esito == TriStatoFallito) != (c.Differenze > 0):
				t.Errorf("%s: %s, l'esito non è quello dello stato: %+v", nome, c.Nome, c)
			}
		}
		if g := r.Gate; g != nil {
			for _, v := range g.Voci {
				if !nellaTabella(SedeGate, v.Nome) || v.Classe != classeDi(SedeGate, v.Nome).Classe || v.Esito != esitoDellaVoce(v) || v.Esito == "" {
					t.Errorf("%s: voce del gate %+v", nome, v)
				}
			}
		}
		if r.Esito != esitoAtteso(r.Controlli) {
			t.Errorf("%s: esito %s, dai soli stati %s", nome, r.Esito, esitoAtteso(r.Controlli))
		}
		js, err := jsoncanonico.Codifica(r)
		if err != nil {
			t.Fatal(err)
		}
		var forma struct {
			Controlli []map[string]any `json:"controlli"`
			Gate      *struct {
				Voci []map[string]any `json:"voci"`
			} `json:"gate"`
			Chiusura map[string]any `json:"chiusura"`
		}
		if err := json.Unmarshal(js, &forma); err != nil {
			t.Fatal(err)
		}
		voci := forma.Controlli
		if forma.Gate != nil {
			voci = append(voci, forma.Gate.Voci...)
		}
		for _, v := range voci {
			if v["classe"] == nil || v["esito"] == nil {
				t.Errorf("%s: nel JSON una voce senza classe o esito: %v", nome, v)
			}
		}
		if forma.Chiusura["conclusione"] == nil {
			t.Errorf("%s: nel JSON manca la chiusura", nome)
		}
	}
	for _, n := range []string{"interruzione", "thread", ControlloCopia, nomeControlloGate, ControlloScenario} {
		if !nomi[n] {
			t.Errorf("le corse non hanno prodotto il controllo %s: la prova non lo guarda", n)
		}
	}
}

// controlliPassatiACME: i controlli obbligatori del runner di una corsa sugli export con -attesi, tutti passati.
func controlliPassatiACME() []Controllo {
	var out []Controllo
	for _, n := range []string{"manifest", VoceIndice, VoceAttesi, VoceCasi, "grammatiche", "export", "valutazione", ControlloCasiFoto,
		ControlloClientiExport, ControlloScenario, ControlloRisoluzione, ControlloFonti, ControlloCasiIndice, ControlloAttesiInteri,
		ControlloTraduzione, "confronto", ControlloBaselineRighe, ControlloInvariantiC5, ControlloLetture, "casi_contratto"} {
		out = append(out, Controllo{Nome: n, Stato: ControlloEseguito})
	}
	return out
}

// TestRispostaR116ChiusuraConISoliEsterni (R116 §4, 2): gli obbligatori del runner tutti passati e le sole voci esterne
// del gate non eseguite: la chiusura dice «parte obbligatoria del runner conclusa; esterni da chiudere nel registro»,
// con la prova di ognuno; il gate resta incompleto e, con -gate, l'uscita resta 3. Poi le altre tre conclusioni: un
// obbligatorio non passato, un esterno fallito, tutto passato (e il rapporto completo solo con il gate).
func TestRispostaR116ChiusuraConISoliEsterni(t *testing.T) {
	g := calcolaGate(ingressoGateACME())
	passati := controlliPassatiACME()
	b := &banco{o: Opzioni{Gate: true}, r: RapportoBanco{Controlli: append(append([]Controllo(nil), passati...), esitoUscitaGate(g).controllo()), Gate: &g}}
	b.chiudi()
	ch := b.r.Chiusura
	// clienti_degli_export è fuori perimetro (D-R115 A): non conta fra gli obbligatori del runner.
	if ch == nil || ch.Conclusione != ConclusioneConEsterni || !ch.Obbligatori.Conclusi || ch.Obbligatori.Totale != len(passati)-1+4 ||
		ch.Obbligatori.Passati != ch.Obbligatori.Totale || len(ch.Obbligatori.NonPassati) != 0 || ch.RapportoCompleto {
		t.Fatalf("chiusura: %+v", ch)
	}
	if len(ch.Esterni) != 3 {
		t.Fatalf("gli obbligatori esterni: %+v", ch.Esterni)
	}
	for _, e := range ch.Esterni {
		if e.Sede != SedeGate || e.Prova == "" || e.Esito != TriStatoNonEseguito {
			t.Errorf("esterno senza prova o con un esito sbagliato: %+v", e)
		}
	}
	// Fuori perimetro: clienti_degli_export, dai controlli, e PO-30; la parte dei finiti del n.1 no, perché la corsa non ne
	// ha (partiDelControllo).
	if len(ch.FuoriPerimetro) != 2 || ch.FuoriPerimetro[0].Nome != ControlloClientiExport || ch.FuoriPerimetro[1].Nome != "PO-30" ||
		ch.FuoriPerimetro[1].Motivazione == "" {
		t.Errorf("fuori perimetro: %+v", ch.FuoriPerimetro)
	}
	if b.r.Gate.Esito != GateIncompleto || b.r.Esito.CodiceUscita() != UscitaNonEseguito {
		t.Fatalf("il gate incompleto e l'uscita 3 con -gate: %s, %s", b.r.Gate.Esito, b.r.PrimaRiga())
	}
	if txt := b.r.Testo(); !strings.Contains(txt, "\nchiusura: "+ConclusioneConEsterni+"; rapporto incompleto; gate: incompleto\n") ||
		!strings.Contains(txt, "obbligatorio esterno: motore_senza_llm (non_eseguito), prova: A1c-L4S-08") {
		t.Errorf("il riepilogo della chiusura:\n%s", txt)
	}

	chiudiCon := func(cc []Controllo, g *Gate) *Chiusura {
		b := &banco{r: RapportoBanco{Controlli: cc, Gate: g}}
		b.chiudi()
		return b.r.Chiusura
	}
	// Un obbligatorio del runner non eseguito: non conclusa, e il controllo è fra i non passati, con la fonte della sua
	// classe (D-R116 O per le letture parziali, che restano obbligatorie).
	cc := append([]Controllo(nil), passati...)
	for i := range cc {
		if cc[i].Nome == ControlloLetture {
			cc[i].Stato, cc[i].Motivo = ControlloNonEseguito, "2 parti non verificabili"
		}
	}
	if ch := chiudiCon(cc, &g); ch.Conclusione != ConclusioneNonConclusa || len(ch.Obbligatori.NonPassati) != 1 ||
		!strings.Contains(ch.Obbligatori.NonPassati[0].Fonte, "D-R116 O") {
		t.Errorf("un obbligatorio non eseguito: %+v", ch)
	}
	// Una voce obbligatoria del gate non superata: non conclusa.
	in := ingressoGateACME()
	in.CasiRiservatiNonRiservati = 1
	if gr := calcolaGate(in); chiudiCon(passati, &gr).Conclusione != ConclusioneNonConclusa {
		t.Errorf("una voce obbligatoria non superata: %+v", chiudiCon(passati, &gr))
	}
	// La parte del runner di un esterno fallita (il collegamento può scrivere): conclusa, con l'esterno fallito.
	in = ingressoGateACME()
	in.SolaLetturaOk, in.SolaLetturaMotivo = false, "il collegamento può scrivere"
	if gs := calcolaGate(in); chiudiCon(passati, &gs).Conclusione != ConclusioneEsternoFallito {
		t.Errorf("un esterno fallito: %+v", chiudiCon(passati, &gs))
	}
	// Tutto passato (anche gli esterni, che oggi il runner non supera mai): conclusa e, senza informativi incompleti,
	// rapporto completo; senza il gate il rapporto non è mai completo.
	tutte := Gate{Esito: GateSuperato}
	for _, v := range g.Voci {
		v.Stato = VoceSuperata
		tutte.Voci = append(tutte.Voci, v)
	}
	classificaVoci(tutte.Voci)
	if ch := chiudiCon(passati, &tutte); ch.Conclusione != ConclusioneConclusa || !ch.RapportoCompleto || len(ch.Esterni) != 3 {
		t.Errorf("tutto passato: %+v", ch)
	}
	if ch := chiudiCon(passati, nil); ch.Conclusione != ConclusioneConclusa || ch.RapportoCompleto || !strings.HasPrefix(ch.Gate, "assente") {
		t.Errorf("senza il gate: %+v", ch)
	}
}

// TestRispostaR116LeClassiNonCambianoLUscita: le classi e la chiusura non cambiano l'uscita (R116 B: «Non convertire
// genericamente il codice 3 in 0»): un controllo che la tabella non conosce è obbligatorio per difetto, e non eseguito
// dà 3; una differenza prevale ancora; con i soli esterni non eseguiti e -gate l'uscita resta 3 (sopra), e su ogni
// corsa della scena l'esito è quello dei soli stati (TestRispostaR116OgniVoceHaClasseEdEsito).
func TestRispostaR116LeClassiNonCambianoLUscita(t *testing.T) {
	ignota := classeDi(SedeControllo, "controllo_inventato_acme")
	if ignota.Classe != ClasseObbligatorio || ignota.Motivazione == "" {
		t.Fatalf("una voce sconosciuta: %+v (obbligatoria per difetto)", ignota)
	}
	b := &banco{r: RapportoBanco{Controlli: append(controlliPassatiACME(), Controllo{Nome: "controllo_inventato_acme", Stato: ControlloNonEseguito, Motivo: "manca"})}}
	b.chiudi()
	if b.r.Esito.CodiceUscita() != UscitaNonEseguito || b.r.Chiusura.Conclusione != ConclusioneNonConclusa {
		t.Errorf("un obbligatorio sconosciuto non eseguito: %s, %+v", b.r.PrimaRiga(), b.r.Chiusura)
	}
	b.r.Controlli = append(b.r.Controlli, Controllo{Nome: "casi_contratto", Stato: ControlloEseguito, Differenze: 1})
	b.chiudi()
	if b.r.Esito.CodiceUscita() != UscitaConDifferenze {
		t.Errorf("1 prevale su 3: %s", b.r.PrimaRiga())
	}
}

// TestRispostaR116GliInformativiIncompleti: le sezioni informative che la corsa non ha completato stanno nella chiusura,
// ognuna con il motivo, e non toccano la conclusione della parte obbligatoria: la fonte contro l'atteso dei prodotti
// attesi dello scenario con parti non verificate, o che la corsa non verifica (PO-29; R109, precisata dall'utente il
// 07/10); le parti non applicabili; le chiavi libere non verificate; il profilo dei limiti assente dopo una fotografia
// letta. Una sezione prodotti calcolata, con la fonte contro l'atteso verificata per intero, non è incompleta.
func TestRispostaR116GliInformativiIncompleti(t *testing.T) {
	prodotti := func(fonte *ConfrontoFonte) *SezioneProdotti {
		s := &SezioneProdotti{Calcolata: true, Thread: []ProdottiThread{{Prodotti: []ProdottoInformativo{{Rif: "identificativo:9123456"}}}}}
		if fonte != nil {
			s.FontiScenario = &FontiDelloScenario{Prodotti: []ConfrontoFonte{*fonte}}
		}
		return s
	}
	g := Gate{Esito: GateSuperato}
	nonVerificata := &ConfrontoFonte{Base: "9123456", NonVerificate: []string{"motivo: non fissato dagli attesi"}}
	r := RapportoBanco{Controlli: controlliPassatiACME(), Fotografia: &SintesiFotografia{}, Prodotti: prodotti(nonVerificata), Gate: &g,
		Dettagli: []DettaglioControllo{{Nome: ControlloInvariantiC5, NonApplicabili: []string{"a", "b"}}},
		Attesi:   &SintesiAttesi{ChiaviLibere: []string{"x"}}}
	ch := chiusuraDi(r)
	motivi := map[string]string{}
	for _, v := range ch.Informativi {
		motivi[v.Nome] = v.Motivo
	}
	for nome, parte := range map[string]string{
		SezioneInformativaProdotti:       "1 prodotti attesi dello scenario su 1 con parti della fonte contro l'atteso non verificate (PO-29, R109)",
		SezioneInformativaNonApplicabili: "2 parti non applicabili",
		SezioneInformativaChiaviLibere:   "1 chiavi libere non verificate",
		SezioneInformativaProfilo:        "non calcolato",
	} {
		if !strings.Contains(motivi[nome], parte) {
			t.Errorf("%s: %q, atteso «%s»", nome, motivi[nome], parte)
		}
	}
	if len(ch.Informativi) != 4 || ch.Conclusione != ConclusioneConclusa || ch.RapportoCompleto {
		t.Errorf("gli informativi incompleti non toccano la conclusione, ma il rapporto non è completo: %+v", ch)
	}
	// La fonte di uno scenario che la corsa non verifica è incompleta anche senza prodotti.
	r.Prodotti = prodotti(nil)
	r.Prodotti.FontiScenario = &FontiDelloScenario{NonVerificabile: "il thread dello scenario non è fra quelli scelti"}
	if ch := chiusuraDi(r); !strings.Contains(fmt.Sprint(ch.Informativi), "la fonte contro l'atteso dei prodotti dello scenario non si verifica in questa corsa: il thread dello scenario non è fra quelli scelti") {
		t.Errorf("lo scenario non verificabile: %+v", ch.Informativi)
	}
	r.Prodotti, r.Dettagli, r.Attesi, r.ProfiloLimiti = prodotti(&ConfrontoFonte{Base: "9123456", Differenze: []string{"stato"}}), nil, nil, &ProfiloLimiti{}
	if ch := chiusuraDi(r); len(ch.Informativi) != 0 || !ch.RapportoCompleto {
		t.Errorf("senza informativi incompleti (una differenza della fonte è informazione, non una parte incompleta): %+v", ch)
	}
}

// controlloA1a: la forma del Controllo di A1a, prima della classe e dell'esito.
type controlloA1a struct {
	Nome       string `json:"nome"`
	Stato      string `json:"stato"`
	Differenze int    `json:"differenze"`
	Motivo     string `json:"motivo,omitempty"`
}

// TestRispostaR116IlJSONDiA1aNonCambia (R116 §4, 3): il rapporto di A1a non porta né la classe né l'esito dei
// controlli, e i suoi byte non cambiano: i controlli scritti da Esegui, riletti con la forma di A1a (che rifiuta un
// campo in più) e riscritti in forma canonica, sono gli stessi byte; un Controllo senza classe si scrive come prima.
func TestRispostaR116IlJSONDiA1aNonCambia(t *testing.T) {
	d := preparaDataset(t, nil)
	for _, modalita := range []string{ModalitaRegole, ModalitaCasi} {
		if _, err := Esegui(context.Background(), Opzioni{Modalita: modalita, Dataset: d.manifest, Uscita: filepath.Join(d.dir, "banco")}, nil); err != nil {
			t.Fatal(err)
		}
		js, err := os.ReadFile(filepath.Join(d.dir, "banco", modalita+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var parti map[string]json.RawMessage
		if err := json.Unmarshal(js, &parti); err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(parti["controlli"]))
		dec.DisallowUnknownFields()
		var cc []controlloA1a
		if err := dec.Decode(&cc); err != nil {
			t.Fatalf("%s: i controlli di A1a hanno un campo che A1a non conosce: %v", modalita, err)
		}
		vecchi, err := jsoncanonico.Codifica(cc)
		if err != nil || len(cc) == 0 || !bytes.Equal(vecchi, parti["controlli"]) {
			t.Errorf("%s: i byte dei controlli non sono quelli di A1a:\n%s\n%s", modalita, parti["controlli"], vecchi)
		}
		if bytes.Contains(js, []byte(`"classe"`)) {
			t.Errorf("%s: la classe nel rapporto di A1a", modalita)
		}
	}
	js, err := jsoncanonico.Codifica(Controllo{Nome: "manifest", Stato: ControlloEseguito, Motivo: "letto"})
	if err != nil || string(js) != `{"differenze":0,"motivo":"letto","nome":"manifest","stato":"eseguito"}` {
		t.Errorf("un Controllo senza classe: %s (%v)", js, err)
	}
}

// TestRispostaR117SentinelleDelegate (R117 §4 (a)): con le sentinelle dichiarate e il database del manifest, la riga è
// delegata ad A1c-L4D-01, non NON ESEGUITA: obbligatorio esterno, esito non eseguito dal runner, la prova e l'ambiente
// nel motivo, fra gli esterni della chiusura; l'uscita non dipende da lei (tutto il resto passato: conforme). La stessa
// corsa con la riga non eseguita, come prima di R117, esce con 3.
func TestRispostaR117SentinelleDelegate(t *testing.T) {
	col := migrazioni.Collegamento{Utente: "ruolo_acme_lettura", Database: "copia_acme"}
	piena := dataset.CopiaAttesa{Ruolo: "ruolo_acme_lettura", Database: "copia_acme", Schema: 21, Escluse: []string{"tabella_esclusa_acme"},
		Sentinelle: map[string]int64{"thread": 2}}
	corsa := func(sentinelle esitoControllo) *banco {
		copia, fermo, _ := controllaCopia(piena, col, 21)
		if fermo {
			t.Fatal("la copia del manifest non ferma la corsa")
		}
		b := &banco{}
		b.controllo("manifest", ControlloEseguito, 0, "")
		b.controllo("sola_lettura", ControlloEseguito, 0, "")
		b.esito(copia)
		b.esito(sentinelle)
		for _, n := range []string{"fotografia", "valutazione", ControlloCasiFoto, "confronto"} {
			b.controllo(n, ControlloEseguito, 0, "")
		}
		b.chiudi()
		return b
	}
	_, _, sent := controllaCopia(piena, col, 21)
	if sent == nil {
		t.Fatal("sentinelle dichiarate: nessuna riga")
	}
	b := corsa(*sent)
	c, _ := controlloBanco(b.r, ControlloSentinelle)
	if c.Stato != ControlloDelegato || c.Classe != ClasseObbligatorioEsterno || c.Esito != TriStatoNonEseguito || c.Differenze != 0 ||
		!strings.Contains(c.Motivo, "A1c-L4D-01") || !strings.Contains(c.Motivo, `"copia_acme"`) {
		t.Fatalf("la riga delle sentinelle: %+v", c)
	}
	if b.r.Esito != EsitoConforme || b.r.Esito.CodiceUscita() != UscitaConforme || b.r.Motivo != "" || len(b.r.Dettagli) != 0 {
		t.Fatalf("l'uscita non dipende dalle sentinelle delegate: %s, dettagli %+v", b.r.PrimaRiga(), b.r.Dettagli)
	}
	ch := b.r.Chiusura
	if ch.Conclusione != ConclusioneConEsterni || len(ch.Esterni) != 1 || ch.Esterni[0].Nome != ControlloSentinelle ||
		ch.Esterni[0].Prova != "A1c-L4D-01" || !strings.Contains(ch.Esterni[0].Motivo, `"copia_acme"`) {
		t.Errorf("le sentinelle nella chiusura: %+v", ch)
	}
	// T-B6-219: la riga dice sentinelle e impronte di contenuto, che A1c-L4D-01 verifica sulla copia intatta (R117 b).
	if !strings.Contains(b.r.Testo(), "\n  - sentinelle: delegato [obbligatorio_esterno, non_eseguito] — sentinelle e impronte di contenuto verificate fuori dal runner da A1c-L4D-01") ||
		!strings.Contains(classeDi(SedeControllo, ControlloSentinelle).Motivazione, "le sentinelle e le impronte di contenuto della copia") {
		t.Errorf("il riepilogo:\n%s", b.r.Testo())
	}
	vecchia := *sent
	vecchia.delegato = false
	if b := corsa(vecchia); b.r.Esito.CodiceUscita() != UscitaNonEseguito {
		t.Errorf("senza la delega la riga è NON ESEGUITA e l'uscita 3: %s", b.r.PrimaRiga())
	}
}

// TestRispostaR114LaMisuraNelBanco (R114, precisata dall'utente il 07/10): sugli export la lettura del vecchio motore
// non c'è. La misura del cliente lo dichiara: i due decisi sono esclusi dal denominatore con senza_lettura_vecchia,
// invece di contare «nessuna correzione»; il riepilogo dice il denominatore e la copertura, e la chiusura mette le
// correzioni fra gli informativi incompleti.
func TestRispostaR114LaMisuraNelBanco(t *testing.T) {
	r := corsaBanco(t, mutaBanco{}, true, false)
	if len(r.CorrezioniManuali) != 1 {
		t.Fatalf("correzioni: %+v", r.CorrezioniManuali)
	}
	x := r.CorrezioniManuali[0].Correzioni
	if x.Decisi != 2 || x.Valutabili != 0 || x.Esclusi.SenzaLetturaVecchia != 2 || x.Esclusi.Totale() != x.Decisi-x.Valutabili ||
		x.Prima != 0 || x.Dopo != 0 {
		t.Fatalf("la misura sugli export: %+v", x)
	}
	if txt := r.Testo(); !strings.Contains(txt, "(indicatore ricostruito delle correzioni necessarie): decisi 2, denominatore 0 valutabili (copertura 0 su 2)") ||
		!strings.Contains(txt, "senza_lettura_vecchia 2") {
		t.Errorf("il riepilogo della misura:\n%s", txt)
	}
	trovata := false
	for _, v := range r.Chiusura.Informativi {
		if v.Nome == SezioneInformativaCorrezioni && strings.Contains(v.Motivo, "copertura 0 su 2") {
			trovata = true
		}
	}
	if !trovata {
		t.Errorf("le correzioni con la copertura incompleta fra gli informativi incompleti: %+v", r.Chiusura.Informativi)
	}
}
