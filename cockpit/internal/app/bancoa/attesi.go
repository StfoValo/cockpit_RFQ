package bancoa

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"promatec/cockpit/internal/core/confronto"
)

// Questo è l'unico file del modulo che importa la libreria YAML (G3; R40 e). È gopkg.in/yaml.v3 v3.0.1, il
// ripiego offline di R9: go.yaml.in/yaml/v3 non è nella cache dei moduli, e le due hanno la stessa API.

// Attesi: gli attesi della consegna A, nella parte che A1a legge: la testata e i casi_contratto. Le altre
// sezioni le leggerà A1c; qui sono note, quindi non sono chiavi sconosciute, ma non si interpretano.
type Attesi struct {
	Testata Testata
	Casi    []CasoContratto // nell'ordine del file
}

// Testata: le chiavi di primo livello che descrivono il file. È l'hash del file, nel manifest, a fare da
// versione (par.3.7.2): la testata si riporta nel rapporto e non decide niente.
type Testata struct {
	VersioneAttesi                                               int
	Data, Aggiornato, Stato, Piano, BaseRepository, Riservatezza string
}

// CasoContratto: un caso di casi_contratto. ID e profilo sono dati privati: compaiono solo nei rapporti del
// banco, mai nel codice né nei nomi delle prove (R47 c). Il profilo si lega al cliente nel manifest (D-09).
type CasoContratto struct {
	ID, Profilo, Livello string
	StatoAtteso          string // definito | riservato
	Decisione, DipendeDa string // metadati: un caso definito con dipende_da si valuta, e il rapporto lo annota
	OrigineFixture       string
	AllegatoID           string // lega il caso a un allegato: serve in A1c
	Nota                 string
	Contesto, Testo      string         // l'ingresso; il contesto nella grafia degli attesi (R19)
	Precondizioni        *Precondizioni // facoltative
	Atteso               []ChiaveAttesa // in ordine di chiave
}

// Gli stati attesi di un caso.
const (
	StatoAttesoDefinito  = "definito"
	StatoAttesoRiservato = "riservato"
)

// Precondizioni: che cosa il caso presuppone. In A1a le hanno solo casi sull'attributo della revisione in
// campo separato, che si valutano in A1b (A-C02).
type Precondizioni struct {
	BaseStrutturata          string
	EntitaCondivisaConCodice bool
}

// ChiaveAttesa: una chiave di atteso con il suo valore, come è scritto.
type ChiaveAttesa struct {
	Chiave string
	Valore ValoreAtteso
}

// ValoreAtteso: il valore di una chiave degli attesi. Il testo è quello scritto nel file, senza conversioni:
// una revisione «00» resta «00» anche se il YAML la risolverebbe come un intero (D4). Tipo: stringa |
// intero | booleano | nullo | lista | mappa. Una lista ha solo elementi semplici; una mappa non si confronta
// in A1a.
type ValoreAtteso struct {
	Tipo     string
	Testo    string
	Elementi []string
}

// I tipi di un valore atteso.
const (
	TipoStringa  = "stringa"
	TipoIntero   = "intero"
	TipoBooleano = "booleano"
	TipoNullo    = "nullo"
	TipoLista    = "lista"
	TipoMappa    = "mappa"
)

// String: il valore come si scrive nel rapporto.
func (v ValoreAtteso) String() string {
	switch v.Tipo {
	case TipoNullo:
		return "null"
	case TipoLista:
		return "[" + strings.Join(v.Elementi, ", ") + "]"
	case TipoMappa:
		return "{…}"
	}
	return v.Testo
}

// attesiYAML: il file come lo legge il decoder, con KnownFields: una chiave che non è un tag è un errore. Le
// sezioni che A1a non legge sono yaml.Node: si accettano senza interpretarle.
type attesiYAML struct {
	VersioneAttesi        *int       `yaml:"versione_attesi"`
	Data                  string     `yaml:"data"`
	Aggiornato            string     `yaml:"aggiornato"`
	Stato                 string     `yaml:"stato"`
	Piano                 string     `yaml:"piano"`
	BaseRepository        string     `yaml:"base_repository"`
	Riservatezza          string     `yaml:"riservatezza"`
	CasiContratto         []casoYAML `yaml:"casi_contratto"`
	ContrattoRunner       yaml.Node  `yaml:"contratto_runner"`
	Fonti                 yaml.Node  `yaml:"fonti"`
	Profili               yaml.Node  `yaml:"profili"`
	ScenarioInoltro       yaml.Node  `yaml:"scenario_inoltro"`
	BaselineDecisioni     yaml.Node  `yaml:"baseline_decisioni"`
	RealiArchivi          yaml.Node  `yaml:"reali_archivi"`
	CasiIntegrazione      yaml.Node  `yaml:"casi_integrazione"`
	RiservatiNonBloccanti yaml.Node  `yaml:"riservati_non_bloccanti"`
	PrerequisitiGateL4    yaml.Node  `yaml:"prerequisiti_gate_l4"`
	Gate                  yaml.Node  `yaml:"gate"`
	DecisioniSuccessive   yaml.Node  `yaml:"decisioni_successive_da_rivedere"`
}

type casoYAML struct {
	ID             string             `yaml:"id"`
	Profilo        string             `yaml:"profilo"`
	Livello        string             `yaml:"livello"`
	StatoAtteso    string             `yaml:"stato_atteso"`
	Decisione      string             `yaml:"decisione"`
	DipendeDa      string             `yaml:"dipende_da"`
	OrigineFixture string             `yaml:"origine_fixture"`
	AllegatoID     string             `yaml:"allegato_id"`
	Nota           string             `yaml:"nota"`
	Input          *inputYAML         `yaml:"input"`
	Precondizioni  *precondizioniYAML `yaml:"precondizioni"`
	Atteso         yaml.Node          `yaml:"atteso"`
}

