// I servizi di lungo periodo: chi li costruisce, con che cosa, e quando partono.

package runtime

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"promatec/cockpit/internal/ai/agente"
	"promatec/cockpit/internal/core/inbox/ingest"
	"promatec/cockpit/internal/core/rfq/documenti"
	"promatec/cockpit/internal/platform/coda"
	"promatec/cockpit/internal/platform/config"
	"promatec/cockpit/internal/platform/db"
	"promatec/cockpit/internal/platform/storage/nas"
	"promatec/cockpit/internal/platform/storage/staging"
	"promatec/cockpit/internal/transport/web"
)

// Servizi sono i pezzi di lungo periodo del processo, gia' costruiti e collegati fra loro.
//
// Scheduler e Cache partono dentro CostruisciServizi, dove partivano prima: la Cache scrive una riga
// nel log quando il custode si avvia, e spostarla dopo la preparazione del TLS avrebbe cambiato
// l'ordine del log d'avvio senza che nessuno l'avesse chiesto. Esecutore e Ricognitore partono in
// `Ascolta` attraverso Avvia: all'esecutore serve chi sa scompattare un archivio, e quel qualcuno e'
// il server dei worker, che nasce li'.
type Servizi struct {
	// Pool e' il database che tutti condividono: sta qui perche' chi monta le rotte lo chiede a questo
	// insieme di pezzi, non a un parametro in piu' che verrebbe da un'altra parte.
	Pool *pgxpool.Pool

	Scrittore   *nas.Scrittore
	Ingest      *ingest.Servizio
	Agente      *agente.Servizio
	Esecutore   *EsecutoreServer
	Ricognitore *documenti.Ricognitore

	// RadiceStaging, OpzioniSync, IntervalloSync e SyncApertura servono a chi monta le rotte: la
	// schermata dice all'operatore ogni quanto si sincronizza, e «Aggiorna ora» accoda esattamente il
	// job che accoderebbe lo scheduler.
	RadiceStaging  string
	OpzioniSync    coda.SyncOpzioni
	IntervalloSync time.Duration
	SyncApertura   bool

	// MotoreA: la Distinta nuova del motore A, preparata all'avvio da [motore_a] (giro 5, A1d; piano 7.4.5).
	MotoreA web.AnteprimaMotoreA
}

