// L'anteprima del motore A all'avvio: l'indice delle regole di [motore_a], le grammatiche e il file dei casi che
// elenca, letti una volta sola (giro 5, A1d; piano 7.4.5; T-A1d-16, T-A1d-38).

package runtime

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"promatec/cockpit/internal/core/inbox/classificazione/motorea"
	"promatec/cockpit/internal/core/registro/regole/grammatica"
	"promatec/cockpit/internal/core/valutazione"
	"promatec/cockpit/internal/platform/config"
	"promatec/cockpit/internal/platform/jsoncanonico"
	"promatec/cockpit/internal/transport/web"
)

// regoleLette: ciò che l'avvio legge dalla cartella dell'indice, prima di ogni controllo di contenuto (piano 7.4.5).
//   - Grammatiche: per nome com'è scritto nell'indice; manca il file che non si legge o che esce dalla cartella, e
//     CompilaInsieme lo dà come regole.file_assente (il cliente è scartato, gli altri restano attivi: par.3.6.5).
//   - Fuori: i nomi dell'indice che escono dalla cartella (R55): non si leggono, e il log lo dice.
//   - Casi, ErroreCasi: il file dei casi dell'indice (R29 b = C), letto e con lo sha256 controllato qui; nil con
//     l'errore, o senza errore se l'indice non lo dichiara.
type regoleLette struct {
	Indice      grammatica.IndiceRegole
	Grammatiche map[string][]byte
	Fuori       []string
	Casi        []byte
	ErroreCasi  error
}

// leggiRegoleMotoreA è la parte che tocca il disco (piano 7.4.5), separata per le prove con una cartella temporanea:
// l'indice (LeggiIndice: limiti, voci, sha256 presenti), poi i file che elenca, relativi alla cartella dell'indice e
// mai fuori (filepath.IsLocal per il motivo, os.Root per i collegamenti che escono: R55). Le barre rovesciate del nome
// valgono come separatori, come nel banco (bancoa.go:355). Lo sha256 delle grammatiche lo controlla CompilaInsieme
// (regole.sha256_discorde); quello del file dei casi lo controlla questa funzione, con la stessa impronta
// (jsoncanonico.Impronta, maiuscole delle cifre indifferenti). Un errore solo per l'indice: illeggibile o rifiutato.
func leggiRegoleMotoreA(indice string) (regoleLette, error) {
	raw, err := os.ReadFile(indice)
	if err != nil {
		return regoleLette{}, fmt.Errorf("indice %s: %w", indice, err)
	}
	ind, err := grammatica.LeggiIndice(raw)
	if err != nil {
		return regoleLette{}, fmt.Errorf("indice %s: %w", indice, err)
	}
	// os.Root (Go 1.24, ReadFile da Go 1.25; il modulo chiede la 1.26): un nome che passa IsLocal ma è un
	// collegamento verso fuori dalla cartella non si apre (T-A1d-16).
	root, err := os.OpenRoot(filepath.Dir(indice))
	if err != nil {
		return regoleLette{}, fmt.Errorf("indice %s: %w", indice, err)
	}
	defer root.Close()
	r := regoleLette{Indice: ind, Grammatiche: map[string][]byte{}}
	for _, v := range ind.Grammatiche {
		nome, ok := nomeLocale(v.File)
		if !ok {
			r.Fuori = append(r.Fuori, v.File)
			continue
		}
		if b, err := root.ReadFile(nome); err == nil {
			r.Grammatiche[v.File] = b
		}
	}
	if c := ind.Casi; c != nil {
		if nome, ok := nomeLocale(c.File); !ok {
			r.ErroreCasi = fmt.Errorf("il file dei casi %q esce dalla cartella dell'indice", c.File)
		} else if b, err := root.ReadFile(nome); err != nil {
			r.ErroreCasi = fmt.Errorf("il file dei casi %q non si legge: %w", c.File, err)
		} else if !strings.EqualFold(jsoncanonico.Impronta(b), c.Sha256) {
			r.ErroreCasi = fmt.Errorf("il file dei casi %q ha uno sha256 diverso da quello dell'indice", c.File)
		} else {
			r.Casi = b
		}
	}
	return r, nil
}

