package bancoa

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/estrazione/evidenze"
	"promatec/cockpit/internal/core/inbox/classificazione/motorea"
	"promatec/cockpit/internal/core/registro/regole/grammatica"
	valut "promatec/cockpit/internal/core/valutazione"
)

// L1 — le sezioni del rapporto dei modi di A1c, una per una, su ingressi sintetici (piano 6.4.9, «Il rapporto»; le note
// delle revisioni di B6):
//   - i messaggi fuori RFQ del caricatore vengono solo dai casi senza thread (T-B6-04, D-V1-3);
//   - il motivo di un esito contro l'atteso si divide sulle virgole (T-B6-53);
//   - le correzioni manuali per cliente sui soli thread valutati, con i thread non valutati a parte (D5): la misura di
//     confronto sommata campo per campo (R114, precisata dall'utente il 07/10), e fuori dalla misura la stessa base su
//     un altro target (T-B6-51, T-B6-191) e la revisione solo in colonna (R-65);
//   - senza_caso: senza il caso la richiesta dello scenario perde il segmento dichiarato (R48 A);
//   - il censimento forma per forma; il profilo dei limiti contro l'indice e i tetti (R43 B);
//   - la fonte attesa dei prodotti dello scenario accanto a quella calcolata (PO-29; R109, precisata dall'utente il 07/10:
//     la regola del runner; la derivazione esplicita dagli attesi la provano le prove di fonti_scenario_test.go).
//
// I clienti di questi test sono inventati: vedi scena_banco_test.go.

func TestMessaggiFuoriSoloDaiCasiSenzaThread(t *testing.T) {
	th := threadScenario
	b := &banco{ingressi: valut.Ingressi{Versione: 1, Casi: []valut.IngressoCaso{
		{ID: "con-thread", ThreadID: &th, ClienteID: clienteACME, Messaggi: []uuid.UUID{msgScenario}},
		{ID: "elenco", ClienteID: clienteACME, Messaggi: []uuid.UUID{msgFuori2, msgFuori1, msgFuori2}},
	}}}
	got := b.messaggiFuori()
	if len(got) != 2 || got[0] != msgFuori1 || got[1] != msgFuori2 {
		t.Fatalf("messaggi fuori RFQ: %v (solo i casi senza thread, una volta, in ordine)", got)
	}
}

func TestIlMotivoDellEsitoSiDivide(t *testing.T) {
	et := valut.EsitoThread{ThreadID: threadScenario, ClienteID: clienteACME}
	tr := traduzione{voci: []voceTradotta{{voce: VoceAttesa{Percorso: "scenario_inoltro.file[0]", Atteso: "figlio"}, thread: threadScenario,
		risolta: true, file: confronto.FileAtteso{AllegatoID: allFiglio, Sezione: confronto.SezioneScenario}}}}
	ce := confronto.Esito{ControAtteso: []confronto.EsitoFileAtteso{
		{AllegatoID: allFiglio, Sezione: confronto.SezioneScenario, Esito: confronto.EsitoAmbiguo, Peso: confronto.PesoAstensione,
			Motivo: confronto.MotivoAttesoRadiciInPiu + "," + confronto.MotivoAttesoAutoritaDiversa},
		{AllegatoID: allFuori, Esito: confronto.EsitoNonCoperto, Peso: confronto.PesoFuoriDalGate, Motivo: confronto.MotivoAttesoSenzaVoce},
	}}
	tb := ThreadBanco{File: []FileBanco{{AllegatoID: allFiglio}, {AllegatoID: allFuori}}}
	ev, nonCoperti := esitiDelleVoci(et, ce, tr, &tb, "")
	if nonCoperti != 1 || len(ev) != 1 || ev[0].Percorso != "scenario_inoltro.file[0]" || ev[0].Atteso != "figlio" {
		t.Fatalf("esiti per il gate %+v, non coperti %d", ev, nonCoperti)
	}
	m := tb.File[0].Esiti[0].Motivi
	if len(m) != 2 || m[0] != confronto.MotivoAttesoRadiciInPiu || m[1] != confronto.MotivoAttesoAutoritaDiversa || tb.File[0].Esiti[0].Percorso == "" {
		t.Errorf("il motivo ambiguo con due voci: %+v", tb.File[0].Esiti)
	}
}

