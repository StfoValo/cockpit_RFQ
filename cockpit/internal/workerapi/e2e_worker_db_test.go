//go:build integrazione

// L4 — il worker VERO contro il server VERO (correzione del 15/09/2026).
//
// Il 15/09/2026 il worker ha girato a vuoto un pomeriggio: `risultato()` riempiva worker_id e
// lease_token con setdefault, ma il corpo arriva da RisultatoRichiesta.model_dump() e quelle due
// chiavi ci sono già, vuote. Il server rispondeva 400 a ogni result, il job restava in_corso, il
// lease scadeva, lo scheduler lo rimetteva pronto e il worker lo rifaceva: quattro sync della stessa
// finestra, quattro finestre aperte in Outlook, nessun job chiuso.
//
// Quattro livelli di prove erano verdi. L2 girava contro un server finto che accettava qualunque
// corpo; L3 confronta gli schemi dei contratti, e gli schemi erano identici — il difetto non era
// nello schema ma in ciò che il client ci scriveva dentro; L4 provava il server con un client Go
// scritto apposta. Nessuno faceva parlare il client vero con il server vero: è quello che manca, ed
// è questo file.
//
// Qui il server è quello vero — gli stessi gestori HTTP che monta cockpit.exe, sullo stesso
// PostgreSQL — e il client è quello vero: `python prova_e2e.py` lancia worker_outlook.main() con
// --una-volta, sostituendo SOLO la classe che parla con COM. Ciò che non è provato qui è l'avvio di
// cockpit.exe (configurazione, migrazioni, scheduler) e Outlook: il primo è materia di main.go, il
// secondo di L5.
package workerapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"promatec/cockpit/internal/api"
	"promatec/cockpit/internal/config"
	"promatec/cockpit/internal/db"
	"promatec/cockpit/internal/fondazioni"
	"promatec/cockpit/internal/jobs"
	"promatec/cockpit/internal/testutil"
)

// contenutoE2E è il finto allegato di prova_e2e.py: bytes(range(256)) * COCKPIT_E2E_BLOCCHI.
// Deterministico da questa parte e da quella, così l'hash atteso non ha bisogno di essere
// comunicato: se le due definizioni divergono, il server rifiuta il result e il test lo dice.
func contenutoE2E(blocchi int) ([]byte, string) {
	blocco := make([]byte, 256)
	for i := range blocco {
		blocco[i] = byte(i)
	}
	c := bytes.Repeat(blocco, blocchi)
	h := sha256.Sum256(c)
	return c, hex.EncodeToString(h[:])
}

type bancoE2E struct {
	*banco
	workerID      string
	pc            uuid.UUID // la postazione di QUESTO PC: i job interattivi vanno solo qui
	stagingWorker string    // la cartella temporanea del worker, diversa da quella del server
}

// preparaBancoE2E semina le fondazioni sul nome host vero di questa macchina: il worker dichiara al
// claim la postazione su cui gira (`socket.gethostname()`), e una credenziale intestata a un altro
// PC lo farebbe respingere con 403 — che è il comportamento giusto (voce 2.2), ma qui vogliamo
// provare il giro completo, non il rifiuto.
func preparaBancoE2E(t *testing.T) *bancoE2E {
	t.Helper()
	// Unica via d'uscita, e la chiede prova-tutto.ps1 -SenzaPython: senza interprete il worker vero
	// non si avvia. Salta, quindi NON è passata, e lo script la annota come non verificata.
	if os.Getenv("COCKPIT_TEST_SENZA_PYTHON") != "" {
		t.Skip("COCKPIT_TEST_SENZA_PYTHON: il worker vero non viene avviato, prova non verificata")
	}
	// Stessa regola, per un motivo che non dipende da chi lancia: worker_outlook importa pywintypes
	// (pywin32), che esiste solo su Windows. Fuori di lì il worker vero non è avviabile e non c'è
	// niente da provare — saltata NON vuol dire passata, e il registro degli esiti la annota come non
	// verificata. Su Windows, dove pywin32 ci deve essere, un import rotto resta un fallimento.
	if runtime.GOOS != "windows" {
		t.Skipf("il worker vero importa pywin32 (COM), che su %s non esiste: prova non verificata", runtime.GOOS)
	}
	b := preparaBanco(t, 0)
	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("nome host di questa macchina: %v", err)
	}
	host = strings.ToUpper(host)
	cfg := &config.Config{}
	cfg.Outlook.CasellaDefault = "commerciale@azienda.it"
	cfg.Caselle = []config.Casella{{Indirizzo: "commerciale@azienda.it", Nome: "Commerciale", Canale: "outlook", Condivisa: true}}
	cfg.Postazioni = []config.Postazione{{NomeHost: host}}
	cfg.Worker = []config.Worker{{Nome: "outlook@" + host, Tipo: "outlook", Token: tokenProva, Postazione: host,
		Caselle: []string{"commerciale@azienda.it"}}}
	if _, err := fondazioni.Semina(b.ctx, b.q, cfg, testutil.LogSilenzioso()); err != nil {
		t.Fatal(err)
	}
	p, err := b.q.GetPostazionePerHost(b.ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	return &bancoE2E{banco: b, workerID: "outlook@" + host, pc: p.PostazioneID, stagingWorker: t.TempDir()}
}