type inputYAML struct {
	Contesto *string `yaml:"contesto"`
	Testo    *string `yaml:"testo"`
}

type precondizioniYAML struct {
	BaseStrutturata          string `yaml:"base_strutturata"`
	EntitaCondivisaConCodice bool   `yaml:"entita_condivisa_con_codice"`
}

// LeggiAttesi: la decodifica degli attesi YAML. In A1a legge la testata e i casi_contratto; le altre sezioni
// le aggiunge A1c. Una chiave sconosciuta (di primo livello, in un caso o nel suo atteso) è un errore: una
// chiave nuova negli attesi non si ignora. Sono errori anche una chiave ripetuta (la rifiuta la libreria), un
// secondo documento nel file, un caso senza id, profilo, contesto o testo, un id ripetuto, uno stato atteso
// fuori elenco. Il codice conosce solo i nomi neutri delle chiavi (R47 a): nessun nome di cliente sta qui.
// L'errore dice il caso e la chiave, mai il testo dell'ingresso.
func LeggiAttesi(raw []byte) (Attesi, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var f attesiYAML
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return Attesi{}, errors.New("attesi: il file è vuoto")
		}
		return Attesi{}, fmt.Errorf("attesi: %w", err)
	}
	var altro yaml.Node
	if err := dec.Decode(&altro); !errors.Is(err, io.EOF) {
		return Attesi{}, errors.New("attesi: il file contiene più di un documento YAML")
	}
	if f.VersioneAttesi == nil {
		return Attesi{}, errors.New("attesi: manca versione_attesi")
	}
	if f.CasiContratto == nil {
		return Attesi{}, errors.New("attesi: manca casi_contratto")
	}
	// Casi non è mai nil dopo la lettura, nemmeno con zero casi: nil vuol dire «attesi non letti» (VerificaRegole).
	a := Attesi{Casi: []CasoContratto{}, Testata: Testata{
		VersioneAttesi: *f.VersioneAttesi, Data: f.Data, Aggiornato: f.Aggiornato, Stato: f.Stato,
		Piano: f.Piano, BaseRepository: f.BaseRepository, Riservatezza: f.Riservatezza,
	}}
	visti := map[string]bool{}
	for i, c := range f.CasiContratto {
		k, err := caso(i, c)
		if err != nil {
			return Attesi{}, err
		}
		if visti[k.ID] {
			return Attesi{}, fmt.Errorf("attesi: casi_contratto[%d]: l'id %q compare due volte", i, k.ID)
		}
		visti[k.ID] = true
		a.Casi = append(a.Casi, k)
	}
	return a, nil
}

func caso(i int, c casoYAML) (CasoContratto, error) {
	p := fmt.Sprintf("attesi: casi_contratto[%d]", i)
	switch {
	case strings.TrimSpace(c.ID) == "":
		return CasoContratto{}, fmt.Errorf("%s: manca id", p)
	case strings.TrimSpace(c.Profilo) == "":
		return CasoContratto{}, fmt.Errorf("%s (%s): manca profilo", p, c.ID)
	case c.StatoAtteso != StatoAttesoDefinito && c.StatoAtteso != StatoAttesoRiservato:
		return CasoContratto{}, fmt.Errorf("%s (%s): stato_atteso %q fuori elenco (%s, %s)", p, c.ID, c.StatoAtteso, StatoAttesoDefinito, StatoAttesoRiservato)
	case c.Input == nil || c.Input.Contesto == nil || c.Input.Testo == nil:
		return CasoContratto{}, fmt.Errorf("%s (%s): input senza contesto o senza testo", p, c.ID)
	}
	k := CasoContratto{
		ID: c.ID, Profilo: c.Profilo, Livello: c.Livello, StatoAtteso: c.StatoAtteso,
		Decisione: c.Decisione, DipendeDa: c.DipendeDa, OrigineFixture: c.OrigineFixture,
		AllegatoID: c.AllegatoID, Nota: c.Nota,
		Contesto: *c.Input.Contesto, Testo: *c.Input.Testo,
	}
	if c.Precondizioni != nil {
		k.Precondizioni = &Precondizioni{
			BaseStrutturata:          c.Precondizioni.BaseStrutturata,
			EntitaCondivisaConCodice: c.Precondizioni.EntitaCondivisaConCodice,
		}
	}
	switch c.Atteso.Kind {
	case 0:
		return CasoContratto{}, fmt.Errorf("%s (%s): manca atteso", p, c.ID)
	case yaml.MappingNode:
	default:
		return CasoContratto{}, fmt.Errorf("%s (%s): atteso deve essere una mappa", p, c.ID)
	}
	viste := map[string]bool{}
	for j := 0; j+1 < len(c.Atteso.Content); j += 2 {
		chiave := c.Atteso.Content[j].Value
		if viste[chiave] {
			// sotto un yaml.Node la libreria non controlla i doppioni: lo si fa qui
			return CasoContratto{}, fmt.Errorf("%s (%s): atteso.%s: chiave ripetuta", p, c.ID, chiave)
		}
		viste[chiave] = true
		if !ChiaveNota(chiave) {
			return CasoContratto{}, fmt.Errorf("%s (%s): atteso.%s: chiave sconosciuta (una chiave nuova negli attesi non si ignora: va aggiunta alla tabella di traduzione.go)", p, c.ID, chiave)
		}
		k.Atteso = append(k.Atteso, ChiaveAttesa{Chiave: chiave, Valore: valoreDi(c.Atteso.Content[j+1])})
	}
	sort.SliceStable(k.Atteso, func(x, y int) bool { return k.Atteso[x].Chiave < k.Atteso[y].Chiave })
	return k, nil
}

