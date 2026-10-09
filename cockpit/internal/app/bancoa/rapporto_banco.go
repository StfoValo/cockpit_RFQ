package bancoa

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/estrazione/evidenze"
	"promatec/cockpit/internal/core/fotorfq"
	"promatec/cockpit/internal/core/registro/regole/grammatica"
	valut "promatec/cockpit/internal/core/valutazione"
	"promatec/cockpit/internal/platform/dataset"
	"promatec/cockpit/internal/platform/jsoncanonico"
)

// rapporto_banco.go: il rapporto delle modalità dsn ed exports (piano 6.4.9, «Il rapporto»; A1c.md §5.5; contratto §4,
// riga 6.4.9; T-B6-03, F0-01 d). Ha una versione sua, 3: il rapporto di A1a (Rapporto, versione 2) e ScriviRapporto
// non cambiano. Si scrive in JSON canonico (rapporto-<modalità>.json) e in un riepilogo di testo
// (rapporto-<modalità>.txt), in modo atomico, nella cartella dei rapporti, fuori dal modulo. Contiene dati privati (ID,
// UUID, percorsi del dataset, codici): non si incolla in commit, PR o note pubbliche. Il riepilogo, che va anche a
// video, ha solo conteggi, ID e percorsi, mai testi di mail.

// VersioneRapportoBanco: la versione della forma del rapporto dei modi dsn ed exports. Cambiarla vuol dire riscrivere
// la prova che la fissa.
const VersioneRapportoBanco = 3

// RapportoBanco: l'uscita di un'esecuzione delle modalità dsn ed exports, con l'esito in testa (R44).
//   - Controlli: tutti, eseguiti o no, con il motivo (manifest, voci, sola lettura, i controlli del runner 1–5, la
//     traduzione degli attesi, la valutazione, il confronto, i casi di contratto, il gate con -gate). Dettagli: per i
//     controlli del banco, le differenze e le parti non verificate una per una, con il percorso (la regola delle chiavi
//     accettate: una chiave non verificata si dichiara, e qui si vede quale).
//   - Thread: per thread il vecchio e il nuovo di ogni file, il badge, l'indicatore di revisione e, con gli attesi, gli
//     esiti; i thread non valutati e i file non valutati sono a parte (D5).
//   - Censimento, SenzaCaso, CorrezioniManuali, ProfiloLimiti: le sezioni del 6.4.9.
//   - Prodotti: solo informazione (contratto §4, riga 6.4.9; T-B0-16): i sette assi, lo stato, il fascicolo, i
//     conflitti; mai PASS/FAIL; con -exports «non calcolata» dove la sezione della fotografia manca (T-12).
//   - Attesi, Casi, Gate: solo con -attesi.
//   - Ogni controllo e ogni voce del gate ha la classe e l'esito tri-stato; Chiusura distingue la conclusione della
//     parte obbligatoria del runner dall'incompletezza del rapporto (R116 B, precisata dall'utente il 07/10;
//     classi.go). Non cambia l'uscita.
type RapportoBanco struct {
	VersioneRapporto  int                  `json:"versione_rapporto"`
	Modalita          string               `json:"modalita"`
	Esito             Esito                `json:"esito"`
	Motivo            string               `json:"motivo,omitempty"`
	Differenze        int                  `json:"differenze"`
	Sorgente          string               `json:"sorgente,omitempty"`
	Manifest          string               `json:"manifest"`
	Sha256Manifest    string               `json:"sha256_manifest,omitempty"`
	Sha256Attesi      string               `json:"sha256_attesi,omitempty"`
	Sha256Indice      string               `json:"sha256_indice,omitempty"`
	Sha256Casi        string               `json:"sha256_casi,omitempty"`
	ImprontaIndice    string               `json:"impronta_indice,omitempty"`
	VersioneLimiti    string               `json:"versione_limiti,omitempty"`
	VersioneAttesi    int                  `json:"versione_attesi,omitempty"`
	Versioni          VersioniBanco        `json:"versioni"`
	Controlli         []Controllo          `json:"controlli"`
	Dettagli          []DettaglioControllo `json:"dettagli_controlli,omitempty"`
	SolaLettura       *SolaLettura         `json:"sola_lettura,omitempty"`
	Fotografia        *SintesiFotografia   `json:"fotografia,omitempty"`
	Valutazione       *SintesiValutazione  `json:"valutazione,omitempty"`
	Thread            []ThreadBanco        `json:"thread,omitempty"`
	Censimento        []VoceCensimento     `json:"censimento,omitempty"`
	SenzaCaso         []ThreadSenzaCaso    `json:"senza_caso,omitempty"`
	CorrezioniManuali []CorrezioniCliente  `json:"correzioni_manuali,omitempty"`
	ProfiloLimiti     *ProfiloLimiti       `json:"profilo_limiti,omitempty"`
	Prodotti          *SezioneProdotti     `json:"prodotti,omitempty"`
	// RigheConfermaAlbero: le righe della «Conferma l'albero» della C5, solo informazione (D-R116 I, decisa dall'utente
	// l'08/10): fuori dalla parte obbligatoria degli invarianti della C5, con il conteggio e il motivo.
	RigheConfermaAlbero *RigheInformative `json:"righe_conferma_albero,omitempty"`
	Attesi              *SintesiAttesi    `json:"attesi,omitempty"`
	Casi                *RapportoCasi     `json:"casi,omitempty"`
	Gate                *Gate             `json:"gate,omitempty"`
	Chiusura            *Chiusura         `json:"chiusura,omitempty"`
	Scritture           string            `json:"scritture"`
}

