package domain

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCartellaThread(t *testing.T) {
	d := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	casi := []struct{ cliente, cognome, oggetto, atteso string }{
		{"ACME", "Rossi", "Supporto cofano", `ACME\WIP\2026 09 08 Rossi Supporto cofano`},
		{"LANDINI ARGO", "Bianchi", "RE: R: RICHIESTA D'OFFERTA 123456789", `LANDINI ARGO\WIP\2026 09 08 Bianchi RICHIESTA D'OFFERTA 123456789`},
		{"ACME", "", "Staffa: nuova/rev. 2?", `ACME\WIP\2026 09 08 Staffa nuova rev. 2`},
		{"ACME", "Rossi", "", `ACME\WIP\2026 09 08 Rossi senza nome`},
		{"ACME", "Rossi", strings.Repeat("x", 100), `ACME\WIP\2026 09 08 Rossi ` + strings.Repeat("x", 60)},
		{"ACME", "Rossi", "Fine con punto.", `ACME\WIP\2026 09 08 Rossi Fine con punto`},
	}
	for _, c := range casi {
		if got := CartellaThread(c.cliente, d, c.cognome, c.oggetto); got != c.atteso {
			t.Errorf("CartellaThread(%q,%q,%q) = %q, atteso %q", c.cliente, c.cognome, c.oggetto, got, c.atteso)
		}
	}
}

func TestPathDocumento(t *testing.T) {
	disegni := LayoutDocumento{Sottocartella: "ELENCO DISEGNI", PerCodice: true}
	radice := LayoutDocumento{Sottocartella: "", PerCodice: false}
	casi := []struct {
		l         LayoutDocumento
		perCodice bool
		codice    string
		nome      string
		atteso    string
	}{
		{disegni, true, "6674611A", "6674611A_4.pdf", `ELENCO DISEGNI\6674611A\6674611A_4.pdf`},
		{disegni, false, "6674611A", "6674611A_4.pdf", `ELENCO DISEGNI\6674611A_4.pdf`},
		{disegni, true, "", "assieme.STEP", `ELENCO DISEGNI\assieme.step`},
		{radice, true, "6674611A", "SO 5467.pdf", `SO 5467.pdf`},
		{LayoutDocumento{"OFFERTE FORNITORI", false}, true, "", "verniciatura?.pdf", `OFFERTE FORNITORI\verniciatura.pdf`},
	}
	for _, c := range casi {
		if got := PathDocumento(c.l, c.perCodice, c.codice, c.nome); got != c.atteso {
			t.Errorf("PathDocumento(%v,%v,%q,%q) = %q, atteso %q", c.l, c.perCodice, c.codice, c.nome, got, c.atteso)
		}
	}
}