// valoreDi: il valore di un nodo, con il testo scritto nel file. Un alias si segue.
func valoreDi(n *yaml.Node) ValoreAtteso {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			return ValoreAtteso{Tipo: TipoNullo}
		case "!!int":
			return ValoreAtteso{Tipo: TipoIntero, Testo: n.Value}
		case "!!bool":
			return ValoreAtteso{Tipo: TipoBooleano, Testo: strings.ToLower(n.Value)}
		}
		// stringhe, e tutto ciò che il YAML risolverebbe in altro (numeri con decimali, date): il testo com'è
		return ValoreAtteso{Tipo: TipoStringa, Testo: n.Value}
	case yaml.SequenceNode:
		v := ValoreAtteso{Tipo: TipoLista}
		for _, e := range n.Content {
			if e.Kind == yaml.AliasNode && e.Alias != nil {
				e = e.Alias
			}
			if e.Kind != yaml.ScalarNode {
				return ValoreAtteso{Tipo: TipoMappa}
			}
			v.Elementi = append(v.Elementi, e.Value)
		}
		return v
	}
	return ValoreAtteso{Tipo: TipoMappa}
}

// ---- A1c: le sezioni degli attesi (6.4.9) ----

// LeggiSezioni: la lettura stretta delle sezioni degli attesi che servono alle modalità dsn ed exports (piano 6.4.9,
// «Che cosa fa il runner con ogni sezione degli attesi»; R47 a, M-21). LeggiAttesi resta quella di A1a, che le accetta
// senza leggerle (A1a-BA): qui si leggono con i nomi neutri, e le traduce poi sezioni.go.
//
// Le chiavi che il runner conosce sono quelle che il piano nomina (6.4.6, 6.4.9; i nomi neutri di M-21): il codice non
// ha mai letto gli attesi veri (P-11). Una chiave che il runner non conosce non si ignora e non ferma la lettura: va in
// NonTradotte, con il suo percorso senza valori e con gli indici degli elenchi scritti «[]», e il controllo del runner
// n.5 la conta come differenza (uscita 1). Così la prima corsa sugli attesi veri dice tutte insieme le chiavi da
// aggiungere alla tabella, e nessuna resta fuori in silenzio. Un valore del tipo sbagliato, o un UUID che non si legge,
// va in Errori, con il percorso.
//
// Le sezioni descrittive (gate, riservati_non_bloccanti, prerequisiti_gate_l4) e le note si riportano nel rapporto come
// righe «percorso = valore», senza interpretarle; da casi_integrazione e da conferma_albero si raccolgono gli ID e gli
// sha256, per il controllo n.2, e i percorsi delle righe della conferma dell'albero, che il runner non verifica e che il
// rapporto porta in una sezione informativa (R-109; D-R116 I, decisa dall'utente l'08/10); da fonti solo gli sha256,
// per il controllo n.3. Nelle tre sezioni a forma libera le chiavi che non sono ID vanno in ChiaviLibere, un elenco
// informativo che non cambia l'uscita (R-109); da B6c anche le chiavi descrittive delle sezioni e delle voci (descrittive,
// T-B6c-04, T-B6c-06, T-B6c-08). contratto_runner e profili restano di A1a (i profili si legano ai clienti nel
// manifest, D-09).
//
// L'errore è solo per un file che non è un documento YAML con una mappa in cima: allora non si legge niente.
func LeggiSezioni(raw []byte) (SezioniAttesi, error) {
	var doc yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(raw)).Decode(&doc); err != nil {
		return SezioniAttesi{}, fmt.Errorf("attesi: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || risolvi(doc.Content[0]).Kind != yaml.MappingNode {
		return SezioniAttesi{}, errors.New("attesi: serve un documento YAML con una mappa in cima")
	}
	l := &lettoreSezioni{}
	cima := risolvi(doc.Content[0])
	viste := map[string]bool{}
	for i := 0; i+1 < len(cima.Content); i += 2 {
		k, v := cima.Content[i].Value, cima.Content[i+1]
		if viste[k] {
			l.errore(k, "chiave ripetuta")
			continue
		}
		viste[k] = true
		switch k {
		case "versione_attesi", "data", "aggiornato", "stato", "piano", "base_repository", "riservatezza",
			"casi_contratto", "contratto_runner", "profili":
			// di A1a: le legge LeggiAttesi
		case "scenario_inoltro":
			l.scenario(v, k)
		case "baseline_decisioni":
			l.out.Baseline = l.voci(v, k, confronto.SezioneBaseline, chiaviBaseline)
		case "reali_archivi":
			l.out.Reali = l.voci(v, k, confronto.SezioneReali, chiaviReali)
		case "decisioni_successive_da_rivedere":
			l.daRivedere(v, k)
		case "casi_integrazione":
			l.out.Integrazione = append(l.out.Integrazione, raccogliID(v, k)...)
			l.out.ChiaviLibere = append(l.out.ChiaviLibere, chiaviLibere(v, k)...)
		case "fonti":
			for _, id := range raccogliID(v, k) {
				if id.Chiave == "sha256" {
					l.out.Fonti = append(l.out.Fonti, strings.ToLower(id.Valore))
				}
			}
			l.out.ChiaviLibere = append(l.out.ChiaviLibere, chiaviLibere(v, k)...)
		case "gate", "riservati_non_bloccanti", "prerequisiti_gate_l4":
			l.out.Riportate = append(l.out.Riportate, riporta(v, k)...)
		default:
			l.nonTradotta(k)
		}
	}
	sort.Strings(l.out.NonTradotte)
	l.out.NonTradotte = unici(l.out.NonTradotte)
	sort.Strings(l.out.Fonti)
	l.out.Fonti = unici(l.out.Fonti)
	l.out.ChiaviLibere = unici(l.out.ChiaviLibere)
	return l.out, nil
}