// DettaglioControllo: le differenze e le parti non verificate di un controllo del banco, una per riga, con il percorso
// negli attesi o l'ID nella fotografia. Il Controllo ne porta solo il numero (è il tipo di A1a, che non cambia).
//
// NonApplicabili: le parti che il piano assegna all'altra modalità, e che questa corsa non verifica né dichiara non
// verificate (R-117): solo informazione, «non applicabile in questa corsa».
//
// AmbitiDedotti: le chiavi giudicate su un ambito dedotto dalle chiavi sorelle della voce, con l'unità (le chiavi dei
// token: T-B6-220, precisata con R-140): solo informazione, dice su quale unità la chiave è stata giudicata.
//
// FuoriPerimetro: le parti che una decisione dell'utente ha messo fuori dal perimetro approvato (D-R116 F, decisa
// dall'utente l'08/10: con gli export, il n.1 senza i finiti), con il percorso; la loro riga nella tabella delle classi
// ha la motivazione e la fonte, e la chiusura le elenca fra i fuori perimetro. Non cambiano lo stato del controllo.
type DettaglioControllo struct {
	Nome           string   `json:"nome"`
	Differenze     []string `json:"differenze,omitempty"`
	NonVerificate  []string `json:"non_verificate,omitempty"`
	NonApplicabili []string `json:"non_applicabili,omitempty"`
	AmbitiDedotti  []string `json:"ambiti_dedotti,omitempty"`
	FuoriPerimetro []string `json:"fuori_perimetro,omitempty"`
}

// RigheInformative: una sezione informativa del rapporto fatta di righe degli attesi che il runner non verifica, con il
// loro numero, il motivo e i percorsi (senza valori).
type RigheInformative struct {
	Righe    int      `json:"righe"`
	Motivo   string   `json:"motivo"`
	Percorsi []string `json:"percorsi,omitempty"`
}

// VersioniBanco: le versioni del codice del rapporto (quelle di A1a, più quelle del motore di A1c che entrano
// nell'impronta dell'esito di valutazione e di confronto; RevisioneRegistrata è la lettura della colonna rev del vecchio
// motore, motorea.LeggiRevisioneRegistrata, R113 B ratificata).
type VersioniBanco struct {
	Rapporto            int    `json:"rapporto"`
	Manifest            int    `json:"manifest"`
	Canonicalizzazione  string `json:"canonicalizzazione"`
	SchemaGrammatiche   int    `json:"schema_grammatiche"`
	Indice              int    `json:"indice"`
	Capacita            string `json:"capacita"`
	Algoritmo           string `json:"algoritmo"`
	SchemaFotografia    int    `json:"schema_fotografia"`
	Casi                int    `json:"casi"`
	Valutazione         string `json:"valutazione"`
	ImprontaProdotto    int    `json:"impronta_prodotto"`
	Formati2D           int    `json:"formati_2d"`
	Composizione        string `json:"composizione"`
	RevisioneRegistrata string `json:"revisione_registrata"`
	Confronto           string `json:"confronto"`
}

// SolaLettura: che cosa può fare il collegamento al DB (migrazioni.ControllaSolaLettura) e come la transazione del
// caricatore si è dichiarata. Con -exports non c'è: nessun database si apre.
type SolaLettura struct {
	Ruolo                  string   `json:"ruolo"`
	Database               string   `json:"database"`
	ScritturaPossibile     bool     `json:"scrittura_possibile"`
	Escluse                []string `json:"escluse,omitempty"`
	EscluseNonLeggibili    *int     `json:"escluse_non_leggibili"` // nil: non controllate (R-107)
	TransazioneSolaLettura string   `json:"transazione_sola_lettura,omitempty"`
	Isolamento             string   `json:"isolamento,omitempty"`
}

// SintesiFotografia: la fotografia senza i dati: l'origine, la coerenza, lo schema, la terna, lo stato delle sezioni,
// l'impronta e le diagnostiche del caricatore o del lettore degli export (D-V1-2: non entrano nell'esito, qui sì).
type SintesiFotografia struct {
	Origine      string                          `json:"origine"`
	Coerente     bool                            `json:"coerente"`
	SchemaDB     int                             `json:"schema_db"`
	Analizzatore *fotorfq.Terna                  `json:"analizzatore,omitempty"`
	Sezioni      map[string]fotorfq.StatoSezione `json:"sezioni,omitempty"`
	Impronta     string                          `json:"impronta"`
	Thread       int                             `json:"thread"`
	FuoriRFQ     int                             `json:"fuori_rfq"`
	Diagnostiche []evidenze.Diagnostica          `json:"diagnostiche,omitempty"`
	Limiti       []string                        `json:"limiti,omitempty"`
}

