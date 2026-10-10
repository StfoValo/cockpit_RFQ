// La rete e l'ascolto: il materiale TLS, le rotte montate sul mux, il listener.

package runtime

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"

	risorse "promatec/cockpit"
	"promatec/cockpit/internal/platform/coda"
	"promatec/cockpit/internal/platform/config"
	"promatec/cockpit/internal/platform/rete"
	"promatec/cockpit/internal/transport/web"
	"promatec/cockpit/internal/transport/workerapi"
)

// PreparaTLS prepara il materiale del certificato, e lo fa PRIMA che le rotte esistano.
//
// L'impronta del certificato finisce in due posti che servono a farlo usare: il log dell'avvio e la
// pagina Postazioni, da cui si scarica il worker.toml che la contiene gia'. Un'impronta che va
// copiata a mano da un file di certificato e' un'impronta che nessuno verifica (voce 2.4).
//
// Restituisce nil quando il TLS non c'e': non e' un errore, e' un server in chiaro, e chi ascolta lo
// sa dal materiale che non riceve.
func PreparaTLS(cfg *config.Config, log *slog.Logger) (*rete.Materiale, error) {
	if cfg.Server.DaRigaDiComando {
		// Gli avviatori di scripts/avvio-rete: chi apre cockpit.toml ci trova un'altra rete, e deve
		// sapere da dove viene questa senza leggere il comando con cui e' partito il processo.
		log.Info("rete dalla riga di comando: le voci di rete di [server] nel file non valgono per questo avvio",
			"indirizzo", cfg.Server.Indirizzo, "url_pubblico", cfg.Server.URLPubblico, "tls_cert", cfg.Server.TLSCert,
			"reti_consentite", cfg.Server.Reti)
	}
	var materiale *rete.Materiale
	if cfg.ETLS() {
		nomi := cfg.Server.TLSNomi
		if len(nomi) == 0 {
			nomi = rete.NomiPredefiniti(cfg.Server.Indirizzo)
		}
		// L'host di url_pubblico entra nei nomi del certificato (7C.1, P1): e' quello che il browser
		// digita, e un certificato che non lo nomina da' un avviso in piu' da ignorare ogni volta.
		if h := cfg.HostPubblico(); h != "" {
			nomi = append(nomi, h)
		}
		m, err := rete.Prepara(cfg.Server.TLSCert, cfg.Server.TLSKey, nomi)
		if err != nil {
			return nil, err
		}
		materiale = m
		if materiale.Generato {
			log.Warn("certificato TLS generato ora (autofirmato)", "cert", cfg.Server.TLSCert, "chiave", cfg.Server.TLSKey,
				"nomi", materiale.Nomi, "scade", materiale.Scadenza.Format("02/01/2006"),
				"nota", "va copiato nei worker.toml come impronta = ...")
		}
		log.Info("TLS attivo", "impronta", materiale.Impronta, "scade", materiale.Scadenza.Format("02/01/2006"))
		if h := cfg.HostPubblico(); h != "" && !materiale.Copre(h) {
			log.Warn("il certificato non nomina l'host di [server].url_pubblico: il browser dira' che il nome non corrisponde",
				"host", h, "nomi_del_certificato", materiale.Nomi,
				"rimedio", "aggiungerlo a [server].tls_nomi e cancellare cert/key per rigenerarli (poi rigenerare i pacchetti: cambia l'impronta)")
		}
	} else if !config.SuLoopback(cfg.Server.Indirizzo) {
		// Ci si arriva solo con consenti_lan_in_chiaro: la configurazione lo rifiuta da sola. Va
		// ricordato a ogni avvio, perché una scelta presa una volta diventa lo stato normale.
		log.Warn("IN CHIARO SULLA LAN: posta, token dei worker e cookie di sessione viaggiano leggibili",
			"indirizzo", cfg.Server.Indirizzo, "nota", "consenti_lan_in_chiaro = true; togliere la riga e indicare tls_cert/tls_key")
	}
	if cfg.Server.TokenWorker != "" {
		log.Warn("[server].token_worker è ancora nel file e non autentica più niente (voce 2.4): togliere la riga",
			"nota", "ogni worker usa il token della sua [[worker]], e il server lo riconosce per sha256")
	}
	return materiale, nil
}