// Le chiavi che il runner conosce in una voce di file, per sezione (6.4.6, 6.4.9; i nomi neutri di M-21). Le chiavi
// comuni a tutte le voci: l'identità del file, le note, lo stato atteso del caso e la domanda da cui dipende.
//
// Da B6c (la prima corsa sugli attesi veri, 08/10) anche le chiavi che la corsa ha detto non tradotte, ognuna con il suo
// trattamento: thread_id della voce (il thread dell'allegato, n.2); codice dei file dello scenario (descrittiva, fra le
// chiavi libere: T-B6c-04); atteso della C5 (T-B6c-07); nell_export_proposte, il nome neutro della riga dell'export
// della C5 (come M-21, solo lessicale: la chiave di prima non ha alias).
var (
	chiaviVoce     = []string{"id", "allegato_id", "sha256", "nome_file", "nota", "dipende_da", "stato_atteso"}
	chiaviScenario = append(append([]string(nil), chiaviVoce...),
		"atteso", "atteso_stato", "atteso_target_base", "radici", "autorita", "scritture_consentite", "codice")
	chiaviBaseline = append(append([]string(nil), chiaviVoce...),
		"atteso", "decisione_proposta", "decisione_documento", "componente_id_proposta_vuoto_preservato", "thread_id")
	chiaviReali = append(append([]string(nil), chiaviVoce...),
		"atteso", "target_presente", "esito_con_target")
	chiaviDaRivedere = append(append([]string(nil), chiaviVoce...),
		"decisione_proposta", "decisione_documento", "nell_export_proposte", "sovrascritta_dal_motore",
		"componente_id_proposta_vuoto_preservato", "in_baseline", "conta_nel_gate", "mostrata_con_badge_di_confronto",
		"anomalie", "etichetta", "thread_id", "atteso")
	// chiaviDecisione: i campi di una riga di proposta o di documento attesa (6.4.9, il controllo (1) della baseline e
	// gli invarianti della C5): le colonne di documento_proposta e di documento che la fotografia porta. Da B6c anche
	// proposta_id (la proposta), e per il documento la provenienza (allegato_id, messaggio_id), il thread, l'estensione e
	// chi l'ha confermato (T-B6c-02); quali valgono per quale riga lo dice campiDellaRiga (sezioni.go).
	chiaviDecisione = []string{"codice", "rev", "componente_id", "documento_id", "stato", "fonte", "tipo", "tipo_proposto",
		"deciso_il", "deciso_da", "sostituito_da", "confermato_il", "sha256", "nome_file", "proposta_id", "allegato_id",
		"messaggio_id", "thread_id", "estensione", "confermato_da"}
	// invariantiBooleani: le chiavi booleane di una voce, che il runner porta come invarianti (6.4.9). Da B6c anche le
	// due della baseline che la corsa ha trovato dentro atteso: decisioni_invariate (il controllo (1)) e
	// revisione_confrontata_separatamente (l'indicatore di revisione, E-19).
	invariantiBooleani = []string{"componente_id_proposta_vuoto_preservato", "sovrascritta_dal_motore", "in_baseline",
		"conta_nel_gate", "mostrata_con_badge_di_confronto", "target_presente", "scritture_consentite",
		"decisioni_invariate", "revisione_confrontata_separatamente"}
	// chiaviDellaVoceInAtteso (T-B6c-01): le chiavi della voce che gli attesi scrivono dentro la mappa «atteso» delle
	// letture, per sezione. Non sono letture del file: si leggono come le chiavi della voce (gli invarianti, l'esito con il
	// target), con lo stesso significato. La stessa chiave nella voce e in atteso è un errore: un valore solo.
	chiaviDellaVoceInAtteso = map[string][]string{
		confronto.SezioneBaseline: {"componente_id_proposta_vuoto_preservato", "decisioni_invariate", "revisione_confrontata_separatamente"},
		confronto.SezioneReali:    {"target_presente", "esito_con_target"},
	}
	// sinonimiDelleLetture: le chiavi delle letture dei file che il piano dà come lo stesso concetto di una chiave della
	// tabella dei casi (6.4.9, «Traduzione degli attesi»; C-30: token_conservato e token_00_conservato). Valgono solo
	// nelle sezioni di A1c: la tabella dei casi di A1a e le modalità regole e casi non cambiano (F0-01; T-B6c-13). Il
	// percorso resta quello scritto negli attesi.
	sinonimiDelleLetture = map[string]string{"token_00_conservato": "token_conservato"}
)

// chiaveDellaRegola: la chiave della tabella dei casi che giudica una chiave delle letture di un file (lei stessa, o
// quella di cui è sinonimo).
func chiaveDellaRegola(k string) string {
	if s, ok := sinonimiDelleLetture[k]; ok {
		return s
	}
	return k
}

// lettoreSezioni: lo stato della lettura delle sezioni.
type lettoreSezioni struct {
	out SezioniAttesi
}

// indiciDegliElenchi: gli indici di un percorso, scritti «[]» per una chiave non tradotta, che conta una volta sola
// e non dice quale voce.
var indiciDegliElenchi = regexp.MustCompile(`\[[0-9]+\]`)

func (l *lettoreSezioni) nonTradotta(p string) {
	l.out.NonTradotte = append(l.out.NonTradotte, indiciDegliElenchi.ReplaceAllString(p, "[]"))
}

func (l *lettoreSezioni) errore(p, motivo string) {
	l.out.Errori = append(l.out.Errori, p+": "+motivo)
}

// risolvi: un alias si segue.
func risolvi(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias
	}
	return n
}

// senzaValore: il nodo manca o vale null.
func senzaValore(n *yaml.Node) bool {
	n = risolvi(n)
	return n == nil || (n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null")
}

// mappa: le coppie di una mappa, con le chiavi note; le altre vanno in NonTradotte. Una mappa vuota o null dà una
// mappa vuota; un valore che non è una mappa è un errore.
func (l *lettoreSezioni) mappa(n *yaml.Node, p string, note []string) map[string]*yaml.Node {
	out := map[string]*yaml.Node{}
	if senzaValore(n) {
		return out
	}
	n = risolvi(n)
	if n.Kind != yaml.MappingNode {
		l.errore(p, "serve una mappa")
		return out
	}
	viste := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		pk := p + "." + k
		if viste[k] {
			l.errore(pk, "chiave ripetuta")
			continue
		}
		viste[k] = true
		if !dentro(k, note) {
			l.nonTradotta(pk)
			continue
		}
		out[k] = n.Content[i+1]
	}
	return out
}