// SintesiValutazione: l'esito di valutazione.Calcola senza i dati dei thread: l'impronta, i thread valutati e no, le
// diagnostiche fuori dai thread, e il conto per codice di tutte le diagnostiche dell'esito (le tre sedi: l'esito, i
// thread, gli ancoraggi; T-B6-28).
type SintesiValutazione struct {
	Impronta          string                 `json:"impronta"`
	ThreadValutati    int                    `json:"thread_valutati"`
	ThreadNonValutati int                    `json:"thread_non_valutati"`
	Diagnostiche      []evidenze.Diagnostica `json:"diagnostiche,omitempty"`
	PerCodice         []ConteggioCodice      `json:"per_codice,omitempty"`
}

// ConteggioCodice: quante diagnostiche hanno un codice, con la gravità.
type ConteggioCodice struct {
	Codice  string `json:"codice"`
	Gravita string `json:"gravita"`
	N       int    `json:"n"`
}

// ThreadBanco: un thread nel rapporto.
//   - AParte: il thread non è valutato (D5): i suoi file stanno fuori dal gate e dalle correzioni manuali per cliente.
//   - File: una riga per allegato, con il nome, la natura e lo sha256 della fotografia.
//   - Conteggi, Correzioni, ImprontaConfronto: quelli di confronto.Esito per il thread.
//   - ControAtteso, ProdottiControAtteso: con gli attesi.
//   - Le diagnostiche nelle loro sedi: quelle di valutazione per il thread, quelle di ancoraggio, quelle di confronto.
type ThreadBanco struct {
	ThreadID              uuid.UUID                       `json:"thread_id"`
	ClienteID             uuid.UUID                       `json:"cliente_id"`
	Valutato              bool                            `json:"valutato"`
	Motivo                string                          `json:"motivo,omitempty"`
	HashSnapshot          string                          `json:"hash_snapshot,omitempty"`
	Caso                  string                          `json:"caso,omitempty"`
	AParte                bool                            `json:"a_parte"`
	VecchiProdotti        []string                        `json:"vecchi_prodotti,omitempty"`
	ProdottiNuovi         []confronto.ProdottoNuovo       `json:"prodotti_nuovi,omitempty"`
	File                  []FileBanco                     `json:"file,omitempty"`
	Conteggi              map[confronto.Badge]int         `json:"conteggi"`
	Correzioni            confronto.CorrezioniManuali     `json:"correzioni"`
	ImprontaConfronto     string                          `json:"impronta_confronto"`
	ProdottiControAtteso  []confronto.EsitoProdottoAtteso `json:"prodotti_contro_atteso,omitempty"`
	Diagnostiche          []evidenze.Diagnostica          `json:"diagnostiche,omitempty"`
	DiagnosticheAncoraggi []evidenze.Diagnostica          `json:"diagnostiche_ancoraggi,omitempty"`
	DiagnosticheConfronto []evidenze.Diagnostica          `json:"diagnostiche_confronto,omitempty"`
}

// FileBanco: un allegato del thread nel rapporto: com'è nella fotografia, la riga di confronto (vecchio, nuovo, badge,
// motivo, indicatore di revisione) e gli esiti contro gli attesi.
type FileBanco struct {
	AllegatoID uuid.UUID           `json:"allegato_id"`
	NomeFile   string              `json:"nome_file"`
	Natura     string              `json:"natura,omitempty"`
	Sha256     string              `json:"sha256,omitempty"`
	Valutato   bool                `json:"valutato"`
	Riga       confronto.EsitoFile `json:"riga"`
	Esiti      []EsitoContro       `json:"esiti,omitempty"`
}

// EsitoContro: l'esito di un file contro una voce degli attesi. Motivi è il motivo diviso sulle virgole (l'esito
// ambiguo può portarne due: T-B6-53, R-49). Nel riepilogo «mancante» si scrive «senza risposta» (R30 c).
//   - ID: l'id della voce negli attesi, accanto al percorso (R-119);
//   - Riportate: la nota, l'etichetta e le anomalie della voce, com'erano negli attesi (solo il rapporto le mostra);
//   - NonVerificabile: perché la corsa non può verificare l'esito (T-B6-104): allora l'esito non conta nel gate.
type EsitoContro struct {
	Sezione         string                `json:"sezione,omitempty"`
	Percorso        string                `json:"percorso,omitempty"`
	ID              string                `json:"id,omitempty"`
	Esito           confronto.EsitoAtteso `json:"esito"`
	Peso            string                `json:"peso"`
	Motivi          []string              `json:"motivi,omitempty"`
	DipendeDa       string                `json:"dipende_da,omitempty"`
	Riportate       []string              `json:"riportate,omitempty"`
	NonVerificabile string                `json:"non_verificabile,omitempty"`
}

// VoceCensimento: un messaggio fuori RFQ di un caso di censimento (R34 c): le letture del motore, forma per forma. Le
// etichette attese, quando ci saranno, stanno negli attesi.
type VoceCensimento struct {
	Caso            string           `json:"caso"`
	MessaggioID     uuid.UUID        `json:"messaggio_id"`
	ClienteID       uuid.UUID        `json:"cliente_id"`
	Valutato        bool             `json:"valutato"`
	Motivo          string           `json:"motivo,omitempty"`
	File            int              `json:"file"`
	Prodotti        int              `json:"prodotti"`
	LettureIdentita int              `json:"letture_identita"`
	Letture         []ConteggioForma `json:"letture,omitempty"`
}