// Ascolta monta le rotte e mette il server in ascolto. Si torna da qui solo quando il contesto si
// chiude o il listener si rompe.
//
// `percorsoLog` e `livello` arrivano da fuori per una ragione sola: finiscono nella riga «cockpit in
// ascolto», che dice dove si scrive il log e a che livello. Ricavarli qui vorrebbe dire decidere una
// seconda volta una cosa gia' decisa.
func Ascolta(ctx context.Context, cfg *config.Config, s *Servizi, materiale *rete.Materiale, log *slog.Logger, percorsoLog string, livello slog.Level) error {
	templ, _ := fs.Sub(risorse.FS, "web/templates")
	static, _ := fs.Sub(risorse.FS, "web/static")
	ws := &web.Server{Pool: s.Pool, Log: log, NAS: s.Scrittore, Ingest: s.Ingest, Templ: templ, Static: static,
		IntervalloSync: s.IntervalloSync, Sync: s.OpzioniSync, SyncAperturaInbox: s.SyncApertura, Modalita: cfg.Server.Modalita,
		TLS: materiale, Indirizzo: cfg.Server.Indirizzo, URLPubblico: cfg.Server.URLPubblico, Workers: risorse.FS, Agente: s.Agente,
		Ricognitore: s.Ricognitore, Staging: s.RadiceStaging, MotoreA: s.MotoreA,
		Analizzatore: coda.Analizzatore{Versione: cfg.Analisi.Versione, Parametri: cfg.Analisi.Parametri}}
	if err := ws.Init(); err != nil {
		return err
	}
	if cfg.Server.URLPubblico == "" {
		log.Info("[server].url_pubblico non dichiarato: i pacchetti delle postazioni useranno l'indirizzo derivato dal bind",
			"url", ws.URLServer(), "nota", "sulla LAN conviene dichiarare un IPv4 stabile o un FQDN che si risolve da ogni PC")
	} else {
		log.Info("indirizzo pubblico del server", "url", ws.URLServer())
	}
	wa := &workerapi.Server{
		Pool: s.Pool, Log: log, Ingest: s.Ingest,
		Staging: s.RadiceStaging, CasellaDefault: cfg.Outlook.CasellaDefault,
		Analizzatore: coda.Analizzatore{Versione: cfg.Analisi.Versione, Parametri: cfg.Analisi.Parametri},
		MaxUpload:    int64(cfg.Server.MaxUploadMB) << 20,
	}
	// Il caricamento interno del Fascicolo (B8.7) fa la strada dei file che arrivano dai worker: la
	// proposta e l'analisi le decide la stessa funzione del workerapi.
	ws.Pipeline, ws.MaxCaricamento = wa, wa.MaxUpload
	// L'esecutore interno parte QUI e non prima, perche' gli serve chi sa scompattare un archivio, e
	// quel qualcuno e' la stessa parte che riceve i risultati dei worker: dal blocco 4A l'estrazione
	// di uno zip e' un job, non un pezzo della richiesta HTTP con cui il download viene consegnato.
	s.Esecutore.Archivi = wa
	s.Avvia(ctx)
	mux := http.NewServeMux()
	ws.Registra(mux)
	wa.Registra(mux)

	// CSRF (voce 2.5): il perché sta su web.ProtezioneCSRF, che è la stessa funzione che i test
	// montano — una protezione che in produzione e nei test passa da due strade diverse è una
	// protezione che una delle due strade prima o poi perde.
	srv := &http.Server{Addr: cfg.Server.Indirizzo, Handler: web.ProtezioneCSRF(logga(log, mux)), ReadHeaderTimeout: 10 * time.Second}
	// Il listener si apre qui e non dentro ListenAndServe, perche' [server].reti_consentite lo
	// avvolge: il filtro deve stare prima del TLS, e ListenAndServe non lascia metterci niente in mezzo.
	ln, err := net.Listen("tcp", cfg.Server.Indirizzo)
	if err != nil {
		return fmt.Errorf("ascolto su %s: %w", cfg.Server.Indirizzo, err)
	}
	if len(cfg.Server.Reti) > 0 {
		ln = rete.SoloDalleReti(ln, cfg.Server.Reti, func(da netip.Addr) {
			log.Warn("connessione rifiutata: non viene da [server].reti_consentite", "da", da, "reti", cfg.Server.Reti)
		})
	}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(sctx)
	}()
	reti := "tutte"
	if len(cfg.Server.Reti) > 0 {
		reti = fmt.Sprint(cfg.Server.Reti)
	}
	log.Info("cockpit in ascolto", "indirizzo", cfg.Schema()+"://"+cfg.Server.Indirizzo, "reti_consentite", reti, "staging", s.RadiceStaging,
		"max_upload_mb", cfg.Server.MaxUploadMB, "log", percorsoLog, "livello", livello)
	if materiale != nil {
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{materiale.Certificato}, MinVersion: tls.VersionTLS12}
		// I due percorsi sono vuoti di proposito: il certificato è già in TLSConfig, e rileggerlo dal
		// disco qui vorrebbe dire poter servire qualcosa di diverso da ciò di cui abbiamo scritto
		// l'impronta nel log.
		err = srv.ServeTLS(ln, "", "")
	} else {
		err = srv.Serve(ln)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func logga(log *slog.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		h.ServeHTTP(w, r)
		if r.URL.Path != "/api/v1/jobs/claim" && r.URL.Path != "/healthz" {
			log.Debug("http", "m", r.Method, "p", r.URL.Path, "ms", time.Since(t).Milliseconds())
		}
	})
}