// elencoDi: gli elementi di un elenco; null o assente: nessuno.
func (l *lettoreSezioni) elencoDi(n *yaml.Node, p string) []*yaml.Node {
	if senzaValore(n) {
		return nil
	}
	n = risolvi(n)
	if n.Kind != yaml.SequenceNode {
		l.errore(p, "serve un elenco")
		return nil
	}
	return n.Content
}

// testoDi: il testo di un valore semplice, com'è scritto; null o assente: "".
func (l *lettoreSezioni) testoDi(n *yaml.Node, p string) string {
	if senzaValore(n) {
		return ""
	}
	n = risolvi(n)
	if n.Kind != yaml.ScalarNode {
		l.errore(p, "serve un valore semplice")
		return ""
	}
	return n.Value
}

func (l *lettoreSezioni) booleanoDi(n *yaml.Node, p string) *bool {
	if senzaValore(n) {
		return nil
	}
	n = risolvi(n)
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!bool" {
		l.errore(p, "serve un booleano")
		return nil
	}
	v := strings.EqualFold(n.Value, "true")
	return &v
}

func (l *lettoreSezioni) interoDi(n *yaml.Node, p string) *int {
	if senzaValore(n) {
		return nil
	}
	n = risolvi(n)
	v, err := strconv.Atoi(n.Value)
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!int" || err != nil {
		l.errore(p, "serve un intero")
		return nil
	}
	return &v
}

func (l *lettoreSezioni) uuidDi(n *yaml.Node, p string) *uuid.UUID {
	s := l.testoDi(n, p)
	if s == "" {
		return nil
	}
	u, err := uuid.Parse(s)
	if err != nil {
		l.errore(p, "l'UUID non si legge")
		return nil
	}
	return &u
}

// testiDi: un elenco di valori semplici, com'è scritto.
func (l *lettoreSezioni) testiDi(n *yaml.Node, p string) []string {
	var out []string
	for i, e := range l.elencoDi(n, p) {
		out = append(out, l.testoDi(e, fmt.Sprintf("%s[%d]", p, i)))
	}
	return out
}

// scenario: la sezione dello scenario della richiesta inoltrata (scenario_inoltro, M-21): gli ingressi del caso che il
// controllo n.1 confronta con il file dei casi, i prodotti attesi, i conteggi dichiarati e i file.
//
// Da B6c: fonte_quantita della sezione è quella dei prodotti attesi che non la scrivono (T-B6c-05: un prodotto che ne
// scrive una diversa è un errore); origine_atteso dice da dove viene l'atteso, e si riporta, fra le chiavi libere: non è
// un dato da confrontare (T-B6c-06).
func (l *lettoreSezioni) scenario(n *yaml.Node, p string) {
	m := l.mappa(n, p, []string{"thread_id", "messaggio_id", "autorita_target", "prodotti_confermati_db",
		"prodotti_attesi", "conteggi", "file", "nota", "fonte_quantita", "origine_atteso"})
	if len(m) == 0 {
		return
	}
	l.nota(m, p)
	l.descrittive(m, p, "origine_atteso")
	s := &ScenarioAtteso{
		ThreadID:             l.uuidDi(m["thread_id"], p+".thread_id"),
		MessaggioID:          l.uuidDi(m["messaggio_id"], p+".messaggio_id"),
		AutoritaTarget:       l.testoDi(m["autorita_target"], p+".autorita_target"),
		ProdottiConfermatiDB: l.booleanoDi(m["prodotti_confermati_db"], p+".prodotti_confermati_db"),
		FonteQuantita:        l.testoDi(m["fonte_quantita"], p+".fonte_quantita"),
	}
	for i, e := range l.elencoDi(m["prodotti_attesi"], p+".prodotti_attesi") {
		pe := fmt.Sprintf("%s.prodotti_attesi[%d]", p, i)
		x := l.mappa(e, pe, []string{"originale", "codice_richiesto", "base", "fase", "quantita_richiesta",
			"fonte_quantita", "nota"})
		l.nota(x, pe)
		pr := ProdottoScenario{
			Percorso:      pe,
			Originale:     l.testoDi(x["originale"], pe+".originale"),
			Base:          l.testoDi(x["base"], pe+".base"),
			Fase:          l.testoDi(x["fase"], pe+".fase"),
			Quantita:      l.interoDi(x["quantita_richiesta"], pe+".quantita_richiesta"),
			FonteQuantita: l.testoDi(x["fonte_quantita"], pe+".fonte_quantita"),
		}
		if c := l.testoDi(x["codice_richiesto"], pe+".codice_richiesto"); c != "" {
			if pr.Originale != "" && pr.Originale != c {
				l.errore(pe, "originale e codice_richiesto diversi")
			}
			pr.Originale = c
		}
		switch {
		case pr.FonteQuantita == "":
			pr.FonteQuantita = s.FonteQuantita
		case s.FonteQuantita != "" && pr.FonteQuantita != s.FonteQuantita:
			l.errore(pe+".fonte_quantita", "diversa da fonte_quantita dello scenario")
		}
		s.Prodotti = append(s.Prodotti, pr)
	}
	// I conteggi sono una mappa a chiavi libere (nome → intero): quali nomi il runner sa contare lo dice
	// controllaConteggi, e un nome che non sa contare è una chiave non tradotta.
	if c := m["conteggi"]; !senzaValore(c) {
		c = risolvi(c)
		if c.Kind != yaml.MappingNode {
			l.errore(p+".conteggi", "serve una mappa")
		} else {
			for i := 0; i+1 < len(c.Content); i += 2 {
				nome := c.Content[i].Value
				if v := l.interoDi(c.Content[i+1], p+".conteggi."+nome); v != nil {
					s.Conteggi = append(s.Conteggi, ConteggioAtteso{Nome: nome, Valore: *v})
				}
			}
			sort.SliceStable(s.Conteggi, func(i, j int) bool { return s.Conteggi[i].Nome < s.Conteggi[j].Nome })
		}
	}
	s.File = l.voci(m["file"], p+".file", confronto.SezioneScenario, chiaviScenario)
	l.out.Scenario = s
}

