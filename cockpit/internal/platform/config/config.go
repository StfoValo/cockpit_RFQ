// Package config legge cockpit.toml (accanto all'exe o passato con -config).
package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"promatec/cockpit/internal/platform/db"
)

type Config struct {
	Server     Server       `toml:"server"`
	DB         DB           `toml:"db"`
	NAS        NAS          `toml:"nas"`
	Outlook    Outlook      `toml:"outlook"`
	Utenti     []Utente     `toml:"utenti"`
	Caselle    []Casella    `toml:"casella"`    // [[casella]] — fase 0, voce 0.6
	Postazioni []Postazione `toml:"postazione"` // [[postazione]]
	Worker     []Worker     `toml:"worker"`     // [[worker]] — credenziali individuali
	Retention  Retention    `toml:"retention"`  // [retention] — fase 1, voce 1.6
	Analisi    Analisi      `toml:"analisi"`    // [analisi] — fase 1, voce 1.12
	Staging    Staging      `toml:"staging"`    // [staging] — checkpoint 3R, D30
	Agente     Agente       `toml:"agente"`     // [agente] — checkpoint 3R §9
	Sicurezza  Sicurezza    `toml:"sicurezza"`  // [sicurezza] — checkpoint 3R, blocco 4
	MotoreA    MotoreA      `toml:"motore_a"`   // [motore_a] — giro 5, A1d: la Distinta nuova, solo admin

	// Avvisi sono le cose del file che non fermano l'avvio ma vanno dette nel log: le voci che non
	// esistono (un refuso, che altrimenti vale come se la riga non ci fosse) e quelle che si leggono ma
	// non hanno effetto. Non sono errori: un file scritto per un binario di prima deve continuare a
	// partire.
	Avvisi []string `toml:"-"`
}

// Sicurezza: che cosa questo server e' autorizzato a MODIFICARE fuori da se' (blocco 4).
//
// Prima c'era `[server].modalita`, che era un interruttore solo: o il Cockpit non toccava niente, o
// toccava tutto. Serviva a far girare il sistema sulla posta vera senza che un difetto diventasse una
// mail modificata, ed e' stata la scelta giusta finche' l'unica domanda era «possiamo fidarci?». Ma
// per provare la copia sul NAS di prova quella domanda si sdoppia: si vuole scrivere un file in una
// cartella di prova, e NON si vuole che una mail vera diventi letta o si sposti. Con un interruttore
// solo quelle due cose sono la stessa cosa.
//
// Sono puntatori perche' l'assenza e il `false` devono restare distinguibili: un file scritto prima
// che questa sezione esistesse non contiene le righe, e il server deve poterlo dire — «questo file
// non dichiara niente, quindi non scrivo niente» — invece di comportarsi come se qualcuno avesse
// scelto. Vedi Capacita().
type Sicurezza struct {
	// ConsentiNasProduzione e' il SECONDO consenso, e serve solo quando i due precedenti insieme
	// varrebbero «scrivi nel fascicolo vero di un cliente»: `nas_scrittura = true` con `[nas].radice`
	// dentro una delle `radici_produzione` dichiarate. Senza, il server non parte e dice quale radice
	// ha riconosciuto.
	//
	// Non e' una conferma per il gusto di chiederla due volte. Il giorno in cui questa riga passera'
	// da `_nas_test` al percorso aziendale — copiando un file, tornando da una prova, cambiando una
	// riga per sbaglio — la scrittura sul NAS vero comincerebbe senza che nessuno l'abbia decisa in
	// quel momento: la radice e la capacita' sono due voci lontane fra loro, e ognuna delle due presa
	// da sola sembra innocua. Questa terza le lega.
	ConsentiNasProduzione *bool `toml:"consenti_nas_produzione"`
	OutlookScrittura      *bool `toml:"outlook_scrittura"` // segna letto, sposta in cartella
	Bozze                 *bool `toml:"bozze"`             // crea_bozza_outlook
	NasScrittura          *bool `toml:"nas_scrittura"`     // crea_cartella_thread, copia_nas
}

// Capacita sono le tre capacita' RISOLTE, piu' le frasi da scrivere nel log di avvio.
type Capacita struct {
	OutlookScrittura bool
	Bozze            bool
	NasScrittura     bool
	// Avvisi: che cosa e' stato deciso e perche', quando la decisione non e' semplicemente «c'e'
	// scritto nel file». Vanno nel log di avvio: una capacita' spenta per un motivo che nessuno vede
	// diventa un sistema che «non funziona», e si passa un pomeriggio a cercare il difetto.
	Avvisi []string
}

// Capacita risolve [sicurezza] in cio' che questo server puo' davvero fare.
//
// Tre regole, in quest'ordine:
//
//  1. SHADOW E' UN PRESET. `modalita = "shadow"` spegne tutto, qualunque cosa dica [sicurezza]. Chi
//     scrive «shadow» sta dicendo «questo server non tocca niente», e quella frase non deve poter
//     essere contraddetta tre righe piu' sotto.
//  2. IL SILENZIO VALE «NON SCRIVERE». Una voce assente e' spenta. In particolare un file che dice
//     `modalita = "produzione"` e non ha la sezione [sicurezza] NON accende niente: «produzione» non
//     deve significare automaticamente «accendi tutto», perche' quel file l'ha scritto qualcuno che
//     non sapeva che queste tre voci esistessero. Il server lo dice nel log, con la riga da aggiungere.
//  3. `[nas].dry_run = true` SPEGNE `nas_scrittura`. E' la compatibilita' con i file scritti prima:
//     quella voce significava esattamente «non scrivere sul NAS», e continua a significarlo. E'
//     DEPRECATA — a regime la stessa cosa si dice con `nas_scrittura = false` — e il server lo scrive
//     nel log ogni volta che la trova.
func (c *Config) Capacita() Capacita {
	cap := Capacita{}
	if c.EShadow() {
		cap.Avvisi = append(cap.Avvisi, "[server].modalita = shadow: tutte le capacita' di scrittura sono spente (preset). "+
			"Per accenderne una servono [sicurezza] e modalita = \"produzione\"")
		if vuole(c.Sicurezza.OutlookScrittura) || vuole(c.Sicurezza.Bozze) || vuole(c.Sicurezza.NasScrittura) {
			cap.Avvisi = append(cap.Avvisi, "[sicurezza] chiede capacita' che la modalita' shadow spegne: in shadow il Cockpit legge il mondo e non lo tocca")
		}
		return cap
	}
	if c.Sicurezza.OutlookScrittura == nil && c.Sicurezza.Bozze == nil && c.Sicurezza.NasScrittura == nil {
		cap.Avvisi = append(cap.Avvisi, "nessuna sezione [sicurezza] nel file: questo server NON modifica niente fuori da se'. "+
			"«produzione» non accende piu' tutto da sola: aggiungere [sicurezza] con le voci che servono (outlook_scrittura, bozze, nas_scrittura)")
		return cap
	}
	cap.OutlookScrittura, cap.Bozze, cap.NasScrittura = vuole(c.Sicurezza.OutlookScrittura), vuole(c.Sicurezza.Bozze), vuole(c.Sicurezza.NasScrittura)
	if c.NAS.DryRun {
		if cap.NasScrittura {
			cap.Avvisi = append(cap.Avvisi, "[nas].dry_run = true spegne [sicurezza].nas_scrittura: la voce e' DEPRECATA e va tolta, "+
				"la stessa cosa si dice con nas_scrittura = false")
		} else {
			cap.Avvisi = append(cap.Avvisi, "[nas].dry_run e' DEPRECATA e non serve piu': la scrittura sul NAS la governa [sicurezza].nas_scrittura")
		}
		cap.NasScrittura = false
	}
	if cap.NasScrittura {
		if r := c.RadiceDiProduzione(); r != "" {
			cap.Avvisi = append(cap.Avvisi, "questo server SCRIVE SUL NAS VERO: [nas].radice e' sotto la radice di produzione "+r+
				" dichiarata in [nas].radici_produzione, e [sicurezza].consenti_nas_produzione = true lo consente")
		}
	}
	return cap
}