// ConteggioForma: le letture di una forma, con la funzione del router.
type ConteggioForma struct {
	Famiglia string `json:"famiglia"`
	Forma    string `json:"forma"`
	Funzione string `json:"funzione"`
	N        int    `json:"n"`
}

// ThreadSenzaCaso: per un thread con un caso, che cosa cambia senza gli ingressi del caso (6.4.9; R48 A: per una
// richiesta inoltrata senza confine, senza il caso nessun prodotto).
type ThreadSenzaCaso struct {
	ThreadID  uuid.UUID     `json:"thread_id"`
	Caso      string        `json:"caso"`
	ConCaso   SintesiThread `json:"con_caso"`
	SenzaCaso SintesiThread `json:"senza_caso"`
	Uguale    bool          `json:"uguale"`
}

// SintesiThread: i numeri di un thread che il caso può cambiare.
type SintesiThread struct {
	Valutato         bool     `json:"valutato"`
	Motivo           string   `json:"motivo,omitempty"`
	StatoRichiesta   string   `json:"stato_richiesta,omitempty"`
	Segmenti         []string `json:"segmenti,omitempty"` // «<segmento> (<origine>)»: scenario, riconoscimento, operatore
	Prodotti         []string `json:"prodotti,omitempty"` // le basi dei candidati prodotto
	ProdottiValutati int      `json:"prodotti_valutati"`
	Candidati        int      `json:"candidati"` // i candidati di ancoraggio dei file
}

// CorrezioniCliente: le correzioni manuali prima e dopo per un cliente (R30 f A), sui thread valutati; i thread non
// valutati sono a parte (D5). È un indicatore ricostruito delle correzioni necessarie, non una misura del tempo
// risparmiato (R114, precisata dall'utente il 07/10, domande-a1c.md).
//   - Correzioni: la misura di confronto, sommata sui thread valutati. Il denominatore è Correzioni.Valutabili, la
//     copertura Valutabili su Decisi; i non determinabili e i fuori copertura, per motivo, sono Correzioni.Esclusi (al
//     posto del vecchio «prima per stringa», che non c'è più: una base che non si legge non è uguale né diversa da
//     niente). Il titolo «prima» del riepilogo è base più marcatore, Prima + PrimaMarcatore, con il marcatore da un
//     solo lato a parte (D-R114 B, decisa dall'utente l'08/10): è solo il testo; nel JSON i due conteggi restano quelli
//     di confronto, separati.
//   - StessaBaseAltroTarget: i decisi con una regressione verso un candidato con la stessa base su un altro target
//     (T-B6-51), su tutti i decisi: fuori dalla misura, non una parte di Dopo (T-B6-191).
//   - RevisioneSoloInColonna: i file con la revisione vecchia solo nella colonna rev (R-65), divisi per lo stato della
//     lettura della colonna (R113 B ratificata: RevisioneInColonna).
type CorrezioniCliente struct {
	ClienteID              uuid.UUID                   `json:"cliente_id"`
	Thread                 int                         `json:"thread"`
	Correzioni             confronto.CorrezioniManuali `json:"correzioni"`
	StessaBaseAltroTarget  int                         `json:"stessa_base_altro_target_fuori_misura"`
	RevisioneSoloInColonna RevisioneInColonna          `json:"revisione_solo_in_colonna"`
	ThreadNonValutati      int                         `json:"thread_non_valutati"`
	CorrezioniNonValutati  confronto.CorrezioniManuali `json:"correzioni_non_valutati"`
}

// RevisioneInColonna: i file la cui revisione vecchia c'è solo nella colonna rev (il codice vecchio si legge senza
// nessuna revisione e la colonna non è vuota: R-65), divisi per lo stato della lettura della colonna con la regola in
// campo separato della famiglia del codice (R113 B ratificata; motorea.LeggiRevisioneRegistrata; T-B6-202). Lo stato
// non è un campo del record piatto: si legge dalla provenienza e dal motivo che valutazione ha dato.
//   - Letta: la colonna si legge, e la revisione vecchia viene da lì (Vecchio.RevisioneDa «colonna»);
//   - NessunaRegola: la famiglia non ha una regola in campo separato (revisione_solo_in_colonna: il limite di R-65);
//   - NonInterpretabile: una regola riservata, una colonna che la regola non legge per intero, un token sospeso
//     (revisione_colonna_non_interpretabile);
//   - Ambigua: più regole, nessuna si sceglie (revisione_colonna_ambigua);
//   - StatoNonNelRecord: il file non è valutato (nuovo_non_valutato prevale sui motivi della colonna), il codice si
//     legge, la revisione vecchia non è interpretata e la colonna non è vuota: lo stato della colonna non si vede, e
//     si conta a parte invece di sparire (T-B6-221). Comprende anche un codice che porta la revisione come token
//     sospeso: il record piatto non li distingue.
type RevisioneInColonna struct {
	Letta             int `json:"letta"`
	NessunaRegola     int `json:"nessuna_regola"`
	NonInterpretabile int `json:"non_interpretabile"`
	Ambigua           int `json:"ambigua"`
	StatoNonNelRecord int `json:"stato_non_nel_record"`
}