// daRivedere: la sezione delle decisioni successive da rivedere (la C5): i file confermati, ognuno da_rivedere, e la
// conferma dell'albero, di cui si raccolgono gli ID.
//
// Da B6c le chiavi della sezione che la prima corsa ha detto non tradotte:
//   - thread_id: il thread della sezione, che il n.2 risolve nella fotografia; cliente, se è un UUID: il cliente di quel
//     thread (n.2). Un cliente scritto con il nome si riporta, fra le chiavi libere (T-B6c-08);
//   - anomalie: si riportano nel rapporto (6.4.9, la C5 nel runner: «le anomalie della sezione compaiono solo nel
//     rapporto»);
//   - deciso_da: chi ha deciso la C5 della sezione, con la sigla (o l'UUID) di un utente; il n.2 lo cerca fra gli utenti
//     della fotografia (C-05, T-B6c-15; il piano 6.5: ListSigleUtenti serve proprio al deciso_da degli attesi C5);
//   - decisione, fonte, intervallo, regole, stato: descrivono la decisione C5 della sezione (quando, da dove, con quali
//     regole); le righe decise stanno nelle voci, verificate una per una. Si riportano, e sono fra le chiavi libere non
//     verificate (T-B6c-08).
func (l *lettoreSezioni) daRivedere(n *yaml.Node, p string) {
	m := l.mappa(n, p, []string{"file_confermati", "conferma_albero", "nota", "thread_id", "cliente", "anomalie",
		"decisione", "deciso_da", "fonte", "intervallo", "regole", "stato"})
	l.nota(m, p)
	// Una chiave scritta non si butta mai: thread_id scritto (anche vuoto) va al n.2, che lo risolve o dà la differenza;
	// cliente con un UUID va al n.2, altrimenti (un nome, un valore vuoto) è descrittiva.
	if x, ok := m["thread_id"]; ok {
		l.out.ThreadDaRivedere = &IDAtteso{Percorso: p + ".thread_id", Chiave: "thread_id", Valore: l.testoDi(x, p+".thread_id")}
	}
	if x, ok := m["cliente"]; ok {
		if s := l.testoDi(x, p+".cliente"); uuid.Validate(s) == nil {
			l.out.ClienteDaRivedere = &IDAtteso{Percorso: p + ".cliente", Chiave: "cliente_id", Valore: s}
		} else {
			l.descrittive(m, p, "cliente")
		}
	}
	if a := m["anomalie"]; a != nil {
		l.out.Riportate = append(l.out.Riportate, riporta(a, p+".anomalie")...)
	}
	// deciso_da scritto (anche vuoto) va al n.2, come thread_id: lo verifica o dice perché non si verifica (C-05).
	if x, ok := m["deciso_da"]; ok {
		l.out.DecisoDaRivedere = &IDAtteso{Percorso: p + ".deciso_da", Chiave: "deciso_da", Valore: l.testoDi(x, p+".deciso_da")}
	}
	l.descrittive(m, p, "decisione", "fonte", "intervallo", "regole", "stato")
	l.out.DaRivedere = l.voci(m["file_confermati"], p+".file_confermati", confronto.SezioneDaRivedere, chiaviDaRivedere)
	if a := m["conferma_albero"]; a != nil {
		l.out.Albero = append(l.out.Albero, raccogliID(a, p+".conferma_albero")...)
		l.out.RigheAlbero = append(l.out.RigheAlbero, righeAlbero(a, p+".conferma_albero")...)
		l.out.ChiaviLibere = append(l.out.ChiaviLibere, chiaviLibere(a, p+".conferma_albero")...)
	}
}

// nota: la nota di una mappa già letta, fra le righe riportate nel rapporto (una nota non si verifica: si riporta).
func (l *lettoreSezioni) nota(m map[string]*yaml.Node, p string) {
	if s := l.testoDi(m["nota"], p+".nota"); s != "" {
		l.out.Riportate = append(l.out.Riportate, RigaRiportata{Percorso: p + ".nota", Valore: s})
	}
}

// descrittive (T-B6c-06, T-B6c-08): le chiavi di una mappa già letta che descrivono la sezione e non sono dati da
// confrontare. Il valore si riporta nel rapporto, riga per riga; il percorso va fra le chiavi libere non verificate, un
// elenco informativo: accettate e dichiarate, mai buttate (la regola delle chiavi accettate).
func (l *lettoreSezioni) descrittive(m map[string]*yaml.Node, p string, chiavi ...string) {
	for _, k := range chiavi {
		x, ok := m[k]
		if !ok {
			continue
		}
		l.out.Riportate = append(l.out.Riportate, riporta(x, p+"."+k)...)
		l.out.ChiaviLibere = append(l.out.ChiaviLibere, indiciDegliElenchi.ReplaceAllString(p+"."+k, "[]"))
	}
}

