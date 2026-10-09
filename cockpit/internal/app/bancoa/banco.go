package bancoa

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/estrazione/evidenze"
	"promatec/cockpit/internal/core/fotorfq"
	"promatec/cockpit/internal/core/fotorfq/caricatore"
	"promatec/cockpit/internal/core/inbox/classificazione/motorea"
	"promatec/cockpit/internal/core/registro/regole/grammatica"
	valut "promatec/cockpit/internal/core/valutazione"
	"promatec/cockpit/internal/platform/dataset"
	"promatec/cockpit/internal/platform/jsoncanonico"
	"promatec/cockpit/internal/platform/migrazioni"
)

// banco.go: le modalità dsn ed exports (piano 6.4.9; T-B6-03, T-B6-04, F0-01, F0-16). Il punto d'ingresso è EseguiBanco:
// Esegui resta quello di A1a, con il suo rapporto (F0-01, condizioni d ed e).
//
// Il banco legge e basta: il DB in sola lettura, attraverso platform/migrazioni (l'apertura e i controlli del
// collegamento) e il caricatore (la fotografia, in una transazione REPEATABLE READ READ ONLY), oppure gli export. Poi
// lo stesso percorso puro del prodotto: valutazione.Calcola, la copia di passaggio.go, confronto.Confronta. Gli attesi
// li legge solo qui (P-11, R2), con -attesi. Le sole scritture sono i file del rapporto, nella cartella dei rapporti,
// fuori dal modulo. Questo pacchetto non nomina pgx: il pool di ApriInLettura passa al caricatore e ai controlli per
// inferenza di tipo.

// EseguiBanco esegue la modalità dsn o exports e scrive il rapporto (versione 3). Le righe a video:
//  1. «sorgente: …» (con -dsn prima di collegarsi, la destinazione senza password; con -exports la cartella, i file e
//     lo sha256 del manifest);
//  2. con -dsn «collegato in sola lettura: ruolo …, scrittura possibile: no, tabelle escluse non leggibili: n»; con
//     -exports «nessun database aperto; fatti dagli export (bypass di Outlook, download e parser Python)»;
//  3. «rapporto: <percorso assoluto>», con la prima scrittura del rapporto («ESITO: NON ESEGUITO — lettura non
//     cominciata») prima di leggere la fotografia;
//
// poi il riepilogo, con l'esito in testa e l'ultima riga «scritture: …». Codici d'uscita (R44): 0 conforme (con -gate,
// gate superato); 1 con differenze (un errore di contratto, un controllo del runner, un caso fallito; con -gate una
// voce non superata); 3 NON ESEGUITO (un file del manifest che manca o è cambiato, la copia non raggiungibile o non
// quella del manifest; con -gate una voce non eseguita e nessuna fallita). 1 prevale su 3. Le sentinelle della copia,
// delegate ad A1c-L4D-01, non decidono l'uscita (R117 b); le classi dei controlli e la chiusura non la cambiano mai
// (R116 B: il 3 non diventa 0).
//
// L'errore è un *ErroreUso per le opzioni sbagliate (uscita 2, nessun rapporto: F0-01 a, b, c), un altro errore se il
// rapporto non si scrive; in tutti e due i casi il RapportoBanco restituito è vuoto. Se fallisce solo la scrittura su
// w, il rapporto c'è ed è restituito insieme all'errore.
func EseguiBanco(ctx context.Context, o Opzioni, w io.Writer) (RapportoBanco, error) {
	if err := o.validaBanco(); err != nil {
		return RapportoBanco{}, err
	}
	b := nuovoBanco(o, w)
	if o.Modalita == ModalitaDSN {
		dest, _, _ := migrazioni.Destinazione(o.DSN) // già controllata da validaBanco
		b.destinazione = dest
		b.r.Sorgente = dest + " (banco, sola lettura)"
		b.riga("sorgente: %s", b.r.Sorgente)
	}
	if b.leggiDataset() {
		switch o.Modalita {
		case ModalitaDSN:
			b.fotografiaDSN(ctx)
		case ModalitaExports:
			b.fotografiaExport()
		}
		if b.fotoLetta && ctx.Err() == nil {
			b.valuta()
		}
	} else if o.Modalita == ModalitaExports {
		b.r.Sorgente = "export " + b.cartellaExport
		b.riga("sorgente: %s", b.r.Sorgente)
	}
	if ctx.Err() != nil {
		b.controllo("interruzione", ControlloNonEseguito, 0, "interrotto: "+ctx.Err().Error())
	}
	b.chiudi()
	if b.errScrittura != nil {
		return RapportoBanco{}, b.errScrittura
	}
	if _, err := scriviRapportoBanco(o.Uscita, b.r); err != nil {
		return RapportoBanco{}, err
	}
	if !b.rapportoAnnunciato {
		b.riga("rapporto: %s", b.percorso)
	}
	if b.errVideo != nil {
		return b.r, b.errVideo
	}
	if w != nil {
		if _, err := io.WriteString(w, b.r.Testo()); err != nil {
			return b.r, err
		}
	}
	return b.r, nil
}

// validaBanco: le opzioni delle modalità dsn ed exports. Ogni errore è un *ErroreUso (uscita 2), e non ripete mai il
// DSN, che può contenere la password (modello comandi.go; R32 c).
func (o Opzioni) validaBanco() error {
	uso := func(f string, a ...any) error { return &ErroreUso{Motivo: fmt.Sprintf(f, a...)} }
	switch o.Modalita {
	case ModalitaDSN:
		switch {
		case strings.TrimSpace(o.DSN) == "":
			return uso("la modalità %s vuole il DSN della copia (-dsn)", ModalitaDSN)
		case o.Exports != "":
			return uso("-dsn ed -exports si escludono")
		case len(o.Thread) == 0 && !o.Tutti:
			return uso("con -dsn serve -thread oppure -tutti")
		case len(o.Thread) > 0 && o.Tutti:
			return uso("-thread e -tutti si escludono")
		case o.Migrazioni == nil:
			return uso("mancano le migrazioni incorporate del binario: servono al controllo dello schema (F0-16)")
		}
		if err := dsnSenzaPassword(o.DSN); err != nil {
			return &ErroreUso{Motivo: err.Error()}
		}
		if _, ok := os.LookupEnv("PGPASSWORD"); ok {
			return uso("PGPASSWORD è impostata: il banco legge la password solo da pgpass.conf (R32 c)")
		}
		if _, _, err := migrazioni.Destinazione(o.DSN); err != nil {
			return &ErroreUso{Motivo: err.Error()}
		}
	case ModalitaExports:
		switch {
		case strings.TrimSpace(o.Exports) == "":
			return uso("la modalità %s vuole la cartella degli export (-exports)", ModalitaExports)
		case o.DSN != "":
			return uso("-dsn ed -exports si escludono")
		case o.Tutti:
			return uso("-tutti vale solo con -dsn")
		}
		if err := dataset.FuoriDalModulo(o.Exports); err != nil {
			return uso("cartella degli export: %s", err.Error())
		}
	default:
		return uso("modalità %q: EseguiBanco esegue %s ed %s; %s e %s si eseguono con Esegui", o.Modalita, ModalitaDSN, ModalitaExports, ModalitaRegole, ModalitaCasi)
	}
	switch {
	case o.Gate && !o.Attesi:
		return uso("-gate vuole -attesi: senza gli attesi il gate non c'è")
	case strings.TrimSpace(o.Dataset) == "":
		return uso("manca il manifest del dataset (-dataset)")
	case strings.TrimSpace(o.Uscita) == "":
		return uso("manca la cartella dei rapporti (-uscita)")
	}
	if err := dataset.FuoriDalModulo(o.Uscita); err != nil {
		return uso("cartella dei rapporti: %s", err.Error())
	}
	if err := dataset.FuoriDalModulo(o.Dataset); err != nil {
		return uso("manifest: %s", err.Error())
	}
	return nil
}