// conta: un file nel contatore della colonna, nel suo stato; un file che non ha la revisione solo nella colonna non
// conta.
func (c *RevisioneInColonna) conta(f confronto.File) {
	v, n := f.Vecchio, f.Nuovo
	switch {
	case v.RevisioneDa == valut.RevisioneDaColonna:
		c.Letta++
	case n.MotivoRevisioni == valut.MotivoRevisioniSoloInColonna:
		c.NessunaRegola++
	case n.MotivoRevisioni == valut.MotivoRevisioniColonnaNonInterpretabile:
		c.NonInterpretabile++
	case n.MotivoRevisioni == valut.MotivoRevisioniColonnaAmbigua:
		c.Ambigua++
	case n.MotivoRevisioni == valut.MotivoRevisioniNuovoNonValutato && v.Leggibile && v.RevisioneDa == "" && strings.TrimSpace(v.Rev) != "":
		c.StatoNonNelRecord++
	}
}

// totale: tutti i file del contatore.
func (c RevisioneInColonna) totale() int {
	return c.Letta + c.NessunaRegola + c.NonInterpretabile + c.Ambigua + c.StatoNonNelRecord
}

// ProfiloLimiti: i massimi osservati, contro i limiti che l'indice dichiara e contro i tetti del codice (R43 B; C-23):
// serve a tarare i valori dell'indice.
type ProfiloLimiti struct {
	VersioneLimiti string                          `json:"versione_limiti"`
	Documenti      int                             `json:"documenti"`
	Osservati      grammatica.LimitiRiconoscimento `json:"osservati"`
	Indice         grammatica.LimitiRiconoscimento `json:"indice"`
	Tetti          grammatica.LimitiRiconoscimento `json:"tetti"`
	OltreIndice    []string                        `json:"oltre_indice,omitempty"`
}

// SintesiAttesi: che cosa il runner ha letto degli attesi: quante voci per sezione (i numeri del piano li confronta il
// revisore: nel codice non ci sono), le chiavi non tradotte, i valori che non si leggono, le sezioni descrittive e le
// note, i controlli (1) e (2) della baseline, e le «chiavi libere non verificate» delle sezioni a forma libera (R-109:
// solo informazione, non cambiano l'uscita).
type SintesiAttesi struct {
	Voci         []ConteggioSezione `json:"voci"`
	NonTradotte  []string           `json:"non_tradotte,omitempty"`
	Errori       []string           `json:"errori,omitempty"`
	Riportate    []RigaRiportata    `json:"riportate,omitempty"`
	Baseline     []esitoBaseline    `json:"baseline,omitempty"`
	ChiaviLibere []string           `json:"chiavi_libere_non_verificate,omitempty"`
	IDVoci       []IDVoce           `json:"id_voci,omitempty"`
}

// IDVoce: l'id di una voce di file degli attesi accanto al suo percorso, per leggere i dettagli dei controlli (che
// portano il percorso) con l'id della voce (R-119).
type IDVoce struct {
	Percorso string `json:"percorso"`
	ID       string `json:"id"`
}

// ConteggioSezione: quante voci ha una sezione degli attesi.
type ConteggioSezione struct {
	Sezione string `json:"sezione"`
	Voci    int    `json:"voci"`
}

// PrimaRiga: «ESITO: …», la prima riga del riepilogo (R44), come per il rapporto di A1a. Un esito eseguito porta anche
// il motivo, se c'è: con -attesi e senza -gate, «gate non_superato», che non decide l'uscita ma non si tace.
func (r RapportoBanco) PrimaRiga() string {
	p := Rapporto{Esito: r.Esito, Differenze: r.Differenze, Motivo: r.Motivo}.PrimaRiga()
	if r.Esito != EsitoNonEseguito && r.Motivo != "" {
		p += " — " + r.Motivo
	}
	return p
}