// vuole risolve un puntatore assente in «no».
func vuole(b *bool) bool { return b != nil && *b }

// MotoreA è [motore_a] (giro 5, A1d; piano 7.4.1, 7.6.1; contratto A1d §2.1, §5): la Distinta nuova del motore A, la
// pagina /thread/<id>/motore-a. Due voci sole (R43 B): il flag e l'indice delle regole. Spenta se la sezione manca o se
// anteprima = false: l'avvio non legge nessun file e la pagina risponde 404 a tutti. Accesa, la vede solo il ruolo
// admin (R11). Le soglie del motore non sono voci di questa sezione: le dichiara l'indice (versione dei limiti e valori,
// compreso il tempo massimo di un calcolo della pagina), e il codice tiene solo i tetti.
type MotoreA struct {
	Anteprima bool   `toml:"anteprima"` // assente = false
	Regole    string `toml:"regole"`    // l'indice di grammatiche, casi e limiti; relativo alla cartella di questo file
}

// Agente: l'analisi semantica dei messaggi (checkpoint 3R §9).
//
// Spenta se non la si accende qui, e comunque solo sulle caselle elencate. Il motivo non e' tecnico:
// il testo delle mail dei clienti esce verso un servizio esterno, e questa e' una cosa da mettere per
// iscritto con l'IT e con chi segue la ISO 27001 — per casella, con la possibilita' di spegnerla —
// prima che parta la prima chiamata, non dopo.
//
// La CHIAVE non sta qui: `chiave_env` e' il NOME della variabile d'ambiente che la contiene. Un file
// di configurazione finisce nei backup e ogni tanto in un repository; una variabile d'ambiente no.
//
// Che cosa esce, nella prima versione: oggetto, corpo, nomi degli allegati, ragione sociale del
// cliente e i candidati gia' calcolati. Mai il contenuto dei file.
type Agente struct {
	Attivo    bool     `toml:"attivo"`
	Modello   string   `toml:"modello"`    // es. "claude-sonnet-5"
	URL       string   `toml:"url"`        // vuoto = quello predefinito del fornitore
	ChiaveEnv string   `toml:"chiave_env"` // NOME della variabile d'ambiente, non la chiave
	Caselle   []string `toml:"caselle"`    // indirizzi su cui l'analisi e' permessa; vuoto = nessuna
}

// Staging: se gli allegati di un cliente riconosciuto scendono da soli nello staging del server (D30).
//
// Perché la domanda esiste. Il tipo di un PDF si sa solo aprendolo: cartiglio, termini, numero di
// pagine. Ma la regola «nessun download automatico» (D24) faceva partire l'analisi solo dopo un clic
// su «Scarica», e fino a quel clic l'operatore vedeva «PDF · da determinare» su ogni allegato di ogni
// messaggio — cioè doveva scaricare per sapere se valeva la pena scaricare.
//
// Con `automatico = true` gli allegati di un mittente riconosciuto, sotto `max_mb`, arrivano nello
// staging appena il messaggio entra, e l'operatore li trova già classificati. Lo staging è una
// cartella del server: la regola vera — «niente sul NAS senza una decisione» — riguarda il NAS, e il
// NAS non lo tocca nessuno da qui.
//
// Il valore predefinito è FALSO: acceso, questo fa partire lavoro su Outlook senza che nessuno abbia
// premuto niente, e una cosa del genere si accende scrivendola nel file di configurazione, non
// perché è il default di un binario.
//
// Vale per l'AGGIORNAMENTO ordinario. Le altre due sincronizzazioni non sono la stessa cosa:
//
//	bootstrap   la prima volta di una casella porta dentro settimane di posta in un colpo solo:
//	            `bootstrap = true` e' la dichiarazione esplicita di volerne anche gli allegati;
//	storico     «Carica precedenti» non scarica mai niente da solo, e non c'e' una voce per
//	            cambiarlo: serve a rendere consultabile la posta vecchia.
type Staging struct {
	Automatico bool `toml:"automatico"`
	Bootstrap  bool `toml:"bootstrap"` // anche alla prima sincronizzazione di una casella; assente = false
	MaxMB      int  `toml:"max_mb"`    // 0 = la soglia predefinita (20 MB)
}

// Retention: per quanto si tengono i job chiusi e i contenuti nella cache dello staging.
//
// Le voci non si somigliano, per quanto stiano vicine. `giorni_job` toglie righe di coda gia'
// chiuse: al peggio si perde una diagnosi. `cache_gg` e `cache_max_mb` tolgono FILE dalla cache
// `_contenuti` (Pre-7, D31): un contenuto che nessuno sta usando adesso — nessuna copia in attesa,
// nessuna proposta aperta, nessun job, nessuna anomalia NAS — e che da `cache_gg` giorni non tocca
// nessuno, oppure il meno usato quando la cache supera `cache_max_mb`. Un file tolto si riprende
// da Outlook, o dall'archivio da cui era stato estratto: per questo la cache si puo' svuotare, e
// per questo si svuota per ultima e con giudizio.
//
// `giorni_staging` e' la voce di prima del blocco 7 e vale come `cache_gg` se `cache_gg` manca:
// chi l'aveva scritta aveva gia' deciso quanto tenere i file, e non deve riscriverlo.
type Retention struct {
	GiorniJob     int  `toml:"giorni_job"`
	GiorniStaging int  `toml:"giorni_staging"` // DEPRECATA: vale come cache_gg se cache_gg manca
	CacheGiorni   *int `toml:"cache_gg"`       // assente = 30; 0 = mai per eta' (resta la capienza)
	CacheMaxMB    int  `toml:"cache_max_mb"`   // 0 = nessun limite di capienza
}

// RetentionCache: da quanti giorni un contenuto deve essere fermo perche' la cache lo tolga. Zero
// = non togliere niente per eta'. Assente nel file = trenta giorni.
func (c *Config) RetentionCache() time.Duration {
	giorni := 30
	switch {
	case c.Retention.CacheGiorni != nil:
		giorni = *c.Retention.CacheGiorni
	case c.Retention.GiorniStaging > 0:
		giorni = c.Retention.GiorniStaging
	}
	if giorni < 0 {
		giorni = 0
	}
	return time.Duration(giorni) * 24 * time.Hour
}

// CacheMaxByte: oltre quanti byte la cache toglie i contenuti meno usati anche se non sono ancora
// vecchi. Zero = nessun limite.
func (c *Config) CacheMaxByte() int64 {
	if c.Retention.CacheMaxMB <= 0 {
		return 0
	}
	return int64(c.Retention.CacheMaxMB) << 20
}