// CostruisciServizi mette insieme i pezzi: lo scrittore del NAS, lo scheduler, la cache dei
// contenuti, l'agente, l'esecutore dei job del server, il ricognitore dell'integrita' e l'ingest.
func CostruisciServizi(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, q *db.Queries, log *slog.Logger, capacita coda.Capacita) (*Servizi, error) {
	// DryRun non viene piu' dal file: e' l'altra faccia di [sicurezza].nas_scrittura. Due voci per la
	// stessa decisione sono due voci che prima o poi si contraddicono, e la piu' silenziosa vince.
	scrittore := &nas.Scrittore{Radice: cfg.NAS.Radice, DryRun: !capacita.NasScrittura}
	if scrittore.Raggiungibile() {
		log.Info("NAS raggiungibile", "radice", cfg.NAS.Radice, "scrittura", capacita.NasScrittura)
	} else {
		log.Warn("NAS non raggiungibile: le copie resteranno in coda", "radice", cfg.NAS.Radice)
	}

	// `dal` è un override e basta: senza, il limite inferiore lo decide chi accoda il sync (il
	// cursore della cartella, altrimenti la finestra iniziale) e lo decide AL MOMENTO. Calcolarlo qui
	// una volta sola voleva dire che un server acceso da un mese apriva ancora la finestra di un mese
	// fa. La data è già stata validata da config.Carica: qui non può più fallire.
	var dal time.Time
	if cfg.Outlook.Dal != "" {
		dal, _ = config.DataDal(cfg.Outlook.Dal)
		log.Warn("finestra iniziale forzata da [outlook].dal: vale solo per le cartelle senza cursore", "dal", cfg.Outlook.Dal)
	}
	radiceStaging, _ := filepath.Abs(cfg.NAS.Staging)
	// Le stesse opzioni per lo scheduler e per «Aggiorna ora»: due sync della stessa casella con
	// cartelle diverse farebbero avanzare il cursore su una finestra che l'altro non ha letto.
	opzioniSync := coda.SyncOpzioni{Cartelle: cfg.Outlook.Cartelle, Dal: dal,
		GiorniIniziali: cfg.Outlook.GiorniSyncIniziale, Lotto: cfg.Outlook.Lotto}
	intervalloSync := time.Duration(cfg.Outlook.IntervalloSyncS) * time.Second
	// Assente = acceso. Il sync di apertura non è il sync periodico: `intervallo_sync_s = 0` dice che
	// il server non accoda niente DA SOLO, non che debba ignorare chi sta aprendo l'Inbox adesso.
	syncApertura := cfg.Outlook.SyncAperturaInbox == nil || *cfg.Outlook.SyncAperturaInbox
	if intervalloSync <= 0 {
		log.Warn("sincronizzazione periodica disattivata: nessun sync viene accodato a tempo finché intervallo_sync_s resta 0",
			"sync_apertura_inbox", syncApertura)
	}
	(&coda.Scheduler{
		Q: q, Log: log, Cartelle: opzioniSync.Cartelle,
		IntervalloSync: intervalloSync,
		Dal:            opzioniSync.Dal, GiorniIniziali: opzioniSync.GiorniIniziali, Lotto: opzioniSync.Lotto,
		RetentionGiorni: cfg.Retention.GiorniJob,
		Staging:         radiceStaging,
	}).Avvia(ctx)
	// La cache dei contenuti (Pre-7, D31): `_contenuti` resta dopo la copia sul NAS, e si svuota per
	// eta' o per capienza, mai sotto a chi la sta usando.
	if cfg.Retention.GiorniStaging > 0 && cfg.Retention.CacheGiorni == nil {
		log.Warn("[retention].giorni_staging e' deprecata: vale come cache_gg", "giorni", cfg.Retention.GiorniStaging,
			"nota", "scrivere cache_gg = "+strconv.Itoa(cfg.Retention.GiorniStaging)+" e togliere la riga")
	}
	(&staging.Cache{Pool: pool, Staging: radiceStaging, Log: log, Retention: cfg.RetentionCache(), MaxByte: cfg.CacheMaxByte()}).Avvia(ctx)
	// L'analisi semantica (checkpoint 3R §9). Spenta se non la si accende in [agente], e comunque
	// spenta se manca la chiave: `DaAmbiente` restituisce nil, e un servizio senza modello non chiama
	// nessuno. Il testo delle mail dei clienti esce verso un servizio esterno, e quella e' una cosa
	// che si mette per iscritto prima.
	servizioAgente := &agente.Servizio{
		Attivo:  cfg.Agente.Attivo,
		Modello: agente.DaAmbiente(cfg.Agente.URL, cfg.Agente.Modello, cfg.Agente.ChiaveEnv),
		Caselle: map[string]bool{},
	}
	for _, c := range cfg.Agente.Caselle {
		servizioAgente.Caselle[strings.ToLower(strings.TrimSpace(c))] = true
	}
	if cfg.Agente.Attivo && servizioAgente.Modello == nil {
		log.Warn("analisi semantica richiesta ma spenta: manca la chiave", "variabile", cfg.Agente.ChiaveEnv)
	}
	if servizioAgente.Attivo && servizioAgente.Modello != nil {
		log.Info("analisi semantica attiva", "modello", servizioAgente.Modello.Nome(), "caselle", len(servizioAgente.Caselle))
	}
	// L'anteprima del motore A (giro 5, A1d): spenta non apre file; accesa legge l'indice delle regole una volta, qui,
	// e un indice rotto non ferma l'avvio (lo dice il log, e la pagina mostra l'errore).
	motoreA := preparaMotoreA(cfg.MotoreA, log)
	esecutore := &EsecutoreServer{Pool: pool, NAS: scrittore, Log: log, Agente: servizioAgente}
	ricognitore := &documenti.Ricognitore{Pool: pool, NAS: scrittore, Log: log, Ogni: cfg.IntervalloIntegrita()}
	// D30: lo staging automatico si accende dal file di configurazione ([staging] automatico = true) e
	// non dal binario. Acceso, gli allegati di un mittente riconosciuto scendono da soli e l'analisi
	// puo' dire che cosa sono; spento, si comporta come prima del checkpoint 3R.
	servizioIngest := &ingest.Servizio{Pool: pool, Log: log,
		StagingAutomatico: cfg.Staging.Automatico,
		StagingBootstrap:  cfg.Staging.Bootstrap,
		StagingMaxByte:    int64(cfg.Staging.MaxMB) * 1024 * 1024}
	return &Servizi{Pool: pool, Scrittore: scrittore, Ingest: servizioIngest, Agente: servizioAgente,
		Esecutore: esecutore, Ricognitore: ricognitore,
		RadiceStaging: radiceStaging, OpzioniSync: opzioniSync, IntervalloSync: intervalloSync,
		SyncApertura: syncApertura, MotoreA: motoreA}, nil
}

// Avvia fa partire i due che aspettavano il server dei worker: l'esecutore dei job del server e il
// ricognitore dell'integrita'.
func (s *Servizi) Avvia(ctx context.Context) {
	s.Esecutore.Avvia(ctx)
	// Il ricognitore dell'integrita' (blocco 5B). Parte SEMPRE, anche con la scrittura spenta: legge
	// il NAS e basta, e un server che non scrive puo' benissimo accorgersi che un file dichiarato nel
	// fascicolo non c'e' piu'. Si spegne solo con [nas].intervallo_integrita_s = 0.
	s.Ricognitore.Avvia(ctx)
}