// Testo: il riepilogo, con l'esito in testa: conteggi per cliente, controlli, gate e percorsi. Mai testi di mail.
func (r RapportoBanco) Testo() string {
	var b strings.Builder
	riga := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	riga("%s", r.PrimaRiga())
	riga("modalità: %s", r.Modalita)
	riga("sorgente: %s", vuotoONo(r.Sorgente))
	riga("manifest: %s (sha256 %s)", r.Manifest, vuotoONo(r.Sha256Manifest))
	riga("attesi: sha256 %s, versione_attesi %d", vuotoONo(r.Sha256Attesi), r.VersioneAttesi)
	riga("indice: sha256 %s, impronta %s; casi: sha256 %s", vuotoONo(r.Sha256Indice), vuotoONo(r.ImprontaIndice), vuotoONo(r.Sha256Casi))
	riga("versione_limiti: %s", vuotoONo(r.VersioneLimiti))
	v := r.Versioni
	riga("versioni: rapporto %d, manifest %d, %s, schema %d, indice %d, %s, %s, fotografia %d, casi %d, %s, impronta prodotto %d, formati 2D %d, %s, %s, %s",
		v.Rapporto, v.Manifest, v.Canonicalizzazione, v.SchemaGrammatiche, v.Indice, v.Capacita, v.Algoritmo,
		v.SchemaFotografia, v.Casi, v.Valutazione, v.ImprontaProdotto, v.Formati2D, v.Composizione, v.RevisioneRegistrata, v.Confronto)
	if s := r.SolaLettura; s != nil {
		escluse := "non controllate"
		if s.EscluseNonLeggibili != nil {
			escluse = fmt.Sprint(*s.EscluseNonLeggibili)
		}
		riga("collegamento: ruolo %s, scrittura possibile: %s, tabelle escluse non leggibili: %s, transazione %s, %s", s.Ruolo, siNo(s.ScritturaPossibile),
			escluse, vuotoONo(s.TransazioneSolaLettura), vuotoONo(s.Isolamento))
	}
	if f := r.Fotografia; f != nil {
		riga("fotografia: origine %s, coerente %s, schema %d, thread %d, fuori RFQ %d, impronta %s, diagnostiche %d",
			f.Origine, siNo(f.Coerente), f.SchemaDB, f.Thread, f.FuoriRFQ, vuotoONo(f.Impronta), len(f.Diagnostiche))
		for _, k := range fotorfq.ChiaviSezioni {
			if st, ok := f.Sezioni[k]; ok && st.Stato != fotorfq.StatoSezioneCompleta {
				riga("  sezione %s: %s", k, st.Stato)
			}
		}
	}
	if x := r.Valutazione; x != nil {
		riga("valutazione: impronta %s, thread valutati %d, non valutati %d", vuotoONo(x.Impronta), x.ThreadValutati, x.ThreadNonValutati)
		for _, c := range x.PerCodice {
			riga("  diagnostica %s %s: %d", c.Gravita, c.Codice, c.N)
		}
	}
	riga("controlli:")
	for _, c := range r.Controlli {
		s := "  - " + c.Nome + ": " + c.Stato + classeEsito(c.Classe, c.Esito)
		if c.Differenze > 0 {
			s += fmt.Sprintf(", %d differenze", c.Differenze)
		}
		if c.Motivo != "" {
			s += " — " + c.Motivo
		}
		riga("%s", s)
	}
	for _, d := range r.Dettagli {
		for _, x := range d.Differenze {
			riga("  %s, differenza: %s", d.Nome, x)
		}
		for _, x := range d.NonVerificate {
			riga("  %s, non verificata: %s", d.Nome, x)
		}
		for _, x := range d.NonApplicabili {
			riga("  %s, non applicabile in questa corsa: %s", d.Nome, x)
		}
		for _, x := range d.AmbitiDedotti {
			riga("  %s, ambito dedotto: %s", d.Nome, x)
		}
		for _, x := range d.FuoriPerimetro {
			riga("  %s, fuori perimetro: %s", d.Nome, x)
		}
	}
	if a := r.RigheConfermaAlbero; a != nil {
		riga("righe della «Conferma l'albero» (solo informazione, D-R116 I): %d, non verificate — %s", a.Righe, a.Motivo)
		for _, p := range a.Percorsi {
			riga("  riga della «Conferma l'albero», non verificata: %s", p)
		}
	}
	for _, pc := range perClienteTesto(r) {
		riga("%s", pc)
	}
	if g := r.Gate; g != nil {
		riga("gate: %s (false associazioni %d; decisioni preservate %d su %d; da rivedere %d, non coperti %d, non valutati %d)",
			g.Esito, g.FalseAssociazioni, g.DecisioniPreservate, g.DecisioniBaseline, g.DaRivedere, g.NonCoperti, len(g.NonValutati))
		for _, v := range g.Voci {
			s := "  - " + v.Nome + ": " + v.Stato + classeEsito(v.Classe, v.Esito)
			if v.Motivo != "" {
				s += " — " + v.Motivo
			}
			riga("%s", s)
		}
		for _, c := range g.PerCliente {
			riga("  cliente %s: copertura %d, astensioni %d, bloccanti %d", c.ClienteID, c.Copertura, c.Astensioni, c.Bloccanti)
		}
	}
	if r.Chiusura != nil {
		testoChiusura(riga, r.Chiusura)
	}
	if a := r.Attesi; a != nil {
		var parti []string
		for _, c := range a.Voci {
			parti = append(parti, fmt.Sprintf("%s %d", c.Sezione, c.Voci))
		}
		riga("attesi letti: %s; chiavi non tradotte %d, valori non letti %d, chiavi libere non verificate %d (solo informazione)",
			strings.Join(parti, ", "), len(a.NonTradotte), len(a.Errori), len(a.ChiaviLibere))
	}
	if r.Casi != nil {
		n := r.Casi.Conteggi
		riga("casi di contratto: %d — passati %d, parziali %d, falliti %d, riservati %d, rimandati %d", n.Totale, n.Passati, n.Parziali, n.Falliti, n.Riservati, n.Rimandati)
	}
	for _, s := range r.SenzaCaso {
		riga("senza il caso: thread %s: prodotti %d con il caso, %d senza", s.ThreadID, len(s.ConCaso.Prodotti), len(s.SenzaCaso.Prodotti))
	}
	if len(r.Censimento) > 0 {
		lett := 0
		for _, c := range r.Censimento {
			lett += c.LettureIdentita
		}
		riga("censimento: %d messaggi fuori RFQ, %d letture d'identità", len(r.Censimento), lett)
	}
	if p := r.Prodotti; p != nil {
		stati := map[string]int{}
		n := 0
		for _, t := range p.Thread {
			for _, x := range t.Prodotti {
				stati[x.Stato]++
				n++
			}
		}
		riga("prodotti (solo informazione): %d, %s%s", n, elencoConteggi(stati), seNonCalcolata(p))
		if p.FontiScenario != nil {
			testoFonti(riga, p.FontiScenario)
		}
	}
	if l := r.ProfiloLimiti; l != nil {
		riga("profilo_limiti (%s): byte per unità %d, unità per documento %d, letture per unità %d, letture per documento %d; oltre l'indice: %s",
			vuotoONo(l.VersioneLimiti), l.Osservati.MaxByteUnita, l.Osservati.MaxUnitaDocumento, l.Osservati.MaxLetturePerUnita,
			l.Osservati.MaxLettureDocumento, elenco(l.OltreIndice))
	}
	riga("scritture: %s", r.Scritture)
	return b.String()
}