// TestCorrezioniPerCliente (R114, precisata dall'utente il 07/10): la misura di ogni thread valutato si somma campo per
// campo, senza ricalcolarla (due thread con tutti i campi diversi da zero, e ogni campo della somma è la somma dei
// due); il thread non valutato sta a parte (D5). Fuori dalla misura: la stessa base su un altro target, solo sui decisi
// (T-B6-191), e la revisione solo in colonna su ogni file.
func TestCorrezioniPerCliente(t *testing.T) {
	doc := uidACME(0x221)
	file := func(documento bool, badge confronto.Badge, motivo string) FileBanco {
		f := FileBanco{Riga: confronto.EsitoFile{File: confronto.File{
			Vecchio: confronto.Vecchio{Fonte: "nome_file", CodiceLetto: "9123456A", CodiceLettoBase: "9123456", Base: "9123456", Codice: "9123456A"},
			Nuovo:   confronto.Nuovo{Valutato: true, MotivoRevisioni: valut.MotivoRevisioniSoloInColonna}},
			Badge: badge, Motivo: motivo}}
		if documento {
			f.Riga.File.Vecchio.Documento = &doc
		}
		return f
	}
	// misura: una misura con ogni campo diverso da zero, a partire da n (i campi in ordine: n, n+1, …).
	misura := func(n int) confronto.CorrezioniManuali {
		var m confronto.CorrezioniManuali
		riempi(reflect.ValueOf(&m).Elem(), &n)
		return m
	}
	a, b, z := misura(0), misura(100), misura(1000)
	thread := []ThreadBanco{
		{ClienteID: clienteACME, Valutato: true, Correzioni: a, File: []FileBanco{
			file(true, confronto.BadgeRegressioneSuConfermata, confronto.MotivoStessaBaseAltroTarget),
			file(false, confronto.BadgeRegressioneSuConfermata, confronto.MotivoStessaBaseAltroTarget), // non deciso: non conta
			file(true, confronto.BadgeRegressioneSuConfermata, confronto.MotivoComponenteNonProposto),
		}},
		{ClienteID: clienteACME, Valutato: true, Correzioni: b},
		{ClienteID: clienteACME, Valutato: false, Correzioni: z},
	}
	c := correzioniPerCliente(thread)
	if len(c) != 1 {
		t.Fatalf("un cliente: %+v", c)
	}
	x := c[0]
	sa, sb, sx := reflect.ValueOf(a), reflect.ValueOf(b), reflect.ValueOf(x.Correzioni)
	var campi func(va, vb, vx reflect.Value, nome string)
	campi = func(va, vb, vx reflect.Value, nome string) {
		for i := 0; i < vx.NumField(); i++ {
			n := nome + vx.Type().Field(i).Name
			if vx.Field(i).Kind() == reflect.Struct {
				campi(va.Field(i), vb.Field(i), vx.Field(i), n+".")
				continue
			}
			if vx.Field(i).Int() != va.Field(i).Int()+vb.Field(i).Int() {
				t.Errorf("%s: %d, attesa la somma dei due thread %d", n, vx.Field(i).Int(), va.Field(i).Int()+vb.Field(i).Int())
			}
		}
	}
	campi(sa, sb, sx, "")
	if x.Thread != 2 || x.ThreadNonValutati != 1 || x.CorrezioniNonValutati != z {
		t.Errorf("i thread non valutati a parte (D5): %+v", x)
	}
	if x.StessaBaseAltroTarget != 1 || x.RevisioneSoloInColonna != (RevisioneInColonna{NessunaRegola: 3}) {
		t.Errorf("fuori dalla misura: stessa base %d (solo i decisi), solo in colonna %+v", x.StessaBaseAltroTarget, x.RevisioneSoloInColonna)
	}
}