// voci: le voci di file di una sezione, con le chiavi note di quella sezione.
func (l *lettoreSezioni) voci(n *yaml.Node, p, sezione string, note []string) []VoceAttesa {
	var out []VoceAttesa
	for i, e := range l.elencoDi(n, p) {
		pe := fmt.Sprintf("%s[%d]", p, i)
		m := l.mappa(e, pe, note)
		v := VoceAttesa{
			Sezione:     sezione,
			Percorso:    pe,
			ID:          l.testoDi(m["id"], pe+".id"),
			AllegatoID:  l.uuidDi(m["allegato_id"], pe+".allegato_id"),
			Sha256:      strings.ToLower(l.testoDi(m["sha256"], pe+".sha256")),
			NomeFile:    l.testoDi(m["nome_file"], pe+".nome_file"),
			DipendeDa:   l.testoDi(m["dipende_da"], pe+".dipende_da"),
			StatoAtteso: l.testoDi(m["stato_atteso"], pe+".stato_atteso"),
			AttesoStato: l.testoDi(m["atteso_stato"], pe+".atteso_stato"),
			Autorita:    l.testoDi(m["autorita"], pe+".autorita"),
			ThreadID:    l.uuidDi(m["thread_id"], pe+".thread_id"),
		}
		// percorso: dove sta una chiave della voce negli attesi (nella voce, o dentro atteso: T-B6c-01).
		percorso := map[string]string{}
		if x, ok := m["atteso"]; ok {
			switch {
			case sezione == confronto.SezioneScenario:
				v.Atteso = l.testoDi(x, pe+".atteso")
			case sezione == confronto.SezioneDaRivedere && !senzaValore(x) && risolvi(x).Kind == yaml.ScalarNode:
				// L'atteso della C5 scritto come valore: l'esito del file, che la traduzione controlla (T-B6c-07).
				v.Atteso = l.testoDi(x, pe+".atteso")
			default:
				var dellaVoce map[string]*yaml.Node
				v.Letture, dellaVoce = l.lettureAttese(x, pe+".atteso", chiaviDellaVoceInAtteso[sezione])
				for _, k := range chiaviDellaVoceInAtteso[sezione] {
					y, ok := dellaVoce[k]
					if !ok {
						continue
					}
					if _, doppia := m[k]; doppia {
						l.errore(pe+"."+k, "chiave ripetuta, nella voce e in atteso")
						continue
					}
					m[k], percorso[k] = y, pe+".atteso."+k
				}
			}
		}
		dove := func(k string) string {
			if q, ok := percorso[k]; ok {
				return q
			}
			return pe + "." + k
		}
		if x, ok := m["atteso_target_base"]; ok {
			v.TargetBaseScritto = true
			v.TargetBase = l.testoDi(x, pe+".atteso_target_base")
		}
		if x, ok := m["radici"]; ok {
			v.RadiciScritte = true
			v.Radici = l.testiDi(x, pe+".radici")
		}
		// esito_con_target null (o vuoto) vuol dire «non ancora scritto dall'utente» (R21 c A; T-B6c-03); scritto, si
		// riporta accanto all'esito del file.
		if x, ok := m["esito_con_target"]; ok && !senzaValore(x) {
			v.EsitoConTargetScritto = true
			v.EsitoConTarget = strings.Join(riportaTesti(x), "; ")
			v.Riportate = append(v.Riportate, "esito_con_target = "+v.EsitoConTarget)
		}
		if x, ok := m["decisione_proposta"]; ok {
			v.Proposta = l.decisione(x, pe+".decisione_proposta")
		}
		if x, ok := m["decisione_documento"]; ok {
			v.Documento = l.decisione(x, pe+".decisione_documento")
		}
		if x, ok := m["nell_export_proposte"]; ok {
			v.Export = l.decisione(x, pe+".nell_export_proposte")
		}
		for _, k := range invariantiBooleani {
			if x, ok := m[k]; ok {
				if b := l.booleanoDi(x, dove(k)); b != nil {
					v.Invarianti = append(v.Invarianti, InvarianteAttesa{Nome: k, Valore: *b})
				}
			}
		}
		for _, k := range []string{"nota", "etichetta"} {
			if s := l.testoDi(m[k], pe+"."+k); s != "" {
				v.Riportate = append(v.Riportate, k+" = "+s)
			}
		}
		for _, a := range l.testiDi(m["anomalie"], pe+".anomalie") {
			v.Riportate = append(v.Riportate, "anomalia = "+a)
		}
		// codice dei file dello scenario (T-B6c-04): il piano non lo fa confrontare (6.4.9, la tabella delle sezioni:
		// collocazione, associazione, autorità, target, radici). Si riporta accanto all'esito del file, ed è fra le
		// chiavi libere non verificate.
		if x, ok := m["codice"]; ok {
			if s := l.testoDi(x, pe+".codice"); s != "" {
				v.Riportate = append(v.Riportate, "codice = "+s)
			}
			l.out.ChiaviLibere = append(l.out.ChiaviLibere, indiciDegliElenchi.ReplaceAllString(pe+".codice", "[]"))
		}
		out = append(out, v)
	}
	return out
}

// lettureAttese: la mappa «atteso» delle letture di un file (baseline, reali), con le chiavi della tabella dei casi
// (traduzione.go) e i loro sinonimi delle sezioni di A1c (sinonimiDelleLetture): una chiave che la tabella non conosce
// è una chiave non tradotta. Le chiavi della voce scritte dentro atteso (dellaVoce, T-B6c-01) non sono letture: si
// restituiscono a parte, e le legge la voce.
func (l *lettoreSezioni) lettureAttese(n *yaml.Node, p string, chiaviDellaVoce []string) ([]ChiaveAttesa, map[string]*yaml.Node) {
	if senzaValore(n) {
		return nil, nil
	}
	n = risolvi(n)
	if n.Kind != yaml.MappingNode {
		l.errore(p, "serve una mappa")
		return nil, nil
	}
	var out []ChiaveAttesa
	dellaVoce := map[string]*yaml.Node{}
	viste := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		switch {
		case viste[k]:
			l.errore(p+"."+k, "chiave ripetuta")
		case dentro(k, chiaviDellaVoce):
			dellaVoce[k] = n.Content[i+1]
		case !ChiaveNota(chiaveDellaRegola(k)):
			l.nonTradotta(p + "." + k)
		default:
			out = append(out, ChiaveAttesa{Chiave: k, Valore: valoreDi(n.Content[i+1])})
		}
		viste[k] = true
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Chiave < out[j].Chiave })
	return out, dellaVoce
}