// chiavePassword: la chiave password in un DSN chiave=valore (pgconn accetta spazi attorno a «=»).
var chiavePassword = regexp.MustCompile(`(?i)(^|\s)password\s*=`)

// dsnSenzaPassword: il controllo guarda il testo del DSN, non la configurazione di pgx, che a password vuota la riempie
// da pgpass (6.4.9; R32 c). Nella forma URL: la password dell'utente o il parametro password; nella forma chiave=valore:
// la chiave password. L'errore non ripete il DSN.
func dsnSenzaPassword(dsn string) error {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return errors.New("il DSN non si legge (non si ripete: può contenere la password)")
		}
		if _, ha := u.User.Password(); ha || u.Query().Has("password") {
			return errors.New("il DSN contiene la password: va tolta, il banco la legge da pgpass.conf (R32 c; il DSN non si ripete)")
		}
		return nil
	}
	if chiavePassword.MatchString(dsn) {
		return errors.New("il DSN contiene la password: va tolta, il banco la legge da pgpass.conf (R32 c; il DSN non si ripete)")
	}
	return nil
}

// FlagThread: il flag -thread di cmd/bancoa, ripetibile; ogni valore è l'UUID di una RFQ, aggiunto a *dest (di solito
// &Opzioni.Thread). Sta qui perché il comando non nomini la libreria degli UUID: il suo grafo resta quello di I.2
// (R-114, T-B6-114).
func FlagThread(dest *[]uuid.UUID) flag.Value { return (*elencoThread)(dest) }

// elencoThread: i thread scelti, come flag.Value.
type elencoThread []uuid.UUID

func (e *elencoThread) String() string {
	if e == nil {
		return ""
	}
	var s []string
	for _, u := range *e {
		s = append(s, u.String())
	}
	return strings.Join(s, ",")
}

func (e *elencoThread) Set(v string) error {
	u, err := uuid.Parse(v)
	if err != nil {
		return errors.New("-thread vuole un UUID")
	}
	*e = append(*e, u)
	return nil
}

// banco: lo stato di un'esecuzione delle modalità dsn ed exports.
type banco struct {
	o                  Opzioni
	w                  io.Writer
	r                  RapportoBanco
	percorso           string // il JSON del rapporto, assoluto
	destinazione       string
	cartellaExport     string
	rapportoAnnunciato bool
	errScrittura       error
	errVideo           error

	manifest   dataset.Manifest
	profili    map[string]uuid.UUID
	indice     *grammatica.IndiceRegole
	cartIdx    string
	ins        *motorea.InsiemeRegole
	attesi     *Attesi
	attesiLett bool
	motivoAtt  string
	sezioni    *SezioniAttesi
	ingressi   valut.Ingressi
	casiLetti  bool
	shaExport  []string
	// mancantiCopia, mancantiExport: i messaggi fuori RFQ dei casi che la copia non ha (Carica li dice inesistenti:
	// una differenza) o che gli export non hanno (solo i messaggi in entrata: una parte non verificabile).
	mancantiCopia  []uuid.UUID
	mancantiExport []uuid.UUID

	foto      fotorfq.Fotografia
	fotoLetta bool
}

func nuovoBanco(o Opzioni, w io.Writer) *banco {
	abs, err := filepath.Abs(o.Uscita)
	if err != nil {
		abs = o.Uscita
	}
	b := &banco{o: o, w: w, percorso: filepath.Join(abs, nomeRapportoBanco(o.Modalita)+".json")}
	if c, err := filepath.Abs(o.Exports); err == nil && o.Exports != "" {
		b.cartellaExport = c
	}
	b.r = RapportoBanco{VersioneRapporto: VersioneRapportoBanco, Modalita: o.Modalita, Manifest: o.Dataset, Versioni: versioniBanco()}
	if o.Modalita == ModalitaDSN {
		b.r.Scritture = "solo " + b.percorso + " (e il riepilogo .txt accanto); nessuna scrittura sul database"
	} else {
		b.r.Scritture = "solo " + b.percorso + " (e il riepilogo .txt accanto); nessun database aperto"
	}
	return b
}

func versioniBanco() VersioniBanco {
	return VersioniBanco{
		Rapporto: VersioneRapportoBanco, Manifest: dataset.VersioneManifest, Canonicalizzazione: jsoncanonico.Versione,
		SchemaGrammatiche: grammatica.VersioneSchema, Indice: grammatica.VersioneIndice, Capacita: grammatica.VersioneCapacita,
		Algoritmo: motorea.VersioneAlgoritmo, SchemaFotografia: fotorfq.VersioneSchema, Casi: valut.VersioneCasi,
		Valutazione: valut.VersioneValutazione, ImprontaProdotto: valut.VersioneImprontaProdotto,
		Formati2D: valut.VersioneFormati2D, Composizione: motorea.VersioneComposizione,
		RevisioneRegistrata: motorea.VersioneRevisioneRegistrata, Confronto: confronto.VersioneConfronto,
	}
}

// riga: una riga a video. Un errore di scrittura su w non ferma il banco: si restituisce alla fine.
func (b *banco) riga(f string, a ...any) {
	if b.w == nil || b.errVideo != nil {
		return
	}
	if _, err := fmt.Fprintf(b.w, f+"\n", a...); err != nil {
		b.errVideo = err
	}
}

func (b *banco) controllo(nome, stato string, differenze int, motivo string) {
	b.r.Controlli = append(b.r.Controlli, Controllo{Nome: nome, Stato: stato, Differenze: differenze, Motivo: motivo})
}

// esito: un controllo del banco nel rapporto, con il dettaglio delle differenze, delle parti non verificate, di quelle
// non applicabili e di quelle fuori perimetro (D-R116 F).
func (b *banco) esito(e esitoControllo) {
	b.r.Controlli = append(b.r.Controlli, e.controllo())
	if len(e.differenze) > 0 || len(e.nonFatti) > 0 || len(e.nonApplicabili) > 0 || len(e.ambitiDedotti) > 0 || len(e.fuoriPerimetro) > 0 {
		b.r.Dettagli = append(b.r.Dettagli, DettaglioControllo{Nome: e.nome, Differenze: e.differenze, NonVerificate: e.nonFatti,
			NonApplicabili: e.nonApplicabili, AmbitiDedotti: e.ambitiDedotti, FuoriPerimetro: e.fuoriPerimetro})
	}
}