// Analisi identifica CON CHE COSA un file è stato analizzato (voce 1.12, A15).
//
// I fatti estratti da un file dipendono da tre cose e solo da tre: il contenuto del file, la versione
// dell'analizzatore e la configurazione che gli è stata data. A parità di tutte e tre, rifare
// l'analisi è lavoro sprecato e il risultato è per definizione lo stesso; se cambia una delle tre, il
// risultato precedente non vale più. Senza queste due informazioni il server non può distinguere i
// due casi, e finisce per fare sempre la scelta sbagliata: o rianalizza tutto ogni volta, o riusa
// fatti calcolati con un dizionario che non è più quello.
//
// Parametri è il blocco che viene mandato al worker; il suo sha256, insieme alla versione, è la
// chiave con cui i fatti vengono conservati e ritrovati. Cambiare un termine qui basta a far
// rianalizzare tutto, senza toccare il codice.
type Analisi struct {
	Versione  int            `toml:"versione"`
	Parametri map[string]any `toml:"parametri"`
}

type Server struct {
	Indirizzo string `toml:"indirizzo"` // es. "127.0.0.1:8080" in sviluppo, "0.0.0.0:8443" in LAN
	// TokenWorker era il token CONDIVISO da tutti i worker. Dalla voce 2.4 non autentica più niente:
	// la credenziale è individuale ([[worker]].token, sha256 in `worker_credenziale`). Resta letto
	// per un motivo solo — dirlo a chi ce l'ha ancora nel file, invece di lasciarlo credere che
	// protegga qualcosa.
	TokenWorker string `toml:"token_worker"`
	// SegretoSessione non ha effetto: le sessioni vivono nel database, e nessun cookie si firma con
	// questo. Resta letto per dirlo nel log (Avvisi) a chi ce l'ha nel file.
	SegretoSessione string `toml:"segreto_sessione"`
	LogLivello      string `toml:"log_livello"` // debug | info | warn | error (assente = info)
	// TLSCert e TLSKey: i due file PEM del listener (voce 2.4). Vuoti = niente TLS, e allora il
	// server accetta solo di ascoltare su loopback. Percorsi relativi al file di configurazione.
	//
	// Se i due file non esistono il server ne genera uno **autofirmato** e li scrive: su una LAN
	// aziendale senza CA interna un certificato autofirmato con l'impronta dichiarata nel worker.toml
	// è più forte di una catena che nessuno verifica, e infinitamente più forte del testo in chiaro.
	// L'impronta sha256 viene scritta nel log all'avvio e mostrata nella pagina *Postazioni*.
	TLSCert string `toml:"tls_cert"`
	TLSKey  string `toml:"tls_key"`
	// TLSNomi sono i nomi e gli indirizzi per cui vale il certificato generato (SAN): il nome host
	// della VM, il suo IP, gli alias con cui i worker la chiamano. Vuoto = nome host del server e
	// l'indirizzo su cui ascolta, se è un IP.
	TLSNomi []string `toml:"tls_nomi"`
	// ConsentiLanInChiaro è la via d'uscita dichiarata, non il default: senza di lei il server si
	// rifiuta di ascoltare fuori da loopback senza TLS. Un server in chiaro sulla LAN espone la posta
	// dell'azienda e i token dei worker a chiunque sia attaccato allo stesso switch, e non se ne
	// accorge nessuno: è esattamente il tipo di difetto che non dà errore.
	ConsentiLanInChiaro bool `toml:"consenti_lan_in_chiaro"`
	// LogFile: dove il server scrive il proprio log, oltre che sullo stdout della finestra.
	// Vuoto = <nas.staging>\log\cockpit.log; "-" = solo stdout, nessun file. Un percorso relativo si
	// legge dalla cartella del file di configurazione.
	LogFile string `toml:"log_file"`
	// Modalita è come gira il server: "shadow" oppure "produzione" (§2.7, D16, voce 9.5).
	//
	// In shadow il Cockpit LEGGE il mondo e non lo tocca: niente bozze, niente «segna letto», niente
	// spostamenti di cartella, niente scritture sul NAS. Serve a far girare il sistema sulla posta
	// vera senza che un difetto si trasformi in una mail modificata o in un file scritto dove non
	// doveva. `dry_run` viene forzato a true e l'unica azione Outlook che resta è «Apri».
	//
	// Vuoto = shadow. È il default sicuro: un cockpit.toml scritto prima che questa opzione
	// esistesse non contiene la riga, e fra le due letture possibili di un silenzio — «non scrivere
	// niente» e «scrivi pure sul NAS di produzione» — solo una si può correggere dopo.
	Modalita string `toml:"modalita"`
	// MaxUploadMB è il limite di un singolo allegato caricato dal worker con
	// PUT /api/v1/allegati/{id}/file (voce 2.3). Oltre, il server risponde 413 prima di leggere il
	// corpo e l'allegato va in errore con il motivo visibile; senza un limite un allegato da qualche
	// gigabyte riempirebbe lo staging del server in silenzio. Assente = 64; zero o meno e' un errore
	// d'avvio (un limite a zero rifiuterebbe ogni allegato).
	MaxUploadMB int `toml:"max_upload_mb"`
	// URLPubblico e' l'indirizzo con cui i worker e i browser CHIAMANO il server (7C.1, P1):
	// «https://10.0.0.7:8443», «https://cockpit.azienda.local:8443». Finisce nel worker.toml del
	// pacchetto e nelle istruzioni della postazione. Vuoto = si deriva dal bind: con
	// `indirizzo = "0.0.0.0:8443"` viene fuori il nome host della macchina, che sulla LAN non e'
	// detto che si risolva — al banco del 20/09/2026 il worker analisi ha avuto «getaddrinfo failed»
	// sul nome del server mentre il worker Outlook lo risolveva. Dichiararlo toglie la dipendenza dal
	// nome. L'host dichiarato entra anche nei nomi del certificato generato (SAN), cosi' il browser
	// non ha un avviso in piu' da ignorare.
	URLPubblico string `toml:"url_pubblico"`
	// RetiConsentite sono le reti da cui il server accetta una connessione: «10.0.0.0/24»,
	// «fd12:3456:789a:1::/64», o un indirizzo solo. Il filtro sta sul listener, prima del TLS: un PC
	// fuori elenco non riceve nemmeno il certificato. Questo PC (loopback e l'indirizzo su cui si
	// ascolta) passa sempre. Vuoto = nessun filtro, com'era prima.
	//
	// Solo reti della LAN (leggiRete): un elenco che lasciasse entrare internet non e' un filtro, e
	// il server non parte.
	RetiConsentite []string `toml:"reti_consentite"`
	// Reti e' RetiConsentite letta: prefissi mascherati, un IPv4 scritto in forma 4in6 riportato a IPv4.
	Reti []netip.Prefix `toml:"-"`
	// DaRigaDiComando dice che le voci di rete qui sopra vengono da -ascolto e compagni (Rete), non
	// dal file: il log dell'avvio lo scrive, perche' chi legge cockpit.toml ci trova un'altra rete.
	DaRigaDiComando bool `toml:"-"`
}