// nomeLocale: il nome di un file dell'indice nella cartella dell'indice; falso se ne esce (R55).
func nomeLocale(f string) (string, bool) {
	n := filepath.FromSlash(strings.ReplaceAll(strings.TrimSpace(f), `\`, "/"))
	return n, n != "" && filepath.IsLocal(n)
}

// preparaMotoreA legge una volta, all'avvio, l'indice delle regole del motore A, le grammatiche e il file dei casi che
// l'indice elenca (R29 b = C); compila le grammatiche con i limiti dell'indice (R43 B) e legge gli ingressi con
// valutazione.LeggiIngressi. Le regole NON vengono da cliente.regole.
//   - Spenta: nessun file aperto, una riga «anteprima motore A spenta», il valore zero.
//   - Indice illeggibile, rifiutato o che non attiva nessuna grammatica: Accesa resta vera, Regole nil, Errore il testo
//     generico; il dettaglio va nel log. Il Cockpit parte lo stesso (piano 7.4.5).
//   - File dei casi assente, rotto, fuori dalla cartella o con sha256 diverso: un avviso e ingressi vuoti (par.3.6.5).
func preparaMotoreA(cfg config.MotoreA, log *slog.Logger) web.AnteprimaMotoreA {
	if !cfg.Anteprima {
		log.Info("anteprima motore A spenta")
		return web.AnteprimaMotoreA{}
	}
	out := web.AnteprimaMotoreA{Accesa: true}
	r, err := leggiRegoleMotoreA(cfg.Regole)
	if err != nil {
		log.Error("anteprima motore A accesa senza regole: l'indice non si legge", "indice", cfg.Regole, "err", err)
		out.Errore = web.ErroreRegoleMotoreA
		return out
	}
	for _, f := range r.Fuori {
		log.Warn("anteprima motore A: un file dell'indice esce dalla sua cartella e non si legge",
			"indice", cfg.Regole, "file", f)
	}
	ins, diag, err := motorea.CompilaInsieme(r.Indice, r.Grammatiche)
	if err != nil {
		log.Error("anteprima motore A accesa senza regole: l'indice non attiva nessuna grammatica",
			"indice", cfg.Regole, "err", err)
		out.Errore = web.ErroreRegoleMotoreA
		return out
	}
	out.Regole = &ins
	// Il tetto (60 s) lo controlla già Limiti.Valida, dentro LeggiIndice.
	out.Limite = time.Duration(r.Indice.Limiti.Anteprima.TempoMassimoMs) * time.Millisecond
	casi := "nessuno"
	switch {
	case r.ErroreCasi != nil:
		log.Warn("anteprima motore A: file dei casi non letto, ingressi vuoti", "indice", cfg.Regole, "err", r.ErroreCasi)
		casi = "non letto"
	case r.Casi != nil:
		in, err := valutazione.LeggiIngressi(r.Casi)
		if err != nil {
			log.Warn("anteprima motore A: file dei casi non valido, ingressi vuoti", "indice", cfg.Regole, "err", err)
			casi = "non valido"
		} else {
			out.Ingressi = in
			casi = jsoncanonico.Impronta(r.Casi)
		}
	}
	// La riga dell'avvio (piano 7.4.5; T-A1d-38): le versioni di valutazione e di motorea, non di confronto, che
	// runtime non importa.
	log.Info("anteprima motore A accesa", "indice", cfg.Regole, "impronta_indice", ins.ImprontaIndice,
		"versione_limiti", r.Indice.Limiti.Versione, "limite", out.Limite, "casi", casi,
		"clienti_attivi", len(ins.Motori), "clienti_scartati", len(ins.Scartati), "scartati", scartatiDi(ins),
		"diagnostiche", len(diag), "versione_valutazione", valutazione.VersioneValutazione,
		"versione_motorea", motorea.VersioneAlgoritmo, "versione_composizione", motorea.VersioneComposizione,
		"versione_revisione_registrata", motorea.VersioneRevisioneRegistrata, "visibile", "solo al ruolo admin")
	return out
}

// scartatiDi: i clienti scartati per la riga dell'avvio, «<uuid> <codice della prima diagnostica>», in ordine di
// stringa (la mappa non si scorre in un ordine). Solo UUID e codici: nessun testo dei clienti nel log.
func scartatiDi(ins motorea.InsiemeRegole) []string {
	out := make([]string, 0, len(ins.Scartati))
	for id, d := range ins.Scartati {
		voce := id.String()
		if len(d) > 0 {
			voce += " " + d[0].Codice
		}
		out = append(out, voce)
	}
	sort.Strings(out)
	return out
}