func siNo(b bool) string {
	if b {
		return "sì"
	}
	return "no"
}

// classeEsito: « [classe, esito]» accanto allo stato di un controllo o di una voce del gate (R116 B); vuoto se il
// controllo non è classificato.
func classeEsito(c ClasseControllo, e EsitoTriStato) string {
	if c == "" && e == "" {
		return ""
	}
	return " [" + string(c) + ", " + string(e) + "]"
}

// elencoConteggi: «a 2, b 1» in ordine di nome.
func elencoConteggi(m map[string]int) string {
	nomi := make([]string, 0, len(m))
	for k := range m {
		nomi = append(nomi, k)
	}
	sort.Strings(nomi)
	var parti []string
	for _, k := range nomi {
		parti = append(parti, fmt.Sprintf("%s %d", k, m[k]))
	}
	if len(parti) == 0 {
		return "—"
	}
	return strings.Join(parti, ", ")
}

// testoFonti: la fonte contro l'atteso dei prodotti attesi dello scenario nel riepilogo (PO-29, R109): una riga per
// prodotto atteso, con la regola, le voci usate, l'atteso e il calcolato, le differenze e le parti non verificate. Un
// «assente» senza motivo negli attesi lo dice («motivo non fissato dagli attesi»), non tace.
func testoFonti(riga func(string, ...any), fs *FontiDelloScenario) {
	if fs.NonVerificabile != "" {
		riga("fonte contro l'atteso dello scenario (PO-29, solo informazione): %d prodotti attesi, non verificata in questa corsa — %s",
			len(fs.Prodotti), fs.NonVerificabile)
	} else {
		riga("fonte contro l'atteso dello scenario (PO-29, solo informazione): %d prodotti attesi; calcolati senza atteso: %s",
			len(fs.Prodotti), elenco(fs.CalcolatiSenzaAtteso))
	}
	for _, c := range fs.Prodotti {
		atteso := "non derivato"
		switch {
		case c.Atteso.Motivo != "":
			atteso = c.Atteso.Stato + ", " + c.Atteso.Motivo
		case c.Atteso.Stato != "":
			atteso = c.Atteso.Stato + ", motivo non fissato dagli attesi"
		}
		calcolato := "nessuno"
		if k := c.Calcolato; k != nil {
			calcolato = vuotoONo(k.Stato) + ", " + vuotoONo(k.Motivo)
		}
		s := fmt.Sprintf("  prodotto atteso %s (regola %s, %d voci radice): atteso %s; calcolato %s", c.Base, c.Derivazione.Regola,
			len(c.Derivazione.Voci)+len(c.Derivazione.NonRisolte), atteso, calcolato)
		if len(c.Differenze) > 0 {
			s += "; differenze: " + strings.Join(c.Differenze, ", ")
		}
		if len(c.NonVerificate) > 0 {
			s += "; non verificate: " + strings.Join(c.NonVerificate, "; ")
		}
		riga("%s", s)
	}
}

func seNonCalcolata(p *SezioneProdotti) string {
	if p.Calcolata {
		return ""
	}
	return "; non calcolata dove manca: " + strings.Join(p.SezioniAssenti, ", ")
}