// Rete e' la rete dichiarata sulla riga di comando (-ascolto, -tls-cert, -tls-key, -url-pubblico,
// -reti): la usano gli avviatori di scripts/avvio-rete, che non toccano cockpit.toml.
//
// Quando c'e' l'indirizzo, vale PER INTERO al posto delle voci di rete di [server]: indirizzo,
// tls_cert, tls_key, tls_nomi, url_pubblico, reti_consentite e consenti_lan_in_chiaro. Prenderne un
// pezzo dal file e un pezzo da qui darebbe un server che ascolta su un indirizzo e manda i worker su
// un altro, o uno che eredita un «in chiaro» dichiarato per un'altra rete. Le altre voci del file
// (database, NAS, utenti, modalita') restano quelle.
//
// I percorsi del certificato sono relativi alla cartella del file di configurazione, come nel file.
type Rete struct {
	Indirizzo   string
	TLSCert     string
	TLSKey      string
	URLPubblico string
	Reti        []string
}

// applica mette la rete della riga di comando al posto di quella del file. Senza indirizzo non
// cambia niente, ma il resto da solo e' un errore: un certificato o un filtro dati senza dire dove
// ascoltare sono una rete a meta', e a meta' non si parte.
func (r Rete) applica(s *Server) error {
	if strings.TrimSpace(r.Indirizzo) == "" {
		if r.TLSCert != "" || r.TLSKey != "" || r.URLPubblico != "" || len(r.Reti) > 0 {
			return fmt.Errorf("config: la rete dalla riga di comando vuole l'indirizzo di ascolto (-ascolto): " +
				"senza, certificato, url pubblico e reti consentite non valgono")
		}
		return nil
	}
	s.Indirizzo = strings.TrimSpace(r.Indirizzo)
	s.TLSCert, s.TLSKey = r.TLSCert, r.TLSKey
	s.TLSNomi = nil
	s.URLPubblico = r.URLPubblico
	s.RetiConsentite = r.Reti
	s.ConsentiLanInChiaro = false
	s.DaRigaDiComando = true
	return nil
}

type DB struct {
	DSN string `toml:"dsn"`
}

// PercorsoLog dice dove va il log del server; "" significa «solo sullo stdout».
//
// Il default non è una cartella qualsiasi: è la stessa <staging>\log dove scrivono i worker, perché
// la diagnosi di un job si fa mettendo le due metà una accanto all'altra — chi ha chiesto che cosa e
// che cosa ha risposto il server — e cercarle in due posti diversi è metà del lavoro.
func (c *Config) PercorsoLog() string {
	switch {
	case c.Server.LogFile == "-":
		return ""
	case c.Server.LogFile != "":
		return c.Server.LogFile
	case c.NAS.Staging != "":
		return filepath.Join(c.NAS.Staging, "log", "cockpit.log")
	default:
		return ""
	}
}

type NAS struct {
	// Radice: la cartella «PREVENTIVI DA FARE» del NAS, un UNC come \\server-nas\preventivi\PREVENTIVI DA FARE
	// (in prova: una cartella locale). Staging: la cartella di lavoro del server (cache dei contenuti,
	// log). Tutte e due, se relative, si leggono dalla cartella del file di configurazione.
	Radice string `toml:"radice"`
	// DryRun e' DEPRECATA dal blocco 4: la scrittura sul NAS la governa [sicurezza].nas_scrittura.
	// Resta letta per compatibilita' con i file scritti prima — `true` spegne nas_scrittura, che e'
	// esattamente cio' che quella voce ha sempre significato — e il server lo dice nel log ogni volta
	// che la trova. Va tolta dai file.
	DryRun  bool   `toml:"dry_run"`
	Staging string `toml:"staging"`
	// RadiciProduzione sono le radici VERE, dichiarate una volta (elenco di percorsi UNC).
	//
	// Servono a una cosa sola: impedire che una prova in shadow parta puntata sul NAS di produzione
	// (§2.7). Un `dry_run` dimenticato a false e una radice copia-incollata dal file di produzione
	// sono due errori di battitura che insieme scrivono nel fascicolo di un cliente; dichiarare qui
	// le radici vere trasforma quella combinazione in un errore all'avvio, che si legge.
	RadiciProduzione []string `toml:"radici_produzione"`
	// IntervalloIntegritaS: ogni quanti secondi il ricognitore confronta i documenti del database con
	// i file veri sul NAS (blocco 5B). Assente = 900 (un quarto d'ora). Zero = nessuna passata
	// automatica; «Controlla ora» in Admin funziona lo stesso, perche' leggere il NAS e' sempre
	// consentito e spegnere il giro periodico non vuol dire rinunciare a guardare.
	//
	// Puntatore e non int perche' qui lo zero e' una scelta — «non guardare da solo» — e va distinto
	// dal silenzio di chi non ha scritto la riga.
	IntervalloIntegritaS *int `toml:"intervallo_integrita_s"`
}

// IntervalloIntegrita e' ogni quanto gira il ricognitore dell'integrita' del NAS.
func (c *Config) IntervalloIntegrita() time.Duration {
	if c.NAS.IntervalloIntegritaS == nil {
		return 15 * time.Minute
	}
	return time.Duration(*c.NAS.IntervalloIntegritaS) * time.Second
}

// Modalità del server (§2.7). Non sono stringhe libere: un valore scritto male non deve poter
// significare «produzione» per distrazione.
const (
	ModalitaShadow     = "shadow"
	ModalitaProduzione = "produzione"
)

// EShadow dice se il server gira in sola lettura verso il mondo.
func (c *Config) EShadow() bool { return c.Server.Modalita == ModalitaShadow }