// esecuzione è un processo worker avviato: il suo output serve solo se qualcosa va storto, e allora
// serve tutto, perché è l'unico posto in cui si vede che cosa il client ha detto al server.
type esecuzione struct {
	cmd *exec.Cmd
	out *bytes.Buffer
}

// avviaWorker lancia `python prova_e2e.py`, cioè worker_outlook con --una-volta e senza COM.
func (b *bancoE2E) avviaWorker(t *testing.T, lavoroS string) *esecuzione {
	t.Helper()
	py, err := exec.LookPath("python")
	if err != nil {
		// Non si salta: il worker È Python, e una prova saltata non è una prova passata. Su una
		// macchina senza interprete questa riga dice esattamente che cosa manca.
		t.Fatalf("python non è nel PATH: questa prova fa girare il worker vero (serve anche pywin32, che worker_outlook importa): %v", err)
	}
	dir, err := filepath.Abs(filepath.Join("..", "..", "workers"))
	if err != nil {
		t.Fatal(err)
	}
	e := &esecuzione{cmd: exec.Command(py, "prova_e2e.py"), out: &bytes.Buffer{}}
	e.cmd.Dir = dir
	// Le variabili in coda vincono su quelle ereditate: il worker non deve leggere il worker.toml
	// vero di questo PC né parlare con il server vero.
	e.cmd.Env = append(os.Environ(),
		"COCKPIT_URL="+b.srv.URL,
		"COCKPIT_TOKEN="+tokenProva,
		"COCKPIT_WORKER_ID="+b.workerID,
		"COCKPIT_STAGING="+b.stagingWorker,
		"COCKPIT_E2E_LAVORO_S="+lavoroS,
		"PYTHONIOENCODING=utf-8",
		"PYTHONUTF8=1",
	)
	e.cmd.Stdout, e.cmd.Stderr = e.out, e.out
	if err := e.cmd.Start(); err != nil {
		t.Fatalf("avvio del worker: %v", err)
	}
	t.Cleanup(func() {
		if e.cmd.ProcessState == nil && e.cmd.Process != nil {
			_ = e.cmd.Process.Kill()
			_ = e.cmd.Wait()
		}
	})
	return e
}

func (e *esecuzione) attendi(t *testing.T) string {
	t.Helper()
	fatto := make(chan error, 1)
	go func() { fatto <- e.cmd.Wait() }()
	select {
	case err := <-fatto:
		if err != nil {
			t.Fatalf("il worker è uscito con errore (%v). Output:\n%s", err, e.out.String())
		}
	case <-time.After(90 * time.Second):
		_ = e.cmd.Process.Kill()
		t.Fatalf("il worker non è uscito entro 90 s. Output:\n%s", e.out.String())
	}
	return e.out.String()
}

func (b *bancoE2E) eseguiWorker(t *testing.T) string {
	t.Helper()
	return b.avviaWorker(t, "0").attendi(t)
}

func (b *bancoE2E) leggiJob(t *testing.T, id int64) db.Job {
	t.Helper()
	j, err := b.q.GetJob(b.ctx, id)
	if err != nil {
		t.Fatalf("job %d: %v", id, err)
	}
	return j
}