// perClienteTesto: per cliente, thread, file, badge, esiti e correzioni, in ordine di cliente.
func perClienteTesto(r RapportoBanco) []string {
	type somma struct {
		thread, valutati, file int
		badge                  map[string]int
		esiti                  map[string]int
	}
	per := map[uuid.UUID]*somma{}
	var ordine []uuid.UUID
	for _, t := range r.Thread {
		s := per[t.ClienteID]
		if s == nil {
			s = &somma{badge: map[string]int{}, esiti: map[string]int{}}
			per[t.ClienteID] = s
			ordine = append(ordine, t.ClienteID)
		}
		s.thread++
		if t.Valutato {
			s.valutati++
		}
		s.file += len(t.File)
		for b, n := range t.Conteggi {
			if n > 0 {
				s.badge[string(b)] += n
			}
		}
		for _, f := range t.File {
			for _, e := range f.Esiti {
				s.esiti[nomeEsitoNelRiepilogo(e.Esito)]++
			}
		}
	}
	sort.Slice(ordine, func(i, j int) bool { return ordine[i].String() < ordine[j].String() })
	var out []string
	for _, c := range ordine {
		s := per[c]
		out = append(out, fmt.Sprintf("cliente %s: thread %d (valutati %d), file %d; badge: %s; esiti: %s",
			c, s.thread, s.valutati, s.file, elencoConteggi(s.badge), elencoConteggi(s.esiti)))
	}
	for _, c := range r.CorrezioniManuali {
		out = append(out, testoCorrezioni(c))
	}
	return out
}

// testoCorrezioni: la misura delle correzioni manuali di un cliente, come la definisce R114 (precisata dall'utente il
// 07/10): un indicatore ricostruito delle correzioni necessarie, con il denominatore, la copertura e gli esclusi per
// motivo; il titolo «prima» è base più marcatore (D-R114 B, decisa dall'utente l'08/10): Prima + PrimaMarcatore, con le
// due parti accanto, e il marcatore da un solo lato a parte, fuori dal titolo; «dopo» nelle sue tre parti. Nessuna
// misura di tempo.
func testoCorrezioni(c CorrezioniCliente) string {
	x, e, r := c.Correzioni, c.Correzioni.Esclusi, c.RevisioneSoloInColonna
	return fmt.Sprintf("correzioni manuali, cliente %s (indicatore ricostruito delle correzioni necessarie): decisi %d, denominatore %d valutabili (copertura %d su %d); "+
		"esclusi per motivo: solo_origine_manuale %d, senza_lettura_vecchia %d, codice_non_leggibile %d, documento_senza_componente %d, nuovo_non_valutato %d; "+
		"prima %d (base e marcatore, D-R114 B: cambi di base %d, marcatori diversi %d), a parte marcatore solo da un lato %d; "+
		"dopo %d (false associazioni %d, ambiguità %d, astensioni %d); solo revisione %d; "+
		"fuori dalla misura: stessa base su un altro target %d, revisione solo in colonna %d (letta %d, nessuna regola %d, non interpretabile %d, ambigua %d, stato non nel record %d); "+
		"a parte %d thread non valutati (decisi %d)",
		c.ClienteID, x.Decisi, x.Valutabili, x.Valutabili, x.Decisi,
		e.SoloOrigineManuale, e.SenzaLetturaVecchia, e.CodiceNonLeggibile, e.DocumentoSenzaComponente, e.NuovoNonValutato,
		x.Prima+x.PrimaMarcatore, x.Prima, x.PrimaMarcatore, x.MarcatoreSoloDaUnLato,
		x.Dopo, x.FalseAssociazioni, x.Ambiguita, x.Astensioni, x.SoloRevisione,
		c.StessaBaseAltroTarget, c.RevisioneSoloInColonna.totale(), r.Letta, r.NessunaRegola, r.NonInterpretabile, r.Ambigua, r.StatoNonNelRecord,
		c.ThreadNonValutati, c.CorrezioniNonValutati.Decisi)
}

// nomeEsitoNelRiepilogo: «mancante» si scrive «senza risposta», per non confonderlo con la disponibilità del file (R30 c).
func nomeEsitoNelRiepilogo(e confronto.EsitoAtteso) string {
	if e == confronto.EsitoMancante {
		return "senza_risposta"
	}
	return string(e)
}

// nomeRapportoBanco: il nome dei file del rapporto di una modalità di A1c, senza estensione (6.4.9).
func nomeRapportoBanco(modalita string) string { return "rapporto-" + modalita }

// scriviRapportoBanco: JSON canonico più il riepilogo di testo, scritti in modo atomico (.tmp, poi Rename) nella
// cartella dei rapporti, che si crea se manca: rapporto-dsn.json e .txt, o rapporto-exports.json e .txt. Rifiuta una
// cartella dentro il modulo. Restituisce il percorso del JSON.
func scriviRapportoBanco(cartella string, r RapportoBanco) (string, error) {
	if r.Modalita != ModalitaDSN && r.Modalita != ModalitaExports {
		return "", fmt.Errorf("bancoa: modalità %q: il nome del rapporto non si sa", r.Modalita)
	}
	if err := dataset.FuoriDalModulo(cartella); err != nil {
		return "", err
	}
	if err := os.MkdirAll(cartella, 0o755); err != nil {
		return "", fmt.Errorf("bancoa: cartella dei rapporti: %w", err)
	}
	js, err := jsoncanonico.Codifica(r)
	if err != nil {
		return "", fmt.Errorf("bancoa: forma canonica del rapporto: %w", err)
	}
	base := filepath.Join(cartella, nomeRapportoBanco(r.Modalita))
	if err := scriviAtomico(base+".json", js); err != nil {
		return "", err
	}
	if err := scriviAtomico(base+".txt", []byte(r.Testo())); err != nil {
		return "", err
	}
	return base + ".json", nil
}