// RadiceDiProduzione restituisce la radice dichiarata di produzione che contiene (o coincide con)
// `nas.radice`, oppure "" se non ce n'è nessuna. Il confronto ignora maiuscole, separatori e barre
// finali, perché sono le tre cose che cambiano fra un file e l'altro senza cambiare la cartella.
func (c *Config) RadiceDiProduzione() string {
	r := normalizzaPercorso(c.NAS.Radice)
	if r == "" {
		return ""
	}
	for _, grezza := range c.RadiciProduzione() {
		p := normalizzaPercorso(grezza)
		if p == "" {
			continue
		}
		if r == p || strings.HasPrefix(r, p+`\`) {
			return grezza
		}
	}
	return ""
}

// RadiciProduzione è l'elenco dichiarato, senza voci vuote.
func (c *Config) RadiciProduzione() []string {
	var out []string
	for _, r := range c.NAS.RadiciProduzione {
		if strings.TrimSpace(r) != "" {
			out = append(out, strings.TrimSpace(r))
		}
	}
	return out
}

func normalizzaPercorso(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "/", `\`)
	for len(p) > 1 && strings.HasSuffix(p, `\`) {
		p = strings.TrimSuffix(p, `\`)
	}
	return strings.ToLower(p)
}

type Outlook struct {
	Cartelle        []string `toml:"cartelle"`          // es. ["Inbox", "Sent Items"]
	IntervalloSyncS int      `toml:"intervallo_sync_s"` // ogni quanti secondi accodare sync_outlook
	// GiorniSyncIniziale: quanto indietro guarda una (casella, cartella) che NON ha ancora un
	// cursore. Vale una volta sola: appena il primo sync scrive un cursore decide il cursore, e un
	// riavvio non riporta la casella qui. Assente (o 0) = coda.GiorniSyncInizialeDefault.
	GiorniSyncIniziale int `toml:"giorni_sync_iniziale"`
	// SyncAperturaInbox: alla PRIMA apertura dell'Inbox di una sessione si accoda un aggiornamento,
	// una volta sola. Assente = true.
	//
	// E' un puntatore perche' l'assenza e il `false` devono essere due cose diverse: con un `bool`
	// normale un file che non nomina la voce e un file che la spegne sarebbero indistinguibili, e il
	// valore predefinito non potrebbe essere `true`.
	//
	// Non c'entra con `intervallo_sync_s`, che governa il sync PERIODICO: a zero non si accoda
	// niente da solo, ma chi apre l'Inbox sta per guardare quella posta e quel sync lo ha chiesto.
	SyncAperturaInbox *bool  `toml:"sync_apertura_inbox"`
	Dal               string `toml:"dal"`             // "2026-09-01": OVERRIDE esplicito della finestra iniziale, per import controllati
	Lotto             int    `toml:"lotto"`           // messaggi per POST ingest
	ConsentiInvio     bool   `toml:"consenti_invio"`  // senza effetto: il server non invia posta, prepara bozze; letta per dirlo nel log
	CasellaDefault    string `toml:"casella_default"` // indirizzo della casella attribuita ai messaggi che non la dichiarano (fase 1)
}

// Casella è una voce [[casella]]: una casella di posta censita, personale o condivisa.
type Casella struct {
	Indirizzo string `toml:"indirizzo"` // 'nome.cognome@azienda.example' (normalizzato in minuscolo)
	Nome      string `toml:"nome"`      // etichetta in UI: 'Francesco'
	Canale    string `toml:"canale"`    // default 'outlook'
	Condivisa bool   `toml:"condivisa"` // true = cassetta condivisa Exchange (nessun proprietario)
	Utente    string `toml:"utente"`    // sigla del proprietario; vuoto o ignorato se condivisa
	// Attiva: assente = true. Serve a dichiarare nel file quale casella è in uso, invece di doverlo
	// fare a mano in database a ogni avvio. Finché lo schema è alla 0003 ne può essere attiva una sola.
	Attiva *bool `toml:"attiva"`
}

// EAttiva risolve il default: una casella senza `attiva` nel file è attiva.
func (c Casella) EAttiva() bool { return c.Attiva == nil || *c.Attiva }

// Postazione è una voce [[postazione]]: un PC con Outlook e un worker.
type Postazione struct {
	NomeHost    string `toml:"nome_host"` // 'PC-FRANCESCO' (normalizzato in maiuscolo)
	Descrizione string `toml:"descrizione"`
	Utente      string `toml:"utente"` // sigla di chi ci lavora
}

// Worker è una voce [[worker]]: credenziale individuale e caselle autorizzate.
// Il token sta solo qui: nel DB finisce il suo sha256.
type Worker struct {
	Nome       string   `toml:"nome"`       // 'outlook@PC-FRANCESCO'
	Tipo       string   `toml:"tipo"`       // outlook | analisi
	Token      string   `toml:"token"`      // segreto individuale
	Postazione string   `toml:"postazione"` // nome host
	Caselle    []string `toml:"caselle"`    // indirizzi autorizzati
}

type Utente struct {
	Sigla    string `toml:"sigla"`
	Nome     string `toml:"nome"`
	Ufficio  string `toml:"ufficio"`
	Ruolo    string `toml:"ruolo"`
	Password string `toml:"password"` // solo per il seed iniziale; nel DB va l'hash bcrypt
}

func Carica(percorso string) (*Config, error) {
	return CaricaConRete(percorso, Rete{})
}

// CaricaConRete e' Carica con la rete della riga di comando al posto di quella del file (Rete). La
// rete entra PRIMA delle verifiche: il rifiuto del chiaro fuori da questo PC, lo schema di url_pubblico
// e le reti consentite valgono per lei come per il file.
func CaricaConRete(percorso string, rete Rete) (*Config, error) {
	c := &Config{}
	c.Server.Indirizzo = "127.0.0.1:8080"
	c.Server.LogLivello = "info"
	c.Server.MaxUploadMB = 64
	c.Server.Modalita = ModalitaShadow
	c.Outlook.Cartelle = []string{"Inbox", "Sent Items"}
	c.Outlook.IntervalloSyncS = 60
	c.Outlook.Lotto = 50
	c.Retention.GiorniJob = 30
	c.Analisi.Versione = 1
	c.Staging.MaxMB = 20
	md, err := toml.DecodeFile(percorso, c)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", percorso, err)
	}
	c.Avvisi = vociSenzaEffetto(md, c)
	if c.DB.DSN == "" {
		return nil, fmt.Errorf("config: [db].dsn mancante")
	}
	// I percorsi del file si leggono dalla cartella del file, non da quella da cui e' partito l'eseguibile:
	// un'attivita' pianificata o un servizio partono da C:\Windows\System32, e li' lo staging relativo
	// finiva in System32 e la radice del NAS non si trovava. Prima valeva solo per tls_cert e tls_key.
	dir := cartellaDelFile(percorso)
	c.NAS.Radice = assoluto(dir, c.NAS.Radice)
	c.NAS.Staging = assoluto(dir, c.NAS.Staging)
	if c.Server.LogFile != "-" {
		c.Server.LogFile = assoluto(dir, c.Server.LogFile)
	}
	c.MotoreA.Regole = assoluto(dir, c.MotoreA.Regole)
	if err := rete.applica(&c.Server); err != nil {
		return nil, err
	}
	if err := c.normalizzaRete(dir); err != nil {
		return nil, err
	}
	if c.Server.MaxUploadMB < 1 {
		return nil, fmt.Errorf("config: [server].max_upload_mb = %d non valido (almeno 1)", c.Server.MaxUploadMB)
	}
	if err := c.normalizzaModalita(); err != nil {
		return nil, err
	}
	if c.NAS.Staging == "" {
		c.NAS.Staging = filepath.Join(dir, "staging")
	}
	if err := os.MkdirAll(c.NAS.Staging, 0o755); err != nil {
		return nil, fmt.Errorf("staging %s: %w", c.NAS.Staging, err)
	}
	if err := c.normalizzaOutlook(); err != nil {
		return nil, err
	}
	if err := c.normalizzaUtenti(); err != nil {
		return nil, err
	}
	if err := c.normalizzaFondazioni(); err != nil {
		return nil, err
	}
	if err := c.normalizzaMotoreA(); err != nil {
		return nil, err
	}
	return c, nil
}

// normalizzaMotoreA ferma l'avvio in un solo caso (piano 7.4.1): l'anteprima accesa senza indice, un errore che si vede
// senza aprire file. L'indice, le grammatiche e il file dei casi li legge l'avvio del server (app/runtime/motore_a.go),
// che con un indice rotto parte lo stesso: la pagina dice l'errore, il log il motivo.
func (c *Config) normalizzaMotoreA() error {
	if c.MotoreA.Anteprima && c.MotoreA.Regole == "" {
		return fmt.Errorf("config: [motore_a].anteprima = true senza [motore_a].regole: serve l'indice delle regole " +
			"del motore A, con un percorso assoluto o relativo alla cartella di questo file")
	}
	return nil
}

// normalizzaUtenti valida [[utenti]] (voce 6.9). Un ruolo che non esiste ferma l'avvio.
//
// Prima diventava `operatore` in silenzio, e questo è il modo peggiore di sbagliare: chi scrive
// `amministratore` invece di `admin` ottiene un utente che crede di amministrare il Cockpit e non
// può aprire nessuna schermata tecnica, senza una riga da nessuna parte che gli dica perché. I
// ruoli ammessi sono quelli dell'enum dello schema, non un elenco copiato qui: sono quattro dalla
// migrazione 0001.
//
// `ufficio` resta informativo: è l'organigramma, non un permesso. Le autorizzazioni dipendono dal
// ruolo, mai dalla sigla e mai dall'ufficio.
func (c *Config) normalizzaUtenti() error {
	viste := map[string]bool{}
	for i := range c.Utenti {
		u := &c.Utenti[i]
		u.Sigla = strings.ToUpper(strings.TrimSpace(u.Sigla))
		u.Ruolo = strings.ToLower(strings.TrimSpace(u.Ruolo))
		if u.Sigla == "" {
			return fmt.Errorf("config: [[utenti]] #%d senza sigla", i+1)
		}
		if viste[u.Sigla] {
			return fmt.Errorf("config: utente %q dichiarato due volte", u.Sigla)
		}
		viste[u.Sigla] = true
		if u.Ruolo == "" {
			return fmt.Errorf("config: utente %s senza ruolo (ammessi: %s)", u.Sigla, RuoliAmmessi())
		}
		if !db.RuoloUtente(u.Ruolo).Valid() {
			return fmt.Errorf("config: utente %s: ruolo %q non valido (ammessi: %s)", u.Sigla, u.Ruolo, RuoliAmmessi())
		}
	}
	// Almeno un `admin`, se qualcuno c'è. Le schermate tecniche — coda dei job, scarti, e soprattutto
	// la generazione dei pacchetti dei worker — sono sue: un file con soli operatori è un Cockpit in
	// cui nessuno può più aggiungere un PC, e lo si scopre dal primo 403 sulla pagina *Postazioni*,
	// cioè mentre si sta facendo altro. Il README lo dichiara dalla voce 6.9: una regola scritta e
	// non imposta è peggio di una regola che non c'è.
	//
	// Nessun utente configurato non è un errore qui: è un file a cui non sono ancora stati aggiunti,
	// e lo dice l'impossibilità di entrare, non un avvio che si rifiuta.
	if len(c.Utenti) > 0 {
		admin := false
		for _, u := range c.Utenti {
			admin = admin || db.RuoloUtente(u.Ruolo) == db.RuoloUtenteAdmin
		}
		if !admin {
			return fmt.Errorf("config: nessun utente con ruolo %q fra i %d [[utenti]]: le schermate tecniche (coda job, scarti, postazioni, pacchetti dei worker) non le aprirebbe nessuno",
				db.RuoloUtenteAdmin, len(c.Utenti))
		}
	}
	return nil
}

// RuoliAmmessi elenca i ruoli dell'enum, per i messaggi d'errore.
func RuoliAmmessi() string {
	nomi := make([]string, 0, len(db.AllRuoloUtenteValues()))
	for _, r := range db.AllRuoloUtenteValues() {
		nomi = append(nomi, string(r))
	}
	return strings.Join(nomi, ", ")
}

// normalizzaModalita valida [server].modalita e ne applica le conseguenze (§2.7, voce 9.5).
//
// Due conseguenze, e sono entrambe rifiuti: in shadow il server non parte se la radice del NAS è una
// radice di produzione dichiarata, e `dry_run` viene forzato a true qualunque cosa dica il file. Un
// avvio in shadow che scrive sul NAS vero non è una shadow: è una prova sulla produzione con un
// badge rassicurante in testata.
func (c *Config) normalizzaModalita() error {
	c.Server.Modalita = strings.ToLower(strings.TrimSpace(c.Server.Modalita))
	if c.Server.Modalita == "" {
		c.Server.Modalita = ModalitaShadow
	}
	if c.Server.Modalita != ModalitaShadow && c.Server.Modalita != ModalitaProduzione {
		return fmt.Errorf("config: [server].modalita = %q non valida (%s | %s)", c.Server.Modalita, ModalitaShadow, ModalitaProduzione)
	}
	if !c.EShadow() {
		// In produzione una radice di produzione non e' vietata: e' il posto dove il Cockpit
		// lavorera' davvero. Cio' che non deve costare un interruttore solo e' COMINCIARE a
		// scriverci. Se la capacita' e' spenta non si controlla niente: un server che non scrive non
		// puo' sbagliare cartella.
		if c.Capacita().NasScrittura {
			if r := c.RadiceDiProduzione(); r != "" && !vuole(c.Sicurezza.ConsentiNasProduzione) {
				return fmt.Errorf("config: [sicurezza].nas_scrittura = true e [nas].radice (%s) è sotto la radice di produzione %q dichiarata in [nas].radici_produzione. "+
					"Per scrivere sul NAS vero serve una seconda dichiarazione esplicita: [sicurezza].consenti_nas_produzione = true. "+
					"Se questa doveva essere una prova, è la radice a essere sbagliata",
					c.NAS.Radice, r)
			}
		}
		return nil
	}
	if r := c.RadiceDiProduzione(); r != "" {
		return fmt.Errorf("config: [server].modalita = shadow ma [nas].radice (%s) è sotto la radice di produzione %q dichiarata in [nas].radici_produzione: "+
			"una prova in shadow non si fa sul NAS vero. Cambiare radice, oppure passare a modalita = \"produzione\" se è ciò che si vuole davvero",
			c.NAS.Radice, r)
	}
	// `dry_run` NON viene piu' forzato qui: dal blocco 4 la scrittura sul NAS la decide
	// [sicurezza].nas_scrittura, e in shadow Capacita() la spegne insieme alle altre due. Forzarla
	// anche qui vorrebbe dire due sorgenti per la stessa decisione, e prima o poi una delle due
	// verrebbe cambiata da sola.
	return nil
}

// ETLS dice se il listener parla TLS: servono tutti e due i file, perché mezzo TLS non esiste.
func (c *Config) ETLS() bool {
	return strings.TrimSpace(c.Server.TLSCert) != "" && strings.TrimSpace(c.Server.TLSKey) != ""
}

// Schema è "https" o "http": serve a scrivere gli indirizzi (log, pagina Postazioni, worker.toml
// generato) con lo schema che il server sta davvero usando, invece di uno scritto a mano.
func (c *Config) Schema() string {
	if c.ETLS() {
		return "https"
	}
	return "http"
}

// SuLoopback dice se [server].indirizzo ascolta solo su questo PC. Un indirizzo senza host
// ("":8080) ascolta su tutte le interfacce: è LAN a tutti gli effetti.
func SuLoopback(indirizzo string) bool {
	host := strings.TrimSpace(indirizzo)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "":
		return false
	case "localhost":
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		// Un nome che non è un IP: non si può dire che sia loopback, e nel dubbio non lo è.
		return false
	}
	return ip.IsLoopback()
}

// normalizzaRete applica la voce 2.4: TLS sul listener e credenziali individuali.
//
// Due conseguenze, e sono di natura diversa.
//
//  1. Un RIFIUTO: fuori da loopback senza TLS il server non parte. Il traffico che passa di qui è la
//     posta dell'azienda, i token dei worker e il cookie di sessione dell'operatore; in chiaro su una
//     LAN li legge chiunque sia attaccato allo stesso switch, e non se ne accorge nessuno mai. Chi ha
//     davvero bisogno del chiaro (una prova, un tunnel che cifra già lui) lo dichiara con
//     `consenti_lan_in_chiaro = true`, che si legge nel file e nel log.
//  2. Un AVVISO che non ferma niente: `[server].token_worker` non autentica più. Toglierlo dal file è
//     una riga in meno; lasciarlo credendo che serva è un segreto condiviso che resta in giro.
func (c *Config) normalizzaRete(dirConfig string) error {
	c.Server.TLSCert = assoluto(dirConfig, c.Server.TLSCert)
	c.Server.TLSKey = assoluto(dirConfig, c.Server.TLSKey)
	if strings.TrimSpace(c.Server.TLSCert) != "" && strings.TrimSpace(c.Server.TLSKey) == "" {
		return fmt.Errorf("config: [server].tls_cert senza [server].tls_key (servono tutti e due)")
	}
	if strings.TrimSpace(c.Server.TLSKey) != "" && strings.TrimSpace(c.Server.TLSCert) == "" {
		return fmt.Errorf("config: [server].tls_key senza [server].tls_cert (servono tutti e due)")
	}
	if !c.ETLS() && !SuLoopback(c.Server.Indirizzo) && !c.Server.ConsentiLanInChiaro {
		return fmt.Errorf("config: [server].indirizzo = %q ascolta fuori da questo PC e [server].tls_cert non c'è. "+
			"In chiaro sulla LAN viaggiano la posta, i token dei worker e il cookie di sessione. "+
			"Indicare tls_cert/tls_key (se i file non esistono il server ne genera uno autofirmato e ne scrive l'impronta), "+
			"oppure dichiarare consenti_lan_in_chiaro = true se il collegamento è già cifrato da qualcos'altro",
			c.Server.Indirizzo)
	}
	// url_pubblico (7C.1, P1): lo schema deve essere quello che il listener parla davvero. Un
	// pacchetto con «http://» su un server TLS manda i worker a bussare in chiaro a una porta che
	// risponde solo cifrata; il contrario manda l'impronta a verificare un certificato che non c'e'.
	c.Server.URLPubblico = strings.TrimRight(strings.TrimSpace(c.Server.URLPubblico), "/")
	if c.Server.URLPubblico != "" {
		u, err := url.Parse(c.Server.URLPubblico)
		if err != nil || u.Host == "" || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("config: [server].url_pubblico = %q non valido: serve «https://host[:porta]» (o http:// se il server gira in chiaro), senza percorso", c.Server.URLPubblico)
		}
		if u.Scheme != c.Schema() {
			return fmt.Errorf("config: [server].url_pubblico = %q usa %s:// ma il server parla %s:// (tls_cert/tls_key %s)",
				c.Server.URLPubblico, u.Scheme, c.Schema(), map[bool]string{true: "presenti", false: "assenti"}[c.ETLS()])
		}
	}
	c.Server.Reti = nil
	for _, voce := range c.Server.RetiConsentite {
		p, err := leggiRete(voce)
		if err != nil {
			return err
		}
		c.Server.Reti = append(c.Server.Reti, p)
	}
	return nil
}

// blocchiLAN sono gli spazi di indirizzi che non escono dalla rete dell'azienda: gli IPv4 privati
// (RFC 1918) e link-local, gli IPv6 ULA (fc00::/7) e link-local, e questo PC.
var blocchiLAN = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("::1/128"),
}

// leggiRete legge una voce di [server].reti_consentite: una rete («10.0.0.0/24») o un indirizzo solo
// («10.0.0.15», che vale /32; per IPv6 /128). La rete deve stare nella LAN: dentro uno dei blocchiLAN,
// oppure un prefisso IPv6 globale non piu' largo di un segmento (/64), che e' come un server IPv6
// vede i PC della sua stessa rete quando l'azienda non usa gli ULA. «0.0.0.0/0», un IPv4 pubblico o
// «2000::/3» farebbero entrare internet: la voce che doveva chiudere aprirebbe, e il server non parte.
func leggiRete(voce string) (netip.Prefix, error) {
	s := strings.TrimSpace(voce)
	var p netip.Prefix
	if strings.Contains(s, "/") {
		q, err := netip.ParsePrefix(s)
		if err != nil {
			return p, fmt.Errorf("config: [server].reti_consentite: %q non e' una rete (serve «10.0.0.0/24», «fd12:3456:789a:1::/64» o un indirizzo solo)", voce)
		}
		p = q
	} else {
		a, err := netip.ParseAddr(s)
		if err != nil || a.Zone() != "" {
			return p, fmt.Errorf("config: [server].reti_consentite: %q non e' un indirizzo (serve «10.0.0.15», «fd12:3456:789a:1::15» o una rete con /, senza %%zona)", voce)
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	if p.Addr().Is4In6() {
		if p.Bits() < 96 {
			return p, fmt.Errorf("config: [server].reti_consentite: %q va oltre gli indirizzi IPv4 che nomina", voce)
		}
		p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
	}
	p = p.Masked()
	for _, b := range blocchiLAN {
		if b.Bits() <= p.Bits() && b.Contains(p.Addr()) {
			return p, nil
		}
	}
	if p.Addr().Is6() && p.Addr().IsGlobalUnicast() && p.Bits() >= 64 {
		return p, nil
	}
	return p, fmt.Errorf("config: [server].reti_consentite: %q (%s) non e' una rete della LAN. Si ammettono gli IPv4 privati "+
		"(10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) e link-local, gli IPv6 ULA (fc00::/7) e link-local, e un prefisso IPv6 "+
		"globale di /64 o piu' stretto: una rete piu' larga lascerebbe entrare internet", voce, p)
}

// HostPubblico e' il solo host di [server].url_pubblico, senza schema e porta; vuoto se non dichiarato.
// Serve al certificato generato (SAN) e al confronto con i nomi di uno gia' esistente.
func (c *Config) HostPubblico() string {
	if c.Server.URLPubblico == "" {
		return ""
	}
	u, err := url.Parse(c.Server.URLPubblico)
	if err != nil {
		return ""
	}
	return strings.Trim(u.Hostname(), "[]")
}

// assoluto risolve un percorso relativo rispetto alla cartella del file di configurazione: i
// percorsi in un file si leggono da dove sta il file, non da dove è stato lanciato l'eseguibile.
//
// Restano come sono i percorsi vuoti, quelli assoluti, e quelli che assoluti lo sono anche se il sistema
// su cui gira il server non li riconosce come tali: un UNC (\\server\share, //server/share), un percorso
// che comincia con una barra (/mnt/nas su Linux, \cartella «dalla radice del disco» su Windows) o con la
// lettera di un disco (C:\..., anche letto da un server Linux). Nessuno di questi e' relativo alla
// cartella del file, e attaccarglielo davanti darebbe un percorso che non esiste.
func assoluto(dir, p string) string {
	p = strings.TrimSpace(p)
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "/") || conDisco(p) {
		return p
	}
	return filepath.Join(dir, p)
}

// conDisco dice se il percorso comincia con la lettera di un disco Windows («C:»).
func conDisco(p string) bool {
	return len(p) >= 2 && p[1] == ':' && ('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z')
}

// cartellaDelFile e' la cartella, assoluta, del file di configurazione: e' la base dei percorsi relativi
// del file. Assoluta perche' finisce nel log e nei messaggi d'errore, e «.\staging» non dice dove.
func cartellaDelFile(percorso string) string {
	dir := filepath.Dir(percorso)
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// vociSenzaEffetto sono gli avvisi sulle voci del file che non cambiano niente: quelle che non esistono
// (un refuso in `reti_consentite` voleva dire nessun filtro, in silenzio) e quelle che si leggono ancora
// ma non hanno piu' effetto. Non fermano l'avvio: un file scritto per un binario di prima deve partire.
func vociSenzaEffetto(md toml.MetaData, c *Config) []string {
	var avvisi []string
	for _, k := range md.Undecoded() {
		avvisi = append(avvisi, fmt.Sprintf("la voce %q non esiste e viene ignorata: un refuso vale come se la riga non ci fosse", k.String()))
	}
	if strings.TrimSpace(c.Server.SegretoSessione) != "" {
		avvisi = append(avvisi, "[server].segreto_sessione non ha effetto: le sessioni stanno nel database e nessun cookie si firma con questo. La riga si puo' togliere")
	}
	if md.IsDefined("outlook", "consenti_invio") {
		avvisi = append(avvisi, "[outlook].consenti_invio non ha effetto sul server: il Cockpit prepara bozze e non ha un invio da accendere. La riga si puo' togliere")
	}
	return avvisi
}

// normalizzaFondazioni mette in forma canonica [[casella]], [[postazione]], [[worker]] e
// [outlook].casella_default, e rifiuta i riferimenti incrociati che non esistono. Un errore qui
// blocca l'avvio: meglio non partire che partire con un routing sbagliato.
func (c *Config) normalizzaFondazioni() error {
	indirizzi := map[string]bool{}
	for i := range c.Caselle {
		k := &c.Caselle[i]
		k.Indirizzo = strings.ToLower(strings.TrimSpace(k.Indirizzo))
		if k.Indirizzo == "" {
			return fmt.Errorf("config: [[casella]] #%d senza indirizzo", i+1)
		}
		if indirizzi[k.Indirizzo] {
			return fmt.Errorf("config: casella %q dichiarata due volte", k.Indirizzo)
		}
		indirizzi[k.Indirizzo] = true
		if k.Canale == "" {
			k.Canale = "outlook"
		}
		if k.Nome == "" {
			k.Nome, _, _ = strings.Cut(k.Indirizzo, "@")
		}
		k.Utente = strings.ToUpper(strings.TrimSpace(k.Utente))
		if k.Condivisa && k.Utente != "" {
			return fmt.Errorf("config: casella %q è condivisa e non può avere un proprietario (utente = %q)", k.Indirizzo, k.Utente)
		}
	}
	host := map[string]bool{}
	for i := range c.Postazioni {
		p := &c.Postazioni[i]
		p.NomeHost = strings.ToUpper(strings.TrimSpace(p.NomeHost))
		if p.NomeHost == "" {
			return fmt.Errorf("config: [[postazione]] #%d senza nome_host", i+1)
		}
		if host[p.NomeHost] {
			return fmt.Errorf("config: postazione %q dichiarata due volte", p.NomeHost)
		}
		host[p.NomeHost] = true
		p.Utente = strings.ToUpper(strings.TrimSpace(p.Utente))
	}
	nomi := map[string]bool{}
	tokenDi := map[string]string{} // token → il primo worker che l'ha dichiarato
	for i := range c.Worker {
		w := &c.Worker[i]
		w.Nome = strings.TrimSpace(w.Nome)
		if w.Nome == "" {
			return fmt.Errorf("config: [[worker]] #%d senza nome", i+1)
		}
		if nomi[w.Nome] {
			return fmt.Errorf("config: worker %q dichiarato due volte", w.Nome)
		}
		nomi[w.Nome] = true
		if w.Tipo != "outlook" && w.Tipo != "analisi" {
			return fmt.Errorf("config: worker %q: tipo %q non valido (outlook | analisi)", w.Nome, w.Tipo)
		}
		// Un worker senza `token` non è più un errore (voce 2.4): significa «il segreto lo genera la
		// pagina Postazioni», che è la via consigliata in azienda — il pacchetto scaricato da lì
		// contiene indirizzo, impronta e token, e nessuno li copia a mano. Un token scritto qui invece
		// vince a ogni avvio. Quello che NON si può fare è darne uno solo a due worker: in quel caso
		// il server non sa chi sta chiamando, e se ne accorge al primo claim con un 401.
		if w.Token != "" {
			if altro, gia := tokenDi[w.Token]; gia {
				return fmt.Errorf("config: i worker %q e %q hanno lo stesso token: un segreto che è di due non identifica nessuno, "+
					"e il server risponderà 401 a tutti e due. Darne uno diverso a ciascuno, oppure lasciarli vuoti e generare il "+
					"pacchetto dalla pagina Postazioni", altro, w.Nome)
			}
			tokenDi[w.Token] = w.Nome
		}
		w.Postazione = strings.ToUpper(strings.TrimSpace(w.Postazione))
		if w.Postazione != "" && !host[w.Postazione] {
			return fmt.Errorf("config: worker %q: postazione %q non dichiarata in [[postazione]]", w.Nome, w.Postazione)
		}
		for j, ind := range w.Caselle {
			w.Caselle[j] = strings.ToLower(strings.TrimSpace(ind))
			if !indirizzi[w.Caselle[j]] {
				return fmt.Errorf("config: worker %q: casella %q non dichiarata in [[casella]]", w.Nome, w.Caselle[j])
			}
		}
	}
	c.Outlook.CasellaDefault = strings.ToLower(strings.TrimSpace(c.Outlook.CasellaDefault))
	switch {
	case c.Outlook.CasellaDefault != "":
		if !indirizzi[c.Outlook.CasellaDefault] {
			return fmt.Errorf("config: [outlook].casella_default = %q non è fra le [[casella]] dichiarate", c.Outlook.CasellaDefault)
		}
	case len(c.Caselle) == 1:
		c.Outlook.CasellaDefault = c.Caselle[0].Indirizzo
	case len(c.Caselle) > 1:
		return fmt.Errorf("config: con più [[casella]] serve [outlook].casella_default")
	}
	return nil
}

// normalizzaOutlook valida [outlook]: la finestra iniziale e l'eventuale override `dal`.
//
// `dal` non è più «la data minima»: è un OVERRIDE per gli import controllati (la precedenza sta in
// coda.AccodaSyncCasella), e un override scritto male non deve passare in silenzio. Prima un
// `dal = "01/09/2026"` veniva scartato senza una riga da nessuna parte e il sync partiva dalla
// finestra predefinita: chi credeva di stare importando settembre importava l'ultima settimana, e se
// ne accorgeva dalle mail che mancavano.
func (c *Config) normalizzaOutlook() error {
	if c.Outlook.GiorniSyncIniziale < 0 {
		return fmt.Errorf("config: [outlook].giorni_sync_iniziale = %d non valido (sono giorni: togli la riga per il valore predefinito)",
			c.Outlook.GiorniSyncIniziale)
	}
	c.Outlook.Dal = strings.TrimSpace(c.Outlook.Dal)
	if c.Outlook.Dal != "" {
		if _, err := DataDal(c.Outlook.Dal); err != nil {
			return err
		}
	}
	return nil
}

// DataDal legge `[outlook].dal` nel fuso locale del server. Sta in un posto solo perché la leggono in
// due — la validazione all'avvio e chi accoda il sync — e due letture diverse della stessa riga
// sarebbero due finestre diverse.
func DataDal(s string) (time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("config: [outlook].dal = %q non è una data AAAA-MM-GG (per esempio 2026-09-01)", s)
	}
	return d, nil
}
