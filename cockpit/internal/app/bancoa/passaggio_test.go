package bancoa

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/ancoraggio"
	"promatec/cockpit/internal/core/confronto"
	valut "promatec/cockpit/internal/core/valutazione"
)

// L1 — A1c-L1-31: il passaggio da valutazione a confronto (piano 6.4.6, «Il passaggio»; R42 B, R53 B; fase 0 di B6,
// CP.3, con l'emendamento F0-19): i record piatti di valutazione e i DTO di confronto hanno gli stessi campi, con gli
// stessi nomi, lo stesso ordine, gli stessi tipi e gli stessi tag (un confronto strutturale con reflect, perché i nomi dei
// tipi annidati sono diversi: F0-09); la copia di passaggio.go li porta tutti (i byte JSON della sorgente e della copia
// coincidono, con un valore pieno e con il valore zero); due copie dello stesso esito danno la stessa impronta di
// confronto.
//
// E la parità dei valori del motore che confronto ricopia come stringhe (T-B6-50): DTO costruiti con le costanti di
// ancoraggio e di valutazione e con gli stati di migrations/0001, poi i badge e gli esiti controllati. La freccia verso
// ancoraggio vive solo in questa prova (nel codice il banco non lo importa).
//
// I clienti di questi test sono inventati: vedi scena_banco_test.go.

// modulo: il percorso del modulo; i tipi dichiarati fuori (uuid.UUID, time.Time) si confrontano per identità.
const moduloCockpit = "promatec/cockpit"

// stessaForma: due tipi hanno la stessa forma (CP.3):
//  1. un tipo dichiarato fuori dal modulo (uuid.UUID, time.Time) dev'essere lo stesso reflect.Type;
//  2. lo stesso Kind;
//  3. per le struct del modulo, lo stesso numero di campi e, indice per indice, lo stesso Name e lo stesso tag json,
//     nessun campo non esportato, e la ricorsione sul tipo;
//  4. per puntatori e slice, la ricorsione sull'elemento; mappe, array, interfacce, canali e funzioni sono vietati;
//  5. per i tipi di base, lo stesso reflect.Type: un tipo con nome del motore (ancoraggio.Collocazione) contro string
//     non passa.
func stessaForma(a, b reflect.Type, percorso string) []string {
	fuori := func(t reflect.Type) bool { return t.PkgPath() != "" && !strings.HasPrefix(t.PkgPath(), moduloCockpit) }
	switch {
	case fuori(a) || fuori(b):
		if a != b {
			return []string{fmt.Sprintf("%s: %v contro %v", percorso, a, b)}
		}
		return nil
	case a.Kind() != b.Kind():
		return []string{fmt.Sprintf("%s: %v contro %v", percorso, a.Kind(), b.Kind())}
	}
	switch a.Kind() {
	case reflect.Struct:
		if a.NumField() != b.NumField() {
			return []string{fmt.Sprintf("%s: %d campi contro %d", percorso, a.NumField(), b.NumField())}
		}
		var out []string
		for i := 0; i < a.NumField(); i++ {
			fa, fb := a.Field(i), b.Field(i)
			p := percorso + "." + fa.Name
			switch {
			case !fa.IsExported() || !fb.IsExported():
				out = append(out, p+": campo non esportato")
			case fa.Name != fb.Name:
				out = append(out, fmt.Sprintf("%s: campo %d, %s contro %s", percorso, i, fa.Name, fb.Name))
			case fa.Tag.Get("json") != fb.Tag.Get("json"):
				out = append(out, fmt.Sprintf("%s: tag %q contro %q", p, fa.Tag.Get("json"), fb.Tag.Get("json")))
			default:
				out = append(out, stessaForma(fa.Type, fb.Type, p)...)
			}
		}
		return out
	case reflect.Pointer, reflect.Slice:
		return stessaForma(a.Elem(), b.Elem(), percorso+"[]")
	case reflect.Map, reflect.Array, reflect.Interface, reflect.Chan, reflect.Func:
		return []string{fmt.Sprintf("%s: %v vietato nei record piatti", percorso, a.Kind())}
	}
	if a != b {
		return []string{fmt.Sprintf("%s: %v contro %v", percorso, a, b)}
	}
	return nil
}