// TestUNC prova il ramo Windows. Sono le stesse asserzioni di prima, con la sola differenza che
// chiamano uncWindows invece di UNC: così girano su qualunque sistema, e il comportamento che va in
// produzione su Windows resta dimostrato anche da una CI che gira su Linux.
func TestUNC(t *testing.T) {
	if got := uncWindows(`\\nas01\TECNICO - PREVENTIVI\PREVENTIVI DA FARE`, `ACME\WIP\x`); got != `\\nas01\TECNICO - PREVENTIVI\PREVENTIVI DA FARE\ACME\WIP\x` {
		t.Errorf("UNC corto: %q", got)
	}
	if got := uncWindows(`C:\promatec\_nas_test\PREVENTIVI DA FARE\`, `\ACME\WIP\x`); got != `C:\promatec\_nas_test\PREVENTIVI DA FARE\ACME\WIP\x` {
		t.Errorf("UNC locale: %q", got)
	}
	lungo := uncWindows(`\\nas01\radice`, strings.Repeat(`cartella lunga\`, 20)+"file.pdf")
	if !strings.HasPrefix(lungo, `\\?\UNC\nas01\radice\`) {
		t.Errorf("UNC lungo di rete senza prefisso: %q", lungo)
	}
	lungoLocale := uncWindows(`C:\radice`, strings.Repeat(`cartella lunga\`, 20)+"file.pdf")
	if !strings.HasPrefix(lungoLocale, `\\?\C:\radice\`) {
		t.Errorf("UNC lungo locale senza prefisso: %q", lungoLocale)
	}
	if strings.Count(lungo, `\\?\`) != 1 || strings.HasPrefix(uncWindows(lungo, "y"), `\\?\\\?\`) {
		t.Errorf("prefisso duplicato: %q", uncWindows(lungo, "y"))
	}
}

// TestUNCPosix prova l'altro ramo: la forma canonica con '\' diventa un percorso del sistema, e non
// si aggiunge nessun prefisso long-path, che fuori da Windows non vuol dire niente.
func TestUNCPosix(t *testing.T) {
	if got := uncPosix(`/mnt/nas/PREVENTIVI DA FARE`, `ACME\WIP\x`); got != `/mnt/nas/PREVENTIVI DA FARE/ACME/WIP/x` {
		t.Errorf("posix corto: %q", got)
	}
	if got := uncPosix(`/mnt/nas/PREVENTIVI/`, `\ACME\WIP\x`); got != `/mnt/nas/PREVENTIVI/ACME/WIP/x` {
		t.Errorf("posix con separatori in eccesso: %q", got)
	}
	// Una radice lasciata in forma Windows non deve produrre un nome di file unico.
	if got := uncPosix(`C:\radice`, `ACME\x`); got != `C:/radice/ACME/x` {
		t.Errorf("posix con radice in forma Windows: %q", got)
	}
	lungo := uncPosix(`/mnt/nas`, strings.Repeat(`cartella lunga\`, 20)+"file.pdf")
	if strings.Contains(lungo, `\`) || strings.Contains(lungo, `?`) {
		t.Errorf("posix lungo: nessun backslash e nessun prefisso long-path, trovato %q", lungo)
	}
}

// TestUNCSeguelLaPiattaforma verifica che UNC scelga il ramo giusto per il sistema su cui gira: è
// ciò che rende utilizzabile il percorso che poi finisce in os.Stat e os.MkdirAll.
func TestUNCSeguelLaPiattaforma(t *testing.T) {
	got := UNC(`radice`, `ACME\x`)
	atteso := uncPosix(`radice`, `ACME\x`)
	if os.PathSeparator == '\\' {
		atteso = uncWindows(`radice`, `ACME\x`)
	}
	if got != atteso {
		t.Errorf("UNC = %q, atteso %q per %s", got, atteso, runtime.GOOS)
	}
	if filepath.Base(got) != "x" {
		t.Errorf("il percorso non è utilizzabile dal sistema: filepath.Base(%q) = %q", got, filepath.Base(got))
	}
}

func TestEstraiCodici(t *testing.T) {
	got := EstraiCodici("RICHIESTA D'OFFERTA 123456789 - codice 6674611A rev 4", "vedi allegato 6674611A_4.pdf e il 12/09/2026 alle 10:30")
	atteso := []string{"123456789", "6674611A", "6674611A_4"}
	if !reflect.DeepEqual(got, atteso) {
		t.Errorf("EstraiCodici = %v, atteso %v", got, atteso)
	}
	c, r := CodiceRev("6674611A_4")
	if c != "6674611A" || r != "4" {
		t.Errorf("CodiceRev = %q %q", c, r)
	}
}

func TestRilevaPortale(t *testing.T) {
	corpo := "Buongiorno,\r\nvi abbiamo caricato sul portale i CAD dei codici 6674611A e 6674612B. Grazie.\r\nCordiali saluti"
	rif := RilevaPortale(corpo)
	if len(rif) != 1 || len(rif[0].Codici) != 2 {
		t.Fatalf("RilevaPortale = %+v", rif)
	}
}

func TestRilevaScadenza(t *testing.T) {
	rifer := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	d, ok := RilevaScadenza("Vi chiediamo cortesemente risposta entro il 15/09/2026.", rifer)
	if !ok || d.Day() != 15 || d.Month() != 9 {
		t.Errorf("RilevaScadenza = %v %v", d, ok)
	}
	if _, ok := RilevaScadenza("inviato il 01/09/2026", rifer); ok {
		t.Errorf("data senza parola chiave non deve essere una scadenza")
	}
}

func TestTriage(t *testing.T) {
	e := Triage(IngressoTriage{Oggetto: "RFQ 6674611A", Corpo: "in allegato i disegni", NomiAllegati: []string{"6674611A_4.pdf"}, BuyerNoto: true, Direzione: "entrata"})
	// 80 e non piu' 95: un PDF non vale piu' «allegato tecnico» (25) ma «allegato di tipo da
	// determinare» (10), perche' nessuno l'ha aperto. L'esito non cambia, ed e' l'esito che conta.
	if e.Esito != "nuova_rfq" || e.Confidenza < 70 {
		t.Errorf("triage RFQ = %+v", e)
	}
	e = Triage(IngressoTriage{Oggetto: "Newsletter settembre", Corpo: "offerte del mese", Direzione: "entrata"})
	if e.Esito != "ignora" {
		t.Errorf("triage newsletter = %+v", e)
	}
	e = Triage(IngressoTriage{Oggetto: "R: Tirocinio per tesi", Corpo: "mi ricordo bene, Bernardo", NomiAllegati: []string{"Screenshot_20260903_090239_Chrome.jpg"}, Direzione: "entrata"})
	if e.Esito != "ignora" || len(e.Codici) != 0 {
		t.Errorf("triage falso positivo rdo/screenshot = %+v", e)
	}
	e = Triage(IngressoTriage{Direzione: "uscita", Oggetto: "RFQ"})
	if e.Esito != "ignora" {
		t.Errorf("triage uscita = %+v", e)
	}

	// Voce 2.1, D10: una mail INTERNA è in uscita (parte da un nostro indirizzo) ma non è roba già
	// vista da noi: «ti giro questa richiesta» è uno dei modi in cui una RFQ arriva sul tavolo.
	// Ignorarla per il mittente vorrebbe dire non proporre niente proprio sui messaggi che un collega
	// ha inoltrato apposta perché qualcuno li guardasse.
	e = Triage(IngressoTriage{Direzione: "uscita", Interno: true, Oggetto: "I: RFQ 6674611A",
		Corpo: "ti giro la richiesta", NomiAllegati: []string{"6674611A_4.pdf"}})
	if e.Esito == "ignora" {
		t.Errorf("triage di una mail interna ignorato per il mittente: %+v", e)
	}
	var detto bool
	for _, m := range e.Motivi {
		if strings.Contains(m, "interna") {
			detto = true
		}
	}
	if !detto {
		t.Errorf("il triage non dice che si tratta di una mail interna: %+v", e.Motivi)
	}
}