// decisione: una riga di proposta o di documento attesa, campo per campo, com'è scritta.
func (l *lettoreSezioni) decisione(n *yaml.Node, p string) []CampoAtteso {
	m := l.mappa(n, p, chiaviDecisione)
	out := []CampoAtteso{}
	for _, k := range chiaviDecisione {
		x, ok := m[k]
		if !ok {
			continue
		}
		c := CampoAtteso{Nome: k}
		if senzaValore(x) {
			c.Nullo = true
		} else {
			c.Valore = l.testoDi(x, p+"."+k)
		}
		out = append(out, c)
	}
	return out
}

// Le chiavi degli ID che il controllo n.2 risolve nella fotografia, nelle sezioni lette senza interpretarle.
var chiaviID = []string{"allegato_id", "thread_id", "messaggio_id", "componente_id", "padre_id", "figlio_id",
	"documento_id", "sha256"}

// raccogliID: gli ID e gli sha256 di una sezione a forma libera, con il percorso: una chiave di chiaviID con un valore
// semplice o un elenco di valori semplici.
func raccogliID(n *yaml.Node, p string) []IDAtteso {
	var out []IDAtteso
	var visita func(n *yaml.Node, p string)
	visita = func(n *yaml.Node, p string) {
		n = risolvi(n)
		if n == nil {
			return
		}
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i].Value, risolvi(n.Content[i+1])
				pk := p + "." + k
				if dentro(k, chiaviID) && v != nil {
					switch {
					case v.Kind == yaml.ScalarNode && v.ShortTag() != "!!null":
						out = append(out, IDAtteso{Percorso: pk, Chiave: k, Valore: v.Value})
						continue
					case v.Kind == yaml.SequenceNode:
						for j, e := range v.Content {
							if e = risolvi(e); e != nil && e.Kind == yaml.ScalarNode && e.ShortTag() != "!!null" {
								out = append(out, IDAtteso{Percorso: fmt.Sprintf("%s[%d]", pk, j), Chiave: k, Valore: e.Value})
							}
						}
						continue
					}
				}
				visita(v, pk)
			}
		case yaml.SequenceNode:
			for i, e := range n.Content {
				visita(e, fmt.Sprintf("%s[%d]", p, i))
			}
		}
	}
	visita(n, p)
	return out
}

// chiaviLibere (R-109): in una sezione a forma libera, le chiavi con un valore semplice che non sono ID né sha256 (e non
// sono id o nota, che non dicono niente da verificare), con il percorso e gli indici scritti «[]». Il runner non le
// verifica: le scrive il rapporto, in un elenco informativo.
func chiaviLibere(n *yaml.Node, p string) []string {
	var out []string
	var visita func(n *yaml.Node, p string)
	visita = func(n *yaml.Node, p string) {
		n = risolvi(n)
		if n == nil {
			return
		}
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k, v := n.Content[i].Value, risolvi(n.Content[i+1])
				pk := p + "." + k
				switch {
				case v == nil:
				case dentro(k, chiaviID) && (v.Kind == yaml.ScalarNode || v.Kind == yaml.SequenceNode):
				case v.Kind == yaml.MappingNode || v.Kind == yaml.SequenceNode:
					visita(v, pk)
				case k == "id" || k == "nota":
				default:
					out = append(out, indiciDegliElenchi.ReplaceAllString(pk, "[]"))
				}
			}
		case yaml.SequenceNode:
			for i, e := range n.Content {
				visita(e, fmt.Sprintf("%s[%d]", p, i))
			}
		}
	}
	visita(n, p)
	return out
}

// righeAlbero (R-109): le righe della conferma dell'albero, una per elemento di ogni elenco (e una per ogni mappa) al
// primo livello della sezione: il runner ne risolve gli ID, ma non verifica che siano invariate nella fotografia.
func righeAlbero(n *yaml.Node, p string) []string {
	n = risolvi(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, risolvi(n.Content[i+1])
		switch {
		case v == nil:
		case v.Kind == yaml.SequenceNode:
			for j := range v.Content {
				out = append(out, fmt.Sprintf("%s.%s[%d]", p, k, j))
			}
		case v.Kind == yaml.MappingNode:
			out = append(out, p+"."+k)
		}
	}
	return out
}

// riporta: una sezione descrittiva come righe «percorso = valore», nell'ordine del file.
func riporta(n *yaml.Node, p string) []RigaRiportata {
	var out []RigaRiportata
	var visita func(n *yaml.Node, p string)
	visita = func(n *yaml.Node, p string) {
		n = risolvi(n)
		if n == nil {
			return
		}
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				visita(n.Content[i+1], p+"."+n.Content[i].Value)
			}
		case yaml.SequenceNode:
			for i, e := range n.Content {
				visita(e, fmt.Sprintf("%s[%d]", p, i))
			}
		case yaml.ScalarNode:
			out = append(out, RigaRiportata{Percorso: p, Valore: n.Value})
		}
	}
	visita(n, p)
	return out
}

// riportaTesti: un valore qualunque come righe «percorso = valore» (o il solo valore per un valore semplice), per una
// chiave che il runner porta nel rapporto senza interpretarla.
func riportaTesti(n *yaml.Node) []string {
	var out []string
	for _, r := range riporta(n, "") {
		if r.Percorso == "" {
			out = append(out, r.Valore)
		} else {
			out = append(out, strings.TrimPrefix(r.Percorso, ".")+" = "+r.Valore)
		}
	}
	return out
}