func TestA1cL131StessaForma(t *testing.T) {
	coppie := []struct{ a, b any }{
		{valut.FileConfrontabile{}, confronto.File{}},
		{valut.ProdottoConfrontabile{}, confronto.ProdottoNuovo{}},
	}
	for _, c := range coppie {
		a, b := reflect.TypeOf(c.a), reflect.TypeOf(c.b)
		if d := stessaForma(a, b, a.Name()); len(d) > 0 {
			t.Errorf("%s e %s non hanno la stessa forma:\n%s", a, b, strings.Join(d, "\n"))
		}
	}
	// F0-19 e i gemelli di R114 e di R113 B ratificata: 19 campi nel vecchio, con CodiceLettoBase subito dopo
	// CodiceLetto, CodiceLettoMarcatore subito dopo CodiceLettoBase e RevisioneDa subito dopo CodiceLettoMarcatore, nei
	// due pacchetti (E2 §2.6).
	for _, tipo := range []reflect.Type{reflect.TypeOf(confronto.Vecchio{}), reflect.TypeOf(valut.VecchioPiatto{})} {
		if tipo.NumField() != 19 || tipo.Field(9).Name != "CodiceLetto" || tipo.Field(10).Name != "CodiceLettoBase" ||
			tipo.Field(11).Name != "CodiceLettoMarcatore" || tipo.Field(11).Tag.Get("json") != "codice_letto_marcatore" ||
			tipo.Field(12).Name != "RevisioneDa" || tipo.Field(12).Tag.Get("json") != "revisione_da" {
			t.Errorf("%s: %d campi, i campi 10, 11 e 12 sono %s, %s e %s (F0-19, R114, R113)", tipo, tipo.NumField(),
				tipo.Field(10).Name, tipo.Field(11).Name, tipo.Field(12).Name)
		}
	}
	// La guardia stessa: un tipo con nome del motore contro string non passa, un campo in più nemmeno.
	type conEnum struct {
		Collocazione ancoraggio.Collocazione `json:"collocazione"`
	}
	type conString struct {
		Collocazione string `json:"collocazione"`
	}
	type conDue struct {
		Collocazione string `json:"collocazione"`
		Altro        string `json:"altro"`
	}
	if len(stessaForma(reflect.TypeOf(conEnum{}), reflect.TypeOf(conString{}), "x")) == 0 ||
		len(stessaForma(reflect.TypeOf(conDue{}), reflect.TypeOf(conString{}), "x")) == 0 {
		t.Fatal("la guardia non vede un tipo con nome o un campo in più")
	}
}

// riempi: un valore con ogni campo non zero: stringhe, booleani, interi, UUID, istanti in UTC al millisecondo,
// puntatori non nil, slice di due elementi.
func riempi(v reflect.Value, n *int) {
	*n++
	switch v.Type() {
	case reflect.TypeOf(uuid.UUID{}):
		v.Set(reflect.ValueOf(uidACME(*n)))
		return
	case reflect.TypeOf(time.Time{}):
		v.Set(reflect.ValueOf(time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC).Add(time.Duration(*n) * time.Millisecond)))
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprintf("valore-%d", *n))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16:
		v.SetInt(int64(*n))
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		riempi(p.Elem(), n)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := 0; i < 2; i++ {
			riempi(s.Index(i), n)
		}
		v.Set(s)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			riempi(v.Field(i), n)
		}
	}
}