// chiudi: l'esito dai controlli, come in A1a: una differenza prevale su un non eseguito (R44). Con -attesi e senza
// -gate il gate non decide l'uscita, ma se non è superato il motivo della prima riga lo dice.
//
// Prima la classe e l'esito di ogni controllo, dalla tabella di classi.go; dopo l'esito, il riepilogo della chiusura
// (R116 B, precisata dall'utente il 07/10). Né l'una né l'altro cambiano l'uscita: un controllo delegato (le sentinelle,
// R117 b) non è NON ESEGUITO, e per questo solo non entra fra i motivi.
func (b *banco) chiudi() {
	b.r.Controlli = classificaControlli(b.r.Controlli)
	var motivi []string
	b.r.Differenze = 0
	for _, c := range b.r.Controlli {
		b.r.Differenze += c.Differenze
		if c.Stato == ControlloNonEseguito {
			motivi = append(motivi, c.Nome+": "+c.Motivo)
		}
	}
	switch {
	case b.r.Differenze > 0:
		b.r.Esito, b.r.Motivo = EsitoConDifferenze, ""
	case len(motivi) > 0:
		b.r.Esito, b.r.Motivo = EsitoNonEseguito, strings.Join(motivi, "; ")
	default:
		b.r.Esito, b.r.Motivo = EsitoConforme, ""
	}
	if !b.o.Gate && b.r.Gate != nil && b.r.Gate.Esito == GateNonSuperato {
		b.r.Motivo = unisciMotivi(b.r.Motivo, "gate non_superato (senza -gate non decide l'uscita)")
	}
	b.r.Chiusura = chiusuraDi(b.r)
}

// primaScrittura: il rapporto con «ESITO: NON ESEGUITO — lettura non cominciata», prima di leggere la fotografia:
// prova che il percorso si scrive, e se la corsa si ferma il rapporto lo dice (R44; modello comandi.go). I controlli
// hanno già la classe e l'esito (R116 B); la chiusura no, perché la corsa non è finita.
func (b *banco) primaScrittura() {
	r := b.r
	r.Controlli = classificaControlli(b.r.Controlli)
	r.Esito, r.Motivo, r.Differenze = EsitoNonEseguito, "lettura non cominciata", 0
	if _, err := scriviRapportoBanco(b.o.Uscita, r); err != nil {
		b.errScrittura = err
		return
	}
	b.riga("rapporto: %s", b.percorso)
	b.rapportoAnnunciato = true
}

// leggiDataset: il manifest, l'indice delle regole, gli attesi (con -attesi), il file dei casi, le grammatiche. Le
// letture del manifest, dell'indice e degli attesi sono quelle di A1a (esecuzione), con gli stessi controlli. Il file
// dei casi si legge qui, prima della fotografia (T-B6-04): porta i messaggi fuori RFQ dei casi di censimento.
func (b *banco) leggiDataset() bool {
	e := &esecuzione{}
	ok := e.leggiManifest(b.o.Dataset)
	b.r.Sha256Manifest = e.r.Sha256Manifest
	b.r.Controlli = append(b.r.Controlli, e.r.Controlli...)
	if !ok {
		return false
	}
	e.r.Controlli = nil
	b.manifest, b.profili = e.manifest, e.profili
	e.leggiIndice()
	if b.o.Attesi {
		e.leggiAttesi()
	}
	b.r.Controlli = append(b.r.Controlli, e.r.Controlli...)
	b.r.Sha256Indice, b.r.VersioneLimiti = e.r.Sha256Indice, e.r.VersioneLimiti
	b.r.Sha256Attesi, b.r.VersioneAttesi = e.r.Sha256Attesi, e.r.VersioneAttesi
	b.indice, b.cartIdx, b.attesi = e.indice, e.cartIdx, e.attesi
	if b.o.Attesi {
		b.leggiSezioni()
	}
	b.leggiCasi()
	if b.indice == nil {
		b.controllo("grammatiche", ControlloNonEseguito, 0, "senza un indice valido le grammatiche non si compilano")
	} else {
		ins, _, err := motorea.CompilaInsieme(*b.indice, e.contenuti())
		if err != nil {
			b.controllo("grammatiche", ControlloEseguito, 1, "indice non valido: "+testoDiagnostiche(err))
		} else {
			b.ins = &ins
			b.r.ImprontaIndice = ins.ImprontaIndice
			b.controllo("grammatiche", ControlloEseguito, len(ins.Scartati), fmt.Sprintf("%d clienti attivi, %d scartati", len(ins.Motori), len(ins.Scartati)))
		}
	}
	return true
}

// leggiSezioni: le sezioni degli attesi (LeggiSezioni), dagli stessi byte che ha letto LeggiAttesi.
func (b *banco) leggiSezioni() {
	raw, err := b.manifest.LeggiFile(VoceAttesi)
	if err != nil {
		b.motivoAtt = err.Error()
		return
	}
	if b.attesi != nil {
		b.attesiLett = true
	} else {
		b.motivoAtt = "LeggiAttesi rifiuta gli attesi (vedi il controllo attesi)"
	}
	s, err := LeggiSezioni(raw)
	if err != nil {
		b.motivoAtt = unisciMotivi(b.motivoAtt, err.Error())
		return
	}
	b.sezioni = &s
}

// leggiCasi: la voce casi del manifest e LeggiIngressi (6.6.3). Un file che manca o è cambiato è NON ESEGUITO; un file
// che LeggiIngressi rifiuta è una differenza. Senza il file dei casi la valutazione si fa lo stesso, senza ingressi.
func (b *banco) leggiCasi() {
	raw, err := b.manifest.LeggiFile(VoceCasi)
	if err != nil {
		b.controllo(VoceCasi, ControlloNonEseguito, 0, err.Error())
		return
	}
	b.r.Sha256Casi = impronta(raw)
	in, err := valut.LeggiIngressi(raw)
	if err != nil {
		b.controllo(VoceCasi, ControlloEseguito, 1, "file dei casi non valido: "+testoDiagnostiche(err))
		return
	}
	b.ingressi, b.casiLetti = in, true
	b.controllo(VoceCasi, ControlloEseguito, 0, fmt.Sprintf("%d casi", len(in.Casi)))
}