// TestIlTestoDellaMisura (R114, precisata dall'utente il 07/10): il riepilogo presenta la misura come «indicatore
// ricostruito delle correzioni necessarie», con il denominatore, la copertura e gli esclusi per motivo; il titolo «prima»
// è base più marcatore, con le due parti accanto e il marcatore da un solo lato a parte (D-R114 B, decisa dall'utente
// l'08/10); dopo con le tre parti; nessuna misura di tempo.
func TestIlTestoDellaMisura(t *testing.T) {
	x := confronto.CorrezioniManuali{Decisi: 9, Valutabili: 4, Esclusi: confronto.EsclusiCorrezioni{SoloOrigineManuale: 1, SenzaLetturaVecchia: 2,
		CodiceNonLeggibile: 1, NuovoNonValutato: 1}, Prima: 1, PrimaMarcatore: 1, MarcatoreSoloDaUnLato: 1, Dopo: 3, FalseAssociazioni: 1,
		Ambiguita: 1, Astensioni: 1}
	s := testoCorrezioni(CorrezioniCliente{ClienteID: clienteACME, Thread: 2, Correzioni: x, StessaBaseAltroTarget: 2, ThreadNonValutati: 1})
	for _, parte := range []string{
		"(indicatore ricostruito delle correzioni necessarie)", "decisi 9, denominatore 4 valutabili (copertura 4 su 9)",
		"esclusi per motivo: solo_origine_manuale 1, senza_lettura_vecchia 2, codice_non_leggibile 1, documento_senza_componente 0, nuovo_non_valutato 1",
		"prima 2 (base e marcatore, D-R114 B: cambi di base 1, marcatori diversi 1), a parte marcatore solo da un lato 1",
		"dopo 3 (false associazioni 1, ambiguità 1, astensioni 1)", "fuori dalla misura: stessa base su un altro target 2",
	} {
		if !strings.Contains(s, parte) {
			t.Errorf("manca %q in:\n%s", parte, s)
		}
	}
	if m := regexp.MustCompile(`(?i)\b(tempo|minut\w*|ore|secondi|giorni|risparm\w*)\b|%`).FindString(s); m != "" {
		t.Errorf("una misura di tempo o una percentuale nel testo (%q):\n%s", m, s)
	}
	if strings.Contains(s, "per stringa") {
		t.Errorf("il «prima» per stringa non c'è più:\n%s", s)
	}
}

// TestSenzaCasoECensimentoNelBanco: sulla scena, senza il caso lo scenario non ha il segmento dichiarato «scenario»
// (R48 A); il censimento ha le letture del messaggio fuori RFQ, forma per forma.
func TestSenzaCasoECensimentoNelBanco(t *testing.T) {
	r, err := EseguiBanco(context.Background(), preparaBanco(t, mutaBanco{}).opzioniExport(false), nil)
	if err != nil || len(r.SenzaCaso) != 1 {
		t.Fatalf("senza_caso %+v (%v)", r.SenzaCaso, err)
	}
	s := r.SenzaCaso[0]
	if s.Uguale || strings.Join(s.ConCaso.Segmenti, ",") != "s:storia:1 (scenario)" || strings.Contains(strings.Join(s.SenzaCaso.Segmenti, ","), "scenario") {
		t.Errorf("con il caso %+v, senza %+v", s.ConCaso, s.SenzaCaso)
	}
	if len(r.Censimento) != 2 || r.Censimento[0].MessaggioID != msgFuori1 || r.Censimento[0].LettureIdentita != 1 ||
		len(r.Censimento[0].Letture) != 1 || r.Censimento[0].Letture[0].N != 1 || r.Censimento[1].LettureIdentita != 0 {
		t.Errorf("censimento: %+v", r.Censimento)
	}
}

func TestProfiloLimiti(t *testing.T) {
	doc := evidenze.DocumentoEvidenze{BundleID: "b", Unita: []evidenze.UnitaEvidenza{{ID: "u:a", Testo: "12345"}, {ID: "u:b", Testo: "123456789"}}}
	letture := []motorea.LetturaCodice{{UnitaID: "u:a"}, {UnitaID: "u:a"}, {UnitaID: "u:b"}}
	e := valut.Esito{VersioneLimiti: "limiti-acme-1", Thread: []valut.EsitoThread{{File: []valut.FileInterpretato{
		{Documento: doc, Interpretazione: motorea.Interpretazione{Letture: letture}}, {}}}}}
	ins := &motorea.InsiemeRegole{Indice: grammatica.IndiceRegole{Limiti: grammatica.Limiti{Riconoscimento: grammatica.LimitiRiconoscimento{
		MaxByteUnita: 8, MaxUnitaDocumento: 10, MaxLetturePerUnita: 10, MaxLettureDocumento: 2}}}}
	p := profiloLimiti(e, ins)
	o := p.Osservati
	if p.Documenti != 1 || o.MaxByteUnita != 9 || o.MaxUnitaDocumento != 2 || o.MaxLetturePerUnita != 2 || o.MaxLettureDocumento != 3 {
		t.Fatalf("osservati %+v su %d documenti", o, p.Documenti)
	}
	if strings.Join(p.OltreIndice, ",") != "max_byte_unita,max_letture_documento" || p.Tetti.MaxByteUnita == 0 || p.VersioneLimiti != "limiti-acme-1" {
		t.Errorf("oltre l'indice %v, tetti %+v", p.OltreIndice, p.Tetti)
	}
}