// attendiJob aspetta che il job soddisfi una condizione, o fallisce dicendo com'era l'ultima volta.
func (b *bancoE2E) attendiJob(t *testing.T, id int64, entro time.Duration, cosa string, ok func(db.Job) bool) db.Job {
	t.Helper()
	scadenza := time.Now().Add(entro)
	var ultimo db.Job
	for time.Now().Before(scadenza) {
		ultimo = b.leggiJob(t, id)
		if ok(ultimo) {
			return ultimo
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("il job %d non è mai arrivato a %s entro %s: stato=%s tentativi=%d errore=%q",
		id, cosa, entro, ultimo.Stato, ultimo.Tentativi, ultimo.Errore.String)
	return ultimo
}

// TestE2EIlWorkerVeroChiudeIlJobDelServerVero è la prova che mancava: un download completo, dal
// claim al file promosso nello staging del server, fatto dal client vero.
//
// Con il difetto del 15/09 il job resta 'in_corso' e il file resta .parte.<token>: il worker ha
// fatto tutto il lavoro e il server non lo sa.
func TestE2EIlWorkerVeroChiudeIlJobDelServerVero(t *testing.T) {
	b := preparaBancoE2E(t)
	_, sha := contenutoE2E(8)

	uscita := b.eseguiWorker(t)

	j := b.leggiJob(t, b.job.JobID)
	if j.Stato != db.StatoJobFatto {
		t.Fatalf("job %s in stato %s (tentativi %d, errore %q): il result del worker vero non è stato accettato.\nOutput del worker:\n%s",
			j.Tipo, j.Stato, j.Tentativi, j.Errore.String, uscita)
	}
	if j.Tentativi != 1 {
		t.Errorf("il job è stato eseguito %d volte: al primo tentativo qualcosa non ha chiuso", j.Tentativi)
	}
	if j.LeaseToken.Valid || j.LeaseFinoA != nil {
		t.Errorf("job chiuso ma il tentativo è ancora aperto: token=%v lease=%v", j.LeaseToken.Valid, j.LeaseFinoA)
	}

	// il file è arrivato davvero: caricato con PUT dentro il tentativo e promosso dal result
	a := b.allegatoOra()
	if a.Stato == db.StatoAllegatoGrezzo || !a.PathStaging.Valid {
		t.Fatalf("allegato non in staging: stato=%s path=%q errore=%q", a.Stato, a.PathStaging.String, a.Errore.String)
	}
	if a.Sha256.String != sha {
		t.Errorf("sha256 in database %s, atteso %s", a.Sha256.String, sha)
	}
	if !esiste(a.PathStaging.String) || hashDi(t, a.PathStaging.String) != sha {
		t.Errorf("il file non c'è o è diverso: %s (staging: %v)", a.PathStaging.String, b.fileDiStaging())
	}
	for _, f := range b.fileDiStaging() {
		if strings.Contains(f, ".parte.") {
			t.Errorf("è rimasto un file di tentativo non promosso: %s", f)
		}
	}
	// e il worker non ha lasciato niente a casa sua: il file locale è di passaggio
	if resti := fileSotto(t, filepath.Join(b.stagingWorker, "tmp")); len(resti) > 0 {
		t.Errorf("il worker ha lasciato file temporanei sul proprio disco: %v", resti)
	}
}

// TestE2EIlBattitoDelWorkerVeroRinnovaIlLease: il heartbeat funzionava «per fortuna», perché il
// worker_id viaggia per un'altra strada. Qui si osserva che il lease si allunga DAVVERO mentre il
// lavoro è in corso, cioè che un job lungo non viene tolto al worker che lo sta facendo.
func TestE2EIlBattitoDelWorkerVeroRinnovaIlLease(t *testing.T) {
	b := preparaBancoE2E(t)

	// un job interattivo per questa postazione, con lease corto: il worker batte ogni lease/4, cioè
	// ogni 5 s, e il lavoro finto dura 8 s. Un battito ci sta dentro, e si vede.
	cas := b.casella
	msg := b.msg.MessaggioID
	scade := time.Now().Add(10 * time.Minute)
	p := api.PayloadApriElemento{
		EntryID:             "ENTRY-disegno.pdf",
		RiferimentoElemento: api.RiferimentoElemento{MessaggioID: &msg, CasellaID: &cas, MessageID: b.msg.ChiaveEsterna},
	}
	j, err := jobs.AccodaCon(b.ctx, b.q, db.TipoJobApriElementoOutlook, p, "", 5, jobs.Opzioni{
		Casella:    uuid.NullUUID{UUID: cas, Valid: true},
		Postazione: uuid.NullUUID{UUID: b.pc, Valid: true},
		ScadeIl:    &scade,
		LeaseS:     20,
	})
	if err != nil || j == nil {
		t.Fatalf("accoda apri: job=%v err=%v", j, err)
	}
	// il job dello staging non deve mettersi in mezzo: qui si guarda solo l'interattivo
	if _, err := b.q.AnnullaJob(b.ctx, b.job.JobID); err != nil {
		t.Fatal(err)
	}

	e := b.avviaWorker(t, "8")
	preso := b.attendiJob(t, j.JobID, 30*time.Second, "in_corso", func(x db.Job) bool {
		return x.Stato == db.StatoJobInCorso && x.LeaseFinoA != nil
	})
	primo := *preso.LeaseFinoA
	if preso.WorkerID.String != b.workerID {
		t.Errorf("il job è in corso per %q, atteso %q", preso.WorkerID.String, b.workerID)
	}
	b.attendiJob(t, j.JobID, 20*time.Second, "lease rinnovato dal battito", func(x db.Job) bool {
		return x.LeaseFinoA != nil && x.LeaseFinoA.After(primo)
	})

	uscita := e.attendi(t)
	fine := b.leggiJob(t, j.JobID)
	if fine.Stato != db.StatoJobFatto {
		t.Fatalf("job interattivo in stato %s (errore %q).\nOutput del worker:\n%s", fine.Stato, fine.Errore.String, uscita)
	}
}

// fileSotto elenca ciò che è rimasto in una cartella, per poterlo scrivere nel messaggio d'errore.
func fileSotto(t *testing.T, radice string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(radice, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Log(fmt.Sprintf("lettura di %s: %v", radice, err))
	}
	return out
}