// messaggiFuori: i messaggi dei casi senza thread, i soli che il caricatore e il lettore degli export prendono come
// messaggi fuori RFQ (D-V1-3; R34): i messaggi di un caso con il thread sono quelli della sua richiesta.
func (b *banco) messaggiFuori() []uuid.UUID {
	var out []uuid.UUID
	visti := map[uuid.UUID]bool{}
	for _, c := range b.ingressi.Casi {
		if c.ThreadID != nil {
			continue
		}
		for _, m := range c.Messaggi {
			if !visti[m] {
				visti[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out
}

// fotografiaDSN: la sequenza -dsn (6.4.9, passi 2–5): l'apertura in sola lettura con il controllo dello schema, i
// controlli del collegamento con le tabelle escluse del manifest, la copia del manifest (ruolo, database, schema), la
// prima scrittura del rapporto, poi caricatore.Carica.
func (b *banco) fotografiaDSN(ctx context.Context) {
	pool, err := migrazioni.ApriInLettura(ctx, b.o.DSN, b.o.Migrazioni)
	if err != nil {
		var sd *migrazioni.SchemaDiverso
		if errors.As(err, &sd) {
			b.controllo("copia", ControlloNonEseguito, 0, fmt.Sprintf("la copia è alla %d, questo bancoa alla %d: usare un bancoa compilato da un commit di A1 oppure una copia TEMPLATE migrata dal proprietario, con lo script dei permessi del banco rilanciato", sd.DelDatabase, sd.DelBinario))
		} else {
			b.controllo("copia", ControlloNonEseguito, 0, "la copia non si raggiunge: "+err.Error())
		}
		return
	}
	defer pool.Close()
	attesa := b.manifest.Copia
	col, err := migrazioni.ControllaSolaLettura(ctx, pool, attesa.Escluse)
	b.r.SolaLettura = solaLetturaDi(col, err, attesa.Escluse)
	if err != nil {
		b.controllo("sola_lettura", ControlloNonEseguito, 0, err.Error())
		return
	}
	b.controllo("sola_lettura", ControlloEseguito, 0, "")
	ultima := -1
	if migs, err := migrazioni.Elenca(b.o.Migrazioni); err == nil && len(migs) > 0 {
		ultima = migs[len(migs)-1].Versione
	}
	copia, fermo, sentinelle := controllaCopia(attesa, col, ultima)
	b.esito(copia)
	if fermo {
		return
	}
	if sentinelle != nil {
		b.esito(*sentinelle)
	}
	b.riga("collegato in sola lettura: ruolo %s, scrittura possibile: %s, tabelle escluse non leggibili: %d", col.Utente, siNo(col.Scrive), *b.r.SolaLettura.EscluseNonLeggibili)
	b.primaScrittura()
	if b.errScrittura != nil {
		return
	}
	f, mancanti, err := caricaSenzaIMancanti(func(messaggi []uuid.UUID) (fotorfq.Fotografia, error) {
		return caricatore.Carica(ctx, pool, caricatore.Richiesta{Thread: b.o.Thread, Tutti: b.o.Tutti, Messaggi: messaggi, Sorgente: b.destinazione})
	}, b.messaggiFuori())
	b.mancantiCopia = mancanti
	if err != nil {
		b.controllo("fotografia", ControlloNonEseguito, 0, "la fotografia non si legge: "+err.Error())
		return
	}
	b.r.SolaLettura.TransazioneSolaLettura, b.r.SolaLettura.Isolamento = f.SolaLettura, f.Isolamento
	b.foto, b.fotoLetta = f, true
	b.controllo("fotografia", ControlloEseguito, 0, "")
}

// caricaSenzaIMancanti: Carica con i messaggi fuori RFQ dei casi; se Carica dice che un messaggio dei casi non esiste
// nella copia, quel messaggio si toglie e si ricarica (una volta per messaggio, al più), così la corsa continua e il
// messaggio che manca diventa una differenza del controllo dei casi (T-B6-74, T-B6-112: con -dsn il file dei casi
// contraddice i dati, uscita 1). Un altro errore resta un errore: la fotografia non si legge (uscita 3). Ogni carica è
// una transazione di sola lettura a sé; la copia non cambia fra due cariche.
func caricaSenzaIMancanti(carica func([]uuid.UUID) (fotorfq.Fotografia, error), messaggi []uuid.UUID) (fotorfq.Fotografia, []uuid.UUID, error) {
	resto := append([]uuid.UUID(nil), messaggi...)
	var mancanti []uuid.UUID
	for {
		f, err := carica(resto)
		if err == nil {
			return f, mancanti, nil
		}
		id, ok := messaggioInesistente(err, resto)
		if !ok {
			return fotorfq.Fotografia{}, mancanti, err
		}
		mancanti = append(mancanti, id)
		var altri []uuid.UUID
		for _, m := range resto {
			if m != id {
				altri = append(altri, m)
			}
		}
		resto = altri
	}
}

// formatoMessaggioInesistente: l'errore del caricatore per un messaggio fuori RFQ che la copia non ha (caricatore.go,
// leggiFuori). Il caricatore non ha un errore sentinella: la prova del banco legge il sorgente del caricatore e cade se
// questo testo cambia.
const formatoMessaggioInesistente = "caricatore: il messaggio %s non esiste"

// messaggioInesistente: il messaggio dei casi che l'errore di Carica dice inesistente, se è uno di quelli chiesti.
func messaggioInesistente(err error, messaggi []uuid.UUID) (uuid.UUID, bool) {
	for _, m := range messaggi {
		if strings.Contains(err.Error(), fmt.Sprintf(formatoMessaggioInesistente, m)) {
			return m, true
		}
	}
	return uuid.Nil, false
}

// fotografiaExport: la sequenza -exports (6.4.9): la riga della sorgente, FotografiaDaExport con i messaggi fuori RFQ
// dei casi, i thread scelti, la riga del database non aperto e la prima scrittura del rapporto.
func (b *banco) fotografiaExport() {
	voci, err := VociExport(b.manifest)
	b.r.Sorgente = "export " + b.cartellaExport
	if err == nil {
		b.r.Sorgente += fmt.Sprintf(" (%d file, manifest sha256 %s)", len(voci), b.r.Sha256Manifest)
		for _, v := range voci {
			b.shaExport = append(b.shaExport, strings.ToLower(v.Sha256))
		}
		sort.Strings(b.shaExport)
	}
	b.riga("sorgente: %s", b.r.Sorgente)
	if err != nil {
		b.controllo("export", ControlloNonEseguito, 0, err.Error())
		return
	}
	f, mancanti, err := fotografiaDaExport(os.DirFS(b.cartellaExport), b.manifest, b.messaggiFuori())
	switch {
	case errors.Is(err, dataset.ErrFileMancante), errors.Is(err, dataset.ErrImprontaDiversa):
		b.controllo("export", ControlloNonEseguito, 0, err.Error())
		return
	case err != nil:
		b.controllo("export", ControlloEseguito, 1, err.Error())
		return
	}
	b.controllo("export", ControlloEseguito, 0, "")
	b.mancantiExport = mancanti // nel controllo dei casi: parti non verificabili (T-B6-112)
	if len(b.o.Thread) > 0 {
		scelti := map[uuid.UUID]bool{}
		for _, id := range b.o.Thread {
			scelti[id] = true
		}
		var tenuti []fotorfq.Thread
		for _, t := range f.Thread {
			if scelti[t.ID] {
				tenuti = append(tenuti, t)
				delete(scelti, t.ID)
			}
		}
		if len(scelti) > 0 {
			b.controllo("thread", ControlloNonEseguito, 0, fmt.Sprintf("%d thread scelti non sono fra quelli esportati", len(scelti)))
			return
		}
		f.Thread = tenuti
	}
	b.riga("nessun database aperto; fatti dagli export (bypass di Outlook, download e parser Python)")
	b.primaScrittura()
	if b.errScrittura != nil {
		return
	}
	b.foto, b.fotoLetta = f, true
}

// parziale: la fotografia ha solo i thread scelti, non tutti (-thread): gli attesi di altri thread non si risolvono.
func (b *banco) parziale() bool { return len(b.o.Thread) > 0 }

// valuta: il percorso comune dalla fotografia al rapporto (6.4.9, passi 6–9):
//  1. valutazione.Calcola con gli ingressi dei casi; per i thread con un caso, una seconda volta senza il caso, solo
//     per il rapporto (senza_caso; R48 A);
//  2. sempre, il file dei casi contro la fotografia (R-103) e, con gli export, i clienti dei thread (T-B6-104);
//  3. con -attesi, le sezioni tradotte e i controlli del runner 1–5, prima degli esiti;
//  4. per ogni thread la copia di passaggio.go e confronto.Confronta, con l'atteso del thread;
//  5. con -attesi la baseline, la C5, le letture dei file, i casi di contratto (EseguiCasiContratto, con
//     estrazione.DaTesto: R52 A) e il gate;
//  6. le sezioni del rapporto: thread, censimento, correzioni manuali per cliente, profilo dei limiti, prodotti.
func (b *banco) valuta() {
	f := b.foto
	b.r.Fotografia = sintesiFotografia(f)
	esito, err := valut.Calcola(f, b.ins, b.ingressi)
	if err != nil {
		b.controllo("valutazione", ControlloEseguito, 1, "errore di contratto della fotografia: "+testoDiagnostiche(err))
		return
	}
	if len(esito.Impronta) != 64 {
		b.controllo("valutazione", ControlloEseguito, 1, "l'impronta dell'esito non è calcolata")
	} else {
		b.controllo("valutazione", ControlloEseguito, 0, "")
	}
	b.r.Valutazione = sintesiValutazione(esito)
	b.r.SenzaCaso = b.senzaCaso(esito)

	ix := indicizza(f)
	cc := b.contesto(f)
	b.esito(controlloCasiNellaFoto(b.ingressi, b.casiLetti, ix, cc, b.mancantiCopia, b.mancantiExport))
	if f.Origine == fotorfq.OrigineExport {
		b.esito(controlloClientiExport(f, cc.nonValutabili))
	}
	var tr *traduzione
	if b.o.Attesi {
		if b.sezioni != nil {
			t := traduci(*b.sezioni, f, ix, b.ins)
			tr = &t
		}
		b.controlliDelRunner(&esito, tr, ix, cc)
	}

	nuovi := map[uuid.UUID]confronto.File{}
	file := map[uuid.UUID]fileDelThread{}
	badge := map[uuid.UUID]confronto.Badge{}
	revisioni := map[uuid.UUID]confronto.IndicatoreRevisione{} // per la baseline (R113 B)
	var impronte []string
	var esitiVoci []esitoVoce
	var prodottiScenario []confronto.EsitoProdottoAtteso
	nonCoperti := 0
	for i, et := range esito.Thread {
		for j := range et.File {
			file[et.File[j].AllegatoID] = fileDelThread{fi: &esito.Thread[i].File[j], valutato: et.Valutato, cliente: et.ClienteID}
		}
		righe := inFiles(et.Confrontabili)
		for _, x := range righe {
			nuovi[x.AllegatoID] = x
		}
		var atteso *confronto.Atteso
		if tr != nil {
			atteso = tr.atteso(et.ThreadID)
			if atteso == nil {
				atteso = &confronto.Atteso{ThreadID: et.ThreadID}
			}
		}
		ce := confronto.Confronta(righe, inProdotti(et.ProdottiConfrontabili), atteso)
		impronte = append(impronte, ce.Impronta)
		for _, r := range ce.File {
			if r.Badge != "" {
				badge[r.File.AllegatoID] = r.Badge
			}
			revisioni[r.File.AllegatoID] = r.Revisione
		}
		tb := b.threadBanco(et, ce, ix)
		if tr != nil {
			ev, nc := esitiDelleVoci(et, ce, *tr, &tb, cc.nonValutabili[et.ThreadID])
			esitiVoci = append(esitiVoci, ev...)
			if et.Valutato {
				nonCoperti += nc
			}
			if b.sezioni != nil && b.sezioni.Scenario != nil && b.sezioni.Scenario.ThreadID != nil && *b.sezioni.Scenario.ThreadID == et.ThreadID {
				prodottiScenario = ce.ProdottiControAtteso
			}
		}
		b.r.Thread = append(b.r.Thread, tb)
	}
	b.esito(controlloConfronto(impronte))

	if b.o.Attesi {
		var eb []esitoBaseline
		in := ingressoGate{Voci: esitiVoci, NonCoperti: nonCoperti, ProdottiScenario: prodottiScenario, Censimento: len(esito.FuoriRFQ)}
		if tr != nil {
			eb = controllaBaseline(*tr, f, ix, nuovi, revisioni, cc)
			b.esito(controlloBaselineDi(eb, true))
			b.esito(controlloInvariantiC5(b.sezioni, *tr, f, ix, cc, badge))
			b.r.RigheConfermaAlbero = righeConfermaAlbero(b.sezioni) // D-R116 I: solo informazione
			b.esito(controlloLetture(b.sezioni, *tr, file, &esito, b.ins, cc, revisioni))
			b.r.Attesi = sintesiAttesi(*b.sezioni, eb)
			in.VociNonRisolte, in.BaselineNonRisolte = vociNonRisolte(*tr)
			in.Scenario = b.sezioni.Scenario
			in.ScenarioNonVerificabile = scenarioNonVerificabile(b.sezioni.Scenario, ix, cc)
			in.ScrittureConsentite = scrittureConsentite(b.sezioni.Scenario)
		} else {
			b.esito(controlloBaselineDi(nil, false))
		}
		in.Baseline = eb
		in.CasiRiservatiNonRiservati = b.casiDiContratto()
		in.SolaLetturaOk, in.SolaLetturaMotivo = b.solaLetturaDelRunner()
		for _, t := range b.r.Thread {
			if !t.Valutato {
				in.NonValutati = append(in.NonValutati, t.ThreadID.String()+" ("+t.Motivo+")")
			}
		}
		g := calcolaGate(in)
		b.r.Gate = &g
		if b.o.Gate {
			b.esito(esitoUscitaGate(g))
		}
	}

	b.r.Censimento = censimento(esito)
	b.r.CorrezioniManuali = correzioniPerCliente(b.r.Thread)
	b.r.ProfiloLimiti = profiloLimiti(esito, b.ins)
	var scenario *ScenarioAtteso
	if b.sezioni != nil {
		scenario = b.sezioni.Scenario
	}
	b.r.Prodotti = sezioneProdotti(f, esito, scenario, tr, ix, scenarioNonVerificabile(scenario, ix, cc))
}

// contesto: ciò che i controlli devono sapere della corsa (contestoCorsa): i thread scelti, l'origine della fotografia,
// la sezione dei componenti, i thread che non si valutano per un limite degli ingressi.
func (b *banco) contesto(f fotorfq.Fotografia) contestoCorsa {
	cc := contestoCorsa{parziale: b.parziale(), export: f.Origine == fotorfq.OrigineExport,
		componentiAssenti: f.Sezioni[fotorfq.SezioneComponenti].Stato == fotorfq.StatoSezioneAssente,
		nonValutabili:     threadSenzaCliente(f)}
	if cc.parziale {
		cc.scelti = map[uuid.UUID]bool{}
		for _, id := range b.o.Thread {
			cc.scelti[id] = true
		}
	}
	return cc
}

// vociNonRisolte: le voci dello scenario e della baseline che non si risolvono o non si traducono, e quelle della sola
// baseline, per il gate.
func vociNonRisolte(tr traduzione) (scenarioEBaseline, baseline int) {
	for _, vt := range tr.voci {
		if vt.risolta {
			continue
		}
		switch vt.voce.Sezione {
		case confronto.SezioneBaseline:
			baseline++
			scenarioEBaseline++
		case confronto.SezioneScenario:
			scenarioEBaseline++
		}
	}
	return scenarioEBaseline, baseline
}

// scrittureConsentite: i percorsi delle voci dello scenario che dichiarano scritture_consentite (R-118): un invariante del
// prodotto (nessuna proiezione su un target di scenario, A2), che il runner non verifica. Lo dichiara la voce del gate
// «zero scritture di dominio», che aspetta già il registro.
func scrittureConsentite(s *ScenarioAtteso) []string {
	if s == nil {
		return nil
	}
	var out []string
	for _, v := range s.File {
		if _, ok := v.invariante("scritture_consentite"); ok {
			out = append(out, v.Percorso)
		}
	}
	return out
}

// scenarioNonVerificabile: perché la corsa non può verificare lo scenario: il suo thread non è fra quelli scelti
// (R-104) o non si valuta per un limite degli ingressi (T-B6-104). Vuoto se si verifica.
func scenarioNonVerificabile(s *ScenarioAtteso, ix indiceFoto, cc contestoCorsa) string {
	if s == nil || s.ThreadID == nil {
		return ""
	}
	if _, ok := ix.thread[*s.ThreadID]; !ok && cc.parziale {
		return "il thread dello scenario non è fra quelli scelti"
	}
	return cc.nonValutabili[*s.ThreadID]
}

// controlliDelRunner: i controlli 1–5 e la traduzione degli attesi (6.4.9), prima degli esiti.
func (b *banco) controlliDelRunner(esito *valut.Esito, tr *traduzione, ix indiceFoto, cc contestoCorsa) {
	var sc *ScenarioAtteso
	if b.sezioni != nil {
		sc = b.sezioni.Scenario
	}
	if b.sezioni == nil {
		b.controllo(ControlloScenario, ControlloNonEseguito, 0, "gli attesi non sono stati letti")
		b.controllo(ControlloRisoluzione, ControlloNonEseguito, 0, "gli attesi non sono stati letti")
	} else {
		b.esito(controlloScenario(sc, b.ingressi, b.casiLetti, esito, cc))
		b.esito(controlloRisoluzione(*b.sezioni, *tr, ix, b.foto, b.parziale()))
	}
	if b.o.Modalita == ModalitaExports {
		b.esito(controlloFonti(b.sezioni, b.shaExport))
	}
	b.esito(controlloCasiIndice(b.r.Sha256Casi, b.indice))
	b.esito(controlloAttesiInteri(b.attesiLett, b.motivoAtt, b.sezioni))
	if tr != nil {
		b.esito(controlloTraduzione(b.sezioni, *tr))
	} else {
		b.esito(controlloTraduzione(nil, traduzione{}))
	}
}

// solaLetturaDelRunner: la parte del runner della voce «zero scritture di dominio»: con -dsn i controlli del
// collegamento e la transazione del caricatore dichiarata in sola lettura e repeatable read; con -exports nessun
// database aperto.
func (b *banco) solaLetturaDelRunner() (bool, string) {
	if b.o.Modalita == ModalitaExports {
		return true, "nessun database aperto"
	}
	s := b.r.SolaLettura
	switch {
	case s == nil:
		return false, "i controlli di sola lettura non ci sono"
	case s.ScritturaPossibile:
		return false, "il collegamento può scrivere"
	case s.TransazioneSolaLettura != "on" || s.Isolamento != "repeatable read":
		return false, "la transazione del caricatore non si è dichiarata in sola lettura e repeatable read"
	}
	return true, "collegamento in sola lettura, transazione REPEATABLE READ READ ONLY"
}

// casiDiContratto: i casi_contratto degli attesi sul motore del prodotto (EseguiCasiContratto, A1a; con
// estrazione.DaTesto da A1b: R52 A), come voce dei controlli. Restituisce i casi riservati negli attesi con l'esito
// «passato» (per il gate: un riservato non è mai contato come passato).
func (b *banco) casiDiContratto() int {
	if b.attesi == nil || b.ins == nil {
		b.controllo("casi_contratto", ControlloNonEseguito, 0, "servono gli attesi e le grammatiche")
		return 0
	}
	rc := RapportoCasi{ClientiAttivi: len(b.ins.Motori), ClientiScartati: len(b.ins.Scartati)}
	rc.Casi = EseguiCasiContratto(*b.attesi, *b.ins, b.profili)
	rc.Conteggi = Conta(rc.Casi)
	b.r.Casi = &rc
	b.controllo("casi_contratto", ControlloEseguito, rc.Conteggi.Falliti, "")
	riservatiPassati := 0
	stato := map[string]string{}
	for _, c := range b.attesi.Casi {
		stato[c.ID] = c.StatoAtteso
	}
	for _, e := range rc.Casi {
		if stato[e.ID] == StatoAttesoRiservato && e.Esito == CasoPassato {
			riservatiPassati++
		}
	}
	return riservatiPassati
}

// senzaCaso: per i thread con un caso, l'esito senza gli ingressi del caso (6.4.9, passo 7; R48 A), solo per il
// rapporto. Gli ingressi restano quelli dei casi senza thread (i messaggi fuori RFQ).
func (b *banco) senzaCaso(con valut.Esito) []ThreadSenzaCaso {
	var conThread []valut.IngressoCaso
	senza := valut.Ingressi{Versione: b.ingressi.Versione}
	for _, c := range b.ingressi.Casi {
		if c.ThreadID == nil {
			senza.Casi = append(senza.Casi, c)
		} else {
			conThread = append(conThread, c)
		}
	}
	if len(conThread) == 0 {
		return nil
	}
	altro, err := valut.Calcola(b.foto, b.ins, senza)
	if err != nil {
		return nil
	}
	per := func(e valut.Esito, id uuid.UUID) (valut.EsitoThread, bool) {
		for _, t := range e.Thread {
			if t.ThreadID == id {
				return t, true
			}
		}
		return valut.EsitoThread{}, false
	}
	var out []ThreadSenzaCaso
	for _, c := range conThread {
		a, ok1 := per(con, *c.ThreadID)
		z, ok2 := per(altro, *c.ThreadID)
		if !ok1 || !ok2 {
			continue
		}
		s := ThreadSenzaCaso{ThreadID: *c.ThreadID, Caso: c.ID, ConCaso: sintesiThread(a), SenzaCaso: sintesiThread(z)}
		ja, _ := jsoncanonico.Codifica(s.ConCaso)
		jz, _ := jsoncanonico.Codifica(s.SenzaCaso)
		s.Uguale = bytes.Equal(ja, jz)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].ThreadID[:], out[j].ThreadID[:]) < 0 })
	return out
}

func sintesiThread(t valut.EsitoThread) SintesiThread {
	s := SintesiThread{Valutato: t.Valutato, Motivo: string(t.Motivo), StatoRichiesta: string(t.Richiesta.Stato),
		ProdottiValutati: len(t.ProdottiValutati)}
	for _, sg := range t.Richiesta.Segmenti {
		s.Segmenti = append(s.Segmenti, sg.SegmentoID+" ("+sg.Origine+")")
	}
	for _, c := range t.Prodotti.Candidati {
		s.Prodotti = append(s.Prodotti, c.Base.Normalizzata)
	}
	for _, f := range t.Confrontabili {
		s.Candidati += len(f.Nuovo.Candidati)
	}
	return s
}

// threadBanco: un thread nel rapporto, dall'esito di valutazione e da quello di confronto.
func (b *banco) threadBanco(et valut.EsitoThread, ce confronto.Esito, ix indiceFoto) ThreadBanco {
	tb := ThreadBanco{ThreadID: et.ThreadID, ClienteID: et.ClienteID, Valutato: et.Valutato, Motivo: string(et.Motivo),
		HashSnapshot: et.HashSnapshot, AParte: !et.Valutato, VecchiProdotti: et.VecchiProdotti,
		ProdottiNuovi: inProdotti(et.ProdottiConfrontabili), Conteggi: ce.Conteggi, Correzioni: ce.Correzioni,
		ImprontaConfronto: ce.Impronta, ProdottiControAtteso: ce.ProdottiControAtteso, Diagnostiche: et.Diagnostiche,
		DiagnosticheAncoraggi: et.Ancoraggi.Diagnostiche, DiagnosticheConfronto: ce.Diagnostiche}
	if c := b.ingressi.CasoDelThread(et.ThreadID); c != nil {
		tb.Caso = c.ID
	}
	for _, riga := range ce.File {
		fb := FileBanco{AllegatoID: riga.File.AllegatoID, Valutato: riga.File.Nuovo.Valutato, Riga: riga}
		if a, ok := ix.allegati[riga.File.AllegatoID]; ok {
			fb.NomeFile, fb.Natura = a.allegato.NomeFile, a.allegato.Natura
			if a.allegato.Sha256 != nil {
				fb.Sha256 = *a.allegato.Sha256
			}
		}
		tb.File = append(tb.File, fb)
	}
	return tb
}

// esitiDelleVoci: gli esiti contro gli attesi di un thread, legati alle voci tradotte (per allegato e sezione, nell'ordine
// delle voci), messi nelle righe dei file del rapporto con la nota, l'etichetta e le anomalie della voce; restituisce gli
// esiti per il gate e quanti file non hanno voci. nonVerificabile: perché il thread non si può verificare (T-B6-104):
// allora i suoi esiti stanno nel rapporto ma non contano nel gate.
func esitiDelleVoci(et valut.EsitoThread, ce confronto.Esito, tr traduzione, tb *ThreadBanco, nonVerificabile string) ([]esitoVoce, int) {
	type chiave struct {
		allegato uuid.UUID
		sezione  string
	}
	coda := map[chiave][]voceTradotta{}
	for _, vt := range tr.voci {
		if vt.risolta && vt.thread == et.ThreadID {
			k := chiave{vt.file.AllegatoID, vt.file.Sezione}
			coda[k] = append(coda[k], vt)
		}
	}
	righe := map[uuid.UUID]int{}
	for i, f := range tb.File {
		righe[f.AllegatoID] = i
	}
	var out []esitoVoce
	nonCoperti := 0
	for _, e := range ce.ControAtteso {
		ec := EsitoContro{Sezione: e.Sezione, Esito: e.Esito, Peso: e.Peso, DipendeDa: e.DipendeDa}
		if e.Motivo != "" {
			ec.Motivi = strings.Split(e.Motivo, ",")
		}
		if e.Esito == confronto.EsitoNonCoperto && e.Sezione == "" {
			nonCoperti++
		} else {
			k := chiave{e.AllegatoID, e.Sezione}
			if q := coda[k]; len(q) > 0 {
				vt := q[0]
				coda[k] = q[1:]
				ec.Percorso, ec.ID, ec.Riportate, ec.NonVerificabile = vt.voce.Percorso, vt.voce.ID, vt.voce.Riportate, nonVerificabile
				out = append(out, esitoVoce{Percorso: vt.voce.Percorso, Sezione: e.Sezione, AllegatoID: e.AllegatoID,
					Cliente: et.ClienteID, Atteso: vt.voce.Atteso, Radici: len(vt.voce.Radici), Esito: e.Esito,
					Riservata: vt.file.Riservato != "" || vt.file.StatoAtteso == confronto.StatoAttesoRiservato,
					DipendeDa: vt.file.DipendeDa, NonVerificabile: nonVerificabile})
			}
		}
		if i, ok := righe[e.AllegatoID]; ok {
			tb.File[i].Esiti = append(tb.File[i].Esiti, ec)
		}
	}
	return out, nonCoperti
}

// sintesiFotografia: la fotografia senza i dati, con le sue diagnostiche (D-V1-2) e, con -exports, i limiti dichiarati.
func sintesiFotografia(f fotorfq.Fotografia) *SintesiFotografia {
	s := &SintesiFotografia{Origine: f.Origine, Coerente: f.Coerente, SchemaDB: f.SchemaDB, Analizzatore: f.Analizzatore,
		Sezioni: f.Sezioni, Thread: len(f.Thread), FuoriRFQ: len(f.FuoriRFQ), Diagnostiche: f.Diagnostiche}
	if h, err := fotorfq.ImprontaFotografia(f); err == nil {
		s.Impronta = h
	}
	if f.Origine == fotorfq.OrigineExport {
		s.Limiti = []string{
			"messaggi solo in entrata e senza canale",
			"HTML solo dei corpi con una tabella: «non esportato» non vuol dire «senza HTML»",
			"fatti «ultima riga per contenuto», con le righe a un'altra terna in diagnosi",
			"motivo parziale dei fatti non determinabile: nessun «fuori richiesta» per un grafo incompleto (R32 b)",
			"proposte senza dettagli né deciso_da",
			"componenti, righe e archi proposti, candidati di codice, sigle, lavoro pendente, versioni della BOM: assenti",
			"nessun oracolo della C5 in questa modalità: le righe si confrontano con quelle dell'export",
		}
	}
	return s
}

// sintesiValutazione: l'esito senza i dati dei thread, con il conto delle diagnostiche per codice nelle tre sedi.
func sintesiValutazione(e valut.Esito) *SintesiValutazione {
	s := &SintesiValutazione{Impronta: e.Impronta, Diagnostiche: e.Diagnostiche}
	conta := map[[2]string]int{}
	aggiungi := func(dd []evidenze.Diagnostica) {
		for _, d := range dd {
			conta[[2]string{d.Codice, string(d.Gravita)}]++
		}
	}
	aggiungi(e.Diagnostiche)
	for _, t := range e.Thread {
		if t.Valutato {
			s.ThreadValutati++
		} else {
			s.ThreadNonValutati++
		}
		aggiungi(t.Diagnostiche)
		aggiungi(t.Ancoraggi.Diagnostiche)
	}
	for _, m := range e.FuoriRFQ {
		aggiungi(m.Diagnostiche)
	}
	for k, n := range conta {
		s.PerCodice = append(s.PerCodice, ConteggioCodice{Codice: k[0], Gravita: k[1], N: n})
	}
	sort.Slice(s.PerCodice, func(i, j int) bool {
		if s.PerCodice[i].Codice != s.PerCodice[j].Codice {
			return s.PerCodice[i].Codice < s.PerCodice[j].Codice
		}
		return s.PerCodice[i].Gravita < s.PerCodice[j].Gravita
	})
	return s
}

// sintesiAttesi: quante voci per sezione, le chiavi non tradotte, i valori non letti, le sezioni descrittive, la
// baseline.
func sintesiAttesi(s SezioniAttesi, eb []esitoBaseline) *SintesiAttesi {
	out := &SintesiAttesi{NonTradotte: s.NonTradotte, Errori: s.Errori, Riportate: s.Riportate, Baseline: eb, ChiaviLibere: s.ChiaviLibere}
	// L'id delle voci accanto al percorso (R-119): il percorso è quello dei dettagli dei controlli e degli esiti.
	var voci []VoceAttesa
	if s.Scenario != nil {
		voci = append(voci, s.Scenario.File...)
	}
	voci = append(append(append(voci, s.Baseline...), s.Reali...), s.DaRivedere...)
	for _, v := range voci {
		if v.ID != "" {
			out.IDVoci = append(out.IDVoci, IDVoce{Percorso: v.Percorso, ID: v.ID})
		}
	}
	if s.Scenario != nil {
		out.Voci = append(out.Voci, ConteggioSezione{Sezione: "scenario_file", Voci: len(s.Scenario.File)},
			ConteggioSezione{Sezione: "scenario_prodotti", Voci: len(s.Scenario.Prodotti)})
	}
	out.Voci = append(out.Voci,
		ConteggioSezione{Sezione: "baseline", Voci: len(s.Baseline)},
		ConteggioSezione{Sezione: "reali", Voci: len(s.Reali)},
		ConteggioSezione{Sezione: "da_rivedere", Voci: len(s.DaRivedere)},
		ConteggioSezione{Sezione: "albero_id", Voci: len(s.Albero)},
		ConteggioSezione{Sezione: "integrazione_id", Voci: len(s.Integrazione)},
		ConteggioSezione{Sezione: "fonti", Voci: len(s.Fonti)})
	return out
}

// censimento: i messaggi fuori RFQ dei casi di censimento, con le letture del motore forma per forma (R34 c).
func censimento(e valut.Esito) []VoceCensimento {
	var out []VoceCensimento
	for _, m := range e.FuoriRFQ {
		v := VoceCensimento{Caso: m.Caso, MessaggioID: m.MessaggioID, ClienteID: m.ClienteID, Valutato: m.Valutato,
			Motivo: string(m.Motivo), File: len(m.File), Prodotti: len(m.Prodotti.Candidati)}
		conta := map[[3]string]int{}
		for _, l := range m.Messaggio.Interpretazione.Letture {
			conta[[3]string{l.Forma.Famiglia, l.Forma.Forma, string(l.Funzione)}]++
			if funzioneDIdentita(l.Funzione) {
				v.LettureIdentita++
			}
		}
		for k, n := range conta {
			v.Letture = append(v.Letture, ConteggioForma{Famiglia: k[0], Forma: k[1], Funzione: k[2], N: n})
		}
		sort.Slice(v.Letture, func(i, j int) bool {
			a, c := v.Letture[i], v.Letture[j]
			if a.Famiglia != c.Famiglia {
				return a.Famiglia < c.Famiglia
			}
			if a.Forma != c.Forma {
				return a.Forma < c.Forma
			}
			return a.Funzione < c.Funzione
		})
		out = append(out, v)
	}
	return out
}

// correzioniPerCliente: le correzioni manuali per cliente, sui thread valutati; i thread non valutati a parte (D5). La
// misura di ogni thread è quella di confronto (R114, precisata dall'utente il 07/10: lo stesso campione, il
// denominatore, gli esclusi per motivo, «dopo» in tre parti), sommata campo per campo con Aggiungi (T-B6-186): il banco
// non la ricalcola. Fuori dalla misura il banco conta solo due cose, sui file dei thread valutati:
//   - RevisioneSoloInColonna: i file con la revisione vecchia solo nella colonna rev (R-65), divisi per lo stato della
//     lettura della colonna con la regola della famiglia (R113 B ratificata; motorea.LeggiRevisioneRegistrata):
//     RevisioneInColonna.conta;
//   - StessaBaseAltroTarget: i decisi con il badge regressione_su_confermata e il motivo stessa_base_altro_target
//     (T-B6-51), su tutti i decisi e non sul campione dei valutabili: non è una parte di Dopo (T-B6-191).
func correzioniPerCliente(thread []ThreadBanco) []CorrezioniCliente {
	per := map[uuid.UUID]*CorrezioniCliente{}
	var ordine []uuid.UUID
	for _, t := range thread {
		c := per[t.ClienteID]
		if c == nil {
			c = &CorrezioniCliente{ClienteID: t.ClienteID}
			per[t.ClienteID] = c
			ordine = append(ordine, t.ClienteID)
		}
		if !t.Valutato {
			c.ThreadNonValutati++
			c.CorrezioniNonValutati.Aggiungi(t.Correzioni)
			continue
		}
		c.Thread++
		c.Correzioni.Aggiungi(t.Correzioni)
		for _, f := range t.File {
			v := f.Riga.File.Vecchio
			c.RevisioneSoloInColonna.conta(f.Riga.File)
			if v.Documento == nil { // deciso = un documento confermato porta il file (T-B6-52)
				continue
			}
			if f.Riga.Badge == confronto.BadgeRegressioneSuConfermata && f.Riga.Motivo == confronto.MotivoStessaBaseAltroTarget {
				c.StessaBaseAltroTarget++
			}
		}
	}
	sort.Slice(ordine, func(i, j int) bool { return bytes.Compare(ordine[i][:], ordine[j][:]) < 0 })
	out := make([]CorrezioniCliente, 0, len(ordine))
	for _, id := range ordine {
		out = append(out, *per[id])
	}
	return out
}

// profiloLimiti: i massimi osservati nei documenti dell'esito (i file dei thread, i messaggi fuori RFQ e i loro file:
// l'esito non porta i documenti dei messaggi dei thread), contro i limiti dell'indice e i tetti del codice (R43 B).
func profiloLimiti(e valut.Esito, ins *motorea.InsiemeRegole) *ProfiloLimiti {
	p := &ProfiloLimiti{VersioneLimiti: e.VersioneLimiti, Tetti: grammatica.TettiLimiti().Riconoscimento}
	if ins != nil {
		p.Indice = ins.Indice.Limiti.Riconoscimento
	}
	documento := func(d evidenze.DocumentoEvidenze, letture []motorea.LetturaCodice) {
		if d.BundleID == "" && len(d.Unita) == 0 {
			return
		}
		p.Documenti++
		o := &p.Osservati
		o.MaxUnitaDocumento = max(o.MaxUnitaDocumento, len(d.Unita))
		o.MaxLettureDocumento = max(o.MaxLettureDocumento, len(letture))
		for _, u := range d.Unita {
			o.MaxByteUnita = max(o.MaxByteUnita, len(u.Testo))
		}
		perUnita := map[string]int{}
		for _, l := range letture {
			perUnita[l.UnitaID]++
		}
		for _, n := range perUnita {
			o.MaxLetturePerUnita = max(o.MaxLetturePerUnita, n)
		}
	}
	for _, t := range e.Thread {
		for _, f := range t.File {
			documento(f.Documento, f.Interpretazione.Letture)
		}
	}
	for _, m := range e.FuoriRFQ {
		documento(m.Messaggio.Documento, m.Messaggio.Interpretazione.Letture)
		for _, f := range m.File {
			documento(f.Documento, f.Interpretazione.Letture)
		}
	}
	if ins != nil {
		o, l := p.Osservati, p.Indice
		for _, x := range []struct {
			nome     string
			oss, lim int
		}{
			{"max_byte_unita", o.MaxByteUnita, l.MaxByteUnita},
			{"max_unita_documento", o.MaxUnitaDocumento, l.MaxUnitaDocumento},
			{"max_letture_per_unita", o.MaxLetturePerUnita, l.MaxLetturePerUnita},
			{"max_letture_documento", o.MaxLettureDocumento, l.MaxLettureDocumento},
		} {
			if x.oss > x.lim {
				p.OltreIndice = append(p.OltreIndice, x.nome)
			}
		}
	}
	return p
}

// ---- le copie dei campi, per passaggio.go ----

func copiaUUID(u *uuid.UUID) *uuid.UUID {
	if u == nil {
		return nil
	}
	v := *u
	return &v
}

func copiaTempo(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

func copiaStringhe(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string{}, s...)
}