func jsonDi(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestA1cL131LaCopiaPortaTuttiICampi(t *testing.T) {
	var f valut.FileConfrontabile
	var p valut.ProdottoConfrontabile
	n := 0
	riempi(reflect.ValueOf(&f).Elem(), &n)
	riempi(reflect.ValueOf(&p).Elem(), &n)
	if a, b := jsonDi(t, f), jsonDi(t, inFile(f)); !bytes.Equal(a, b) {
		t.Errorf("la copia del file perde o cambia un campo:\n%s\n%s", a, b)
	}
	if a, b := jsonDi(t, p), jsonDi(t, inProdotto(p)); !bytes.Equal(a, b) {
		t.Errorf("la copia del prodotto perde o cambia un campo:\n%s\n%s", a, b)
	}
	if a, b := jsonDi(t, valut.FileConfrontabile{}), jsonDi(t, inFile(valut.FileConfrontabile{})); !bytes.Equal(a, b) {
		t.Errorf("la copia del valore zero:\n%s\n%s", a, b)
	}
	if a, b := jsonDi(t, valut.ProdottoConfrontabile{}), jsonDi(t, inProdotto(valut.ProdottoConfrontabile{})); !bytes.Equal(a, b) {
		t.Errorf("la copia del prodotto zero:\n%s\n%s", a, b)
	}
	// La copia non condivide la memoria della sorgente.
	copia := inFile(f)
	copia.Vecchio.Destinazione[0] = "cambiata"
	*copia.Vecchio.Componente = uuid.Nil
	if f.Vecchio.Destinazione[0] == "cambiata" || *f.Vecchio.Componente == uuid.Nil {
		t.Error("la copia condivide slice o puntatori con la sorgente")
	}
	// Due copie dello stesso esito danno la stessa impronta di confronto.
	ff := []valut.FileConfrontabile{f, f}
	ff[1].AllegatoID = uidACME(0xfff)
	pp := []valut.ProdottoConfrontabile{p}
	e1 := confronto.Confronta(inFiles(ff), inProdotti(pp), nil)
	e2 := confronto.Confronta(inFiles(ff), inProdotti(pp), nil)
	if e1.Impronta != e2.Impronta || len(e1.Impronta) != 64 {
		t.Fatalf("impronte %q e %q", e1.Impronta, e2.Impronta)
	}
}

// TestA1cL131IlMarcatoreArrivaAllaMisura (R114, il gemello CodiceLettoMarcatore; T-B6-50): il marcatore della lettura
// del vecchio motore passa da valutazione a confronto, e la misura lo vede. Con la stessa base, la lettura «A» contro
// il deciso «B» è una PrimaMarcatore, non una Prima (D-R114 B: il titolo del rapporto li somma, i due conteggi restano
// separati); la lettura «A»
// contro il deciso senza marcatore è un marcatore da un lato solo. Se la copia perde il campo, il primo caso diventa
// «da un lato solo» e il secondo sparisce.
func TestA1cL131IlMarcatoreArrivaAllaMisura(t *testing.T) {
	comp, doc := uidACME(0x31), uidACME(0x221)
	file := func(letto, marcatoreLetto, deciso, marcatore string) valut.FileConfrontabile {
		return valut.FileConfrontabile{AllegatoID: allBaseline,
			Vecchio: valut.VecchioPiatto{Stato: "confermata", Fonte: "nome_file", Codice: deciso, Base: "9123456", Marcatore: marcatore,
				Leggibile: true, CodiceLetto: letto, CodiceLettoBase: "9123456", CodiceLettoMarcatore: marcatoreLetto, Componente: &comp, Documento: &doc},
			Nuovo: valut.NuovoPiatto{Valutato: true, Basi: []string{"9123456"}, Collocazione: string(ancoraggio.CollocazioneRadice),
				Associazione: string(ancoraggio.AssociazioneCandidatoUnico), Revisioni: valut.RevisioniNonConfrontabili,
				MotivoRevisioni: valut.MotivoRevisioniVecchiaNonLetta,
				Candidati: []valut.CandidatoPiatto{{Target: ancoraggio.RifComponente(comp), Livello: "componente", Base: "9123456",
					Autorita: string(ancoraggio.AutoritaConfermata)}}}}
	}
	for _, c := range []struct {
		nome   string
		f      valut.FileConfrontabile
		misura confronto.CorrezioniManuali
	}{
		{"marcatori scritti e diversi", file("9123456A", "A", "9123456B", "B"),
			confronto.CorrezioniManuali{Decisi: 1, Valutabili: 1, PrimaMarcatore: 1}},
		{"marcatore solo nella lettura", file("9123456A", "A", "9123456", ""),
			confronto.CorrezioniManuali{Decisi: 1, Valutabili: 1, MarcatoreSoloDaUnLato: 1}},
	} {
		e := confronto.Confronta(inFiles([]valut.FileConfrontabile{c.f}), nil, nil)
		if e.File[0].Badge != confronto.BadgeUguale || e.Correzioni != c.misura {
			t.Errorf("%s: badge %s, misura %+v, attesa %+v", c.nome, e.File[0].Badge, e.Correzioni, c.misura)
		}
	}
}

// enumDelloSchema: i valori di un tipo enumerato di migrations/0001, letti dal file (gli stati che confronto riscrive
// come stringhe devono stare fra questi).
func enumDelloSchema(t *testing.T, tipo string) []string {
	t.Helper()
	b, err := os.ReadFile("../../../migrations/0001_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`CREATE TYPE ` + tipo + `\s+AS ENUM \(([^)]*)\)`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("il tipo %s non è in migrations/0001", tipo)
	}
	var out []string
	for _, v := range strings.Split(string(m[1]), ",") {
		out = append(out, strings.Trim(strings.TrimSpace(v), "'"))
	}
	return out
}

// TestParitaDeiValoriDelMotore (T-B6-50): confronto riscrive come stringhe i valori del motore e del DB, perché non
// importa il motore. Qui i DTO si costruiscono con le costanti vere, e i badge e gli esiti devono essere quelli della
// tavola: se un valore cambia nel motore, questa prova cade invece di un badge che cambia in silenzio.
func TestParitaDeiValoriDelMotore(t *testing.T) {
	stati, fonti := enumDelloSchema(t, "stato_proposta"), enumDelloSchema(t, "fonte_proposta")
	for _, v := range []string{"aperta", "confermata", "scartata", "duplicato"} {
		if !dentro(v, stati) {
			t.Errorf("stato_proposta %q non è nello schema: %v", v, stati)
		}
	}
	if !dentro("operatore", fonti) {
		t.Errorf("fonte_proposta operatore non è nello schema: %v", fonti)
	}
	comp := uidACME(0x31)
	doc := uidACME(0x221)
	valutato := func(c ...confronto.Candidato) confronto.Nuovo {
		return confronto.Nuovo{Valutato: true, Basi: []string{"9123456"}, Candidati: c, Collocazione: string(ancoraggio.CollocazioneRadice),
			Associazione: string(ancoraggio.AssociazioneCandidatoUnico), Revisioni: valut.RevisioniNonConfrontabili,
			MotivoRevisioni: valut.MotivoRevisioniVecchiaNonLetta}
	}
	candComp := confronto.Candidato{Target: ancoraggio.RifComponente(comp), Livello: "componente", Base: "9123456", Autorita: string(ancoraggio.AutoritaConfermata)}
	candScen := confronto.Candidato{Target: "scenario:ACME-SCENARIO:1", Livello: "prodotto", Base: "9123456", Autorita: string(ancoraggio.AutoritaScenario)}

	badge := func(f confronto.File) (confronto.Badge, string, confronto.IndicatoreRevisione) {
		e := confronto.Confronta([]confronto.File{f}, nil, nil)
		return e.File[0].Badge, e.File[0].Motivo, e.File[0].Revisione
	}
	// Il componente nella forma di ancoraggio.RifComponente: il file deciso è uguale.
	if b, m, _ := badge(confronto.File{AllegatoID: allBaseline, Vecchio: confronto.Vecchio{Stato: "confermata", Base: "9123456", Componente: &comp, Documento: &doc},
		Nuovo: valutato(candComp)}); b != confronto.BadgeUguale {
		t.Errorf("deciso con il componente proposto: %s %s", b, m)
	}
	// La collocazione fuori richiesta del motore, su un file non deciso.
	n := valutato()
	n.Collocazione, n.Associazione = string(ancoraggio.CollocazioneFuoriRichiesta), string(ancoraggio.AssociazioneNessunCandidato)
	if b, _, _ := badge(confronto.File{AllegatoID: allFuori, Nuovo: n}); b != confronto.BadgeFuoriRichiesta {
		t.Errorf("fuori richiesta: %s", b)
	}
	// La proposta scartata dello schema, con un candidato: diverso, proposta_scartata.
	if b, m, _ := badge(confronto.File{AllegatoID: allRadice, Vecchio: confronto.Vecchio{Stato: "scartata", Fonte: "nome_file", Base: "9123456"},
		Nuovo: valutato(candComp)}); b != confronto.BadgeDiverso || m != confronto.MotivoPropostaScartata {
		t.Errorf("proposta scartata: %s %s", b, m)
	}
	// I valori delle revisioni di valutazione.
	for revisioni, valore := range map[string]string{valut.RevisioniUguali: confronto.RevisioneUguale, valut.RevisioniDiverse: confronto.RevisioneDiversa,
		valut.RevisioniNonConfrontabili: confronto.RevisioneNonDeterminabile} {
		n := valutato(candComp)
		n.Revisioni = revisioni
		if _, _, ind := badge(confronto.File{AllegatoID: allRadice, Nuovo: n}); ind.Valore != valore {
			t.Errorf("revisioni %q → indicatore %q, atteso %q", revisioni, ind.Valore, valore)
		}
	}
	// Le correzioni manuali (R114, precisata dall'utente il 07/10), con i valori del motore e dello schema.
	correzioni := func(v confronto.Vecchio, n confronto.Nuovo) confronto.CorrezioniManuali {
		v.Stato, v.Componente, v.Documento = "confermata", &comp, &doc
		return confronto.Confronta([]confronto.File{{AllegatoID: allBaseline, Vecchio: v, Nuovo: n}}, nil, nil).Correzioni
	}
	// La fonte operatore dello schema, senza la lettura del vecchio motore: la sola origine manuale non è una correzione
	// «prima», ma un escluso (solo_origine_manuale).
	if c := correzioni(confronto.Vecchio{Fonte: "operatore", Base: "9123456"}, valutato(candComp)); c != (confronto.CorrezioniManuali{Decisi: 1,
		Esclusi: confronto.EsclusiCorrezioni{SoloOrigineManuale: 1}}) {
		t.Errorf("fonte operatore: %+v", c)
	}
	// Il motivo dei file di un thread che il motore A non valuta (valutazione.MotivoFileThreadNonValutato), che confronto
	// riscrive come stringa: il deciso esce con nuovo_non_valutato (T-B6-182).
	letto := confronto.Vecchio{Fonte: "nome_file", Base: "9123456", CodiceLetto: "9123456", CodiceLettoBase: "9123456"}
	if c := correzioni(letto, confronto.Nuovo{Valutato: false, Motivo: valut.MotivoFileThreadNonValutato}); c != (confronto.CorrezioniManuali{Decisi: 1,
		Esclusi: confronto.EsclusiCorrezioni{NuovoNonValutato: 1}}) {
		t.Errorf("thread non valutato: %+v", c)
	}
	// Le associazioni ambigua e discordante di ancoraggio, con il solo componente deciso fra i candidati: il badge resta
	// uguale, e l'ambiguità si conta in «dopo».
	for _, a := range []ancoraggio.Associazione{ancoraggio.AssociazioneAmbiguo, ancoraggio.AssociazioneDiscordante} {
		n := valutato(candComp)
		n.Associazione = string(a)
		if c := correzioni(letto, n); c != (confronto.CorrezioniManuali{Decisi: 1, Valutabili: 1, Dopo: 1, Ambiguita: 1}) {
			t.Errorf("associazione %s: %+v", a, c)
		}
	}

	esito := func(n confronto.Nuovo, a confronto.FileAtteso) confronto.EsitoFileAtteso {
		a.AllegatoID, a.Sezione = allRadice, confronto.SezioneScenario
		r := confronto.ConfrontaConAtteso([]confronto.File{{AllegatoID: allRadice, Nuovo: n}}, confronto.Atteso{File: []confronto.FileAtteso{a}})
		return r[0]
	}
	// candidato_scenario con l'autorità scenario e l'associazione candidato_unico del motore: corretto.
	if r := esito(valutato(candScen), confronto.FileAtteso{Atteso: confronto.AttesoRadice, StatoAtteso: confronto.StatoAttesoCandidatoScenario,
		TargetBase: "9123456"}); r.Esito != confronto.EsitoCorretto {
		t.Errorf("candidato dello scenario: %+v", r)
	}
	// L'associazione ambigua del motore: ambiguo.
	amb := valutato(candScen, confronto.Candidato{Target: "scenario:ACME-SCENARIO:2", Livello: "prodotto", Base: "9123457", Autorita: string(ancoraggio.AutoritaScenario)})
	amb.Associazione = string(ancoraggio.AssociazioneAmbiguo)
	if r := esito(amb, confronto.FileAtteso{Atteso: confronto.AttesoRadice, TargetBase: "9123456"}); r.Esito != confronto.EsitoAmbiguo {
		t.Errorf("ambiguo: %+v", r)
	}
	// L'associazione discordante del motore, con la base attesa fra i candidati: ambiguo anche lei (R-112: confronto
	// riconosce il valore come stringa, e se ancoraggio lo cambiasse l'ambiguità discordante sparirebbe in silenzio).
	dis2 := valutato(candScen, confronto.Candidato{Target: "scenario:ACME-SCENARIO:2", Livello: "prodotto", Base: "9123457", Autorita: string(ancoraggio.AutoritaScenario)})
	dis2.Associazione = string(ancoraggio.AssociazioneDiscordante)
	if r := esito(dis2, confronto.FileAtteso{Atteso: confronto.AttesoRadice, TargetBase: "9123456"}); r.Esito != confronto.EsitoAmbiguo {
		t.Errorf("discordante: %+v", r)
	}
	// La collocazione non determinabile, senza candidati: senza risposta.
	nd := valutato()
	nd.Collocazione, nd.Associazione = string(ancoraggio.CollocazioneNonDeterminabile), string(ancoraggio.AssociazioneNessunCandidato)
	if r := esito(nd, confronto.FileAtteso{Atteso: confronto.AttesoRadice, TargetBase: "9123456"}); r.Esito != confronto.EsitoMancante ||
		r.Motivo != confronto.MotivoAttesoNonDeterminabile {
		t.Errorf("non determinabile: %+v", r)
	}
	// La collocazione figlio del motore, con la radice: corretto.
	fi := valutato(confronto.Candidato{Target: "nodo:acme:1", Livello: "componente", Base: "9123457", Autorita: string(ancoraggio.AutoritaProposta),
		Radici: []string{"9123456"}})
	fi.Collocazione = string(ancoraggio.CollocazioneFiglio)
	if r := esito(fi, confronto.FileAtteso{Atteso: confronto.AttesoFiglio, TargetBase: "9123457", Radici: []string{"9123456"}}); r.Esito != confronto.EsitoCorretto {
		t.Errorf("figlio: %+v", r)
	}
	// Le revisioni nuove discordi di valutazione: la nota della revisione non confrontabile (R-46).
	dis := valutato(candComp)
	dis.MotivoRevisioni = valut.MotivoRevisioniNuoveDiscordi
	e := confronto.Confronta([]confronto.File{{AllegatoID: allRadice, Vecchio: confronto.Vecchio{Codice: "9123456A", Base: "9123456"}, Nuovo: dis}}, nil, nil)
	if len(e.Diagnostiche) != 1 {
		t.Errorf("revisioni nuove discordi: %+v", e.Diagnostiche)
	}
}