// TestFonteAttesaDelloScenario (PO-29; R109, precisata dall'utente il 07/10): i file attesi radice del prodotto; con
// uno STEP fra loro la fonte attesa è in_attesa_di_conferma, motivo documento_candidato, con quegli STEP come
// candidati; senza, assente. Le differenze dicono dove la fonte calcolata non coincide, senza un esito.
func TestFonteAttesaDelloScenario(t *testing.T) {
	ix := indiceFontiACME()
	tr := &traduzione{voci: []voceTradotta{voceScenarioACME(0, allFiglio, "radice", "9123456"), voceScenarioACME(1, allRadice, "radice", "9123456"),
		voceScenarioACME(2, allFuori, "fuori", "")}}
	pv := valut.ProdottoValutato{Rif: "identificativo:9123456", Base: motorea.BaseLetta{Normalizzata: "9123456"},
		Fonte: valut.FonteProdotto{Stato: valut.FonteAssente, Motivo: valut.MotivoFonteStepPresenteNonAnalizzato}}
	cf := fonteUnicaACME(t, tr, ix, pv)
	if cf.Atteso.Stato != string(valut.FonteInAttesaDiConferma) || cf.Atteso.Motivo != string(valut.MotivoFonteDocumentoCandidato) ||
		len(cf.Atteso.Candidati) != 1 || cf.Atteso.Candidati[0] != allFiglio {
		t.Fatalf("fonte attesa: %+v", cf.Atteso)
	}
	if strings.Join(cf.Differenze, ",") != "stato,motivo,candidati" {
		t.Errorf("differenze: %v", cf.Differenze)
	}
	// Senza STEP fra i file radice: assente; una fonte calcolata confermata o una BOM di lavoro non sono mai per lo scenario.
	tr = &traduzione{voci: []voceTradotta{voceScenarioACME(0, allRadice, "radice", "9123456")}}
	pv.Fonte = valut.FonteProdotto{Stato: valut.FonteConfermata}
	pv.Nodi = []valut.NodoBOM{{}}
	cf = fonteUnicaACME(t, tr, ix, pv)
	if cf.Atteso.Stato != string(valut.FonteAssente) || len(cf.Atteso.Candidati) != 0 ||
		strings.Join(cf.Differenze, ",") != "stato,confermata: mai per lo scenario,bom_di_lavoro: mai per lo scenario" {
		t.Errorf("senza STEP: %+v, differenze %v", cf.Atteso, cf.Differenze)
	}
}

// TestUnaDifferenzaPrevale (R44): una differenza prevale su un controllo non eseguito; senza differenze, il non eseguito
// dà NON ESEGUITO con i motivi; tutto eseguito senza differenze è conforme.
func TestUnaDifferenzaPrevale(t *testing.T) {
	b := &banco{r: RapportoBanco{Controlli: []Controllo{{Nome: "a", Stato: ControlloEseguito, Differenze: 2},
		{Nome: "b", Stato: ControlloNonEseguito, Motivo: "manca"}}}}
	b.chiudi()
	if b.r.Esito != EsitoConDifferenze || b.r.Differenze != 2 || b.r.Motivo != "" {
		t.Fatalf("1 prevale su 3: %+v", b.r)
	}
	b.r.Controlli[0].Differenze = 0
	b.chiudi()
	if b.r.Esito != EsitoNonEseguito || b.r.Motivo != "b: manca" {
		t.Fatalf("non eseguito: %+v", b.r)
	}
	b.r.Controlli = b.r.Controlli[:1]
	b.chiudi()
	if b.r.Esito != EsitoConforme {
		t.Fatalf("conforme: %+v", b.r)
	}
}
