package bancoa

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"promatec/cockpit/internal/platform/jsoncanonico"
)

// L1 — A1c-L1-26: il rapporto delle modalità di A1c (versione 3; 6.4.9, «Il rapporto»; A1c.md §5.5; R44): la versione
// fissa; i file rapporto-exports.json e .txt scritti in modo atomico (.tmp, poi Rename), in JSON canonico; una cartella
// dentro il modulo rifiutata; la prima scrittura prima della lettura della fotografia, con «ESITO: NON ESEGUITO —
// lettura non cominciata»; l'esito in testa e la lista dei controlli eseguiti e no; a video solo conteggi, ID e percorsi,
// mai i testi delle mail; due corse danno gli stessi byte (nessun orologio); le sezioni senza_caso, censimento,
// correzioni_manuali, profilo_limiti e prodotti (solo informazione, «non calcolata» dove manca una sezione: T-12).
//
// I clienti di questi test sono inventati: vedi scena_banco_test.go.

// TestVersioneRapportoBancoFissa: il rapporto dei modi di A1c ha la versione 3 (T-B6-03); quello di A1a resta alla 2.
func TestVersioneRapportoBancoFissa(t *testing.T) {
	if VersioneRapportoBanco != 3 || VersioneRapporto != 2 {
		t.Fatalf("versioni %d e %d: cambiarle vuol dire riscrivere questa prova («Riscritta per …»)", VersioneRapportoBanco, VersioneRapporto)
	}
}

// spia: uno scrittore che, quando arriva la riga «rapporto: …», legge il rapporto già scritto: così si vede la prima
// scrittura, che il banco fa prima di leggere la fotografia.
type spia struct {
	bytes.Buffer
	primo []byte
}

func (s *spia) Write(p []byte) (int, error) {
	if riga := string(p); strings.HasPrefix(riga, "rapporto: ") && s.primo == nil {
		percorso := strings.TrimSpace(strings.TrimPrefix(riga, "rapporto: "))
		s.primo, _ = os.ReadFile(strings.TrimSuffix(percorso, ".json") + ".txt")
	}
	return s.Buffer.Write(p)
}

func TestRapportoBancoScrittoESenzaTestiDiMail(t *testing.T) {
	s := preparaBanco(t, mutaBanco{})
	var out spia
	r, err := EseguiBanco(context.Background(), s.opzioniExport(true), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out.primo), "ESITO: NON ESEGUITO — lettura non cominciata\n") {
		t.Fatalf("la prima scrittura, prima della lettura: %q", out.primo)
	}
	voci, err := os.ReadDir(s.uscita)
	if err != nil {
		t.Fatal(err)
	}
	var nomi []string
	for _, v := range voci {
		nomi = append(nomi, v.Name())
	}
	if strings.Join(nomi, " ") != "rapporto-exports.json rapporto-exports.txt" {
		t.Fatalf("file nella cartella dei rapporti: %v (nessun .tmp deve restare)", nomi)
	}
	js, _ := os.ReadFile(filepath.Join(s.uscita, "rapporto-exports.json"))
	can, err := jsoncanonico.Codifica(json.RawMessage(js))
	if err != nil || !bytes.Equal(can, js) {
		t.Fatalf("il JSON del rapporto non è canonico: %v", err)
	}
	txt, _ := os.ReadFile(filepath.Join(s.uscita, "rapporto-exports.txt"))
	// Il primo controllo non eseguito della scena è quello delle letture (target_presente senza i componenti): il n.1
	// senza i finiti è fuori perimetro con gli export (D-R116 F), e non lo rende più NON ESEGUITO.
	if !strings.HasPrefix(string(txt), "ESITO: NON ESEGUITO — letture_e_invarianti_dei_file: 1 parti non verificabili") ||
		!strings.Contains(string(txt), "\nmodalità: exports\n") || string(txt) != r.Testo() {
		t.Fatalf("il riepilogo scritto:\n%s", txt)
	}
	video := out.String()
	righe := strings.Split(video, "\n")
	if !strings.HasPrefix(righe[0], "sorgente: export ") || !strings.HasPrefix(righe[1], "nessun database aperto; fatti dagli export") ||
		!strings.HasPrefix(righe[2], "rapporto: ") || !strings.HasPrefix(righe[3], "ESITO: ") {
		t.Fatalf("le prime righe a video:\n%s", strings.Join(righe[:4], "\n"))
	}
	if !strings.HasSuffix(video, "scritture: solo "+filepath.Join(s.uscita, "rapporto-exports.json")+" (e il riepilogo .txt accanto); nessun database aperto\n") {
		t.Errorf("l'ultima riga a video: %q", righe[len(righe)-2])
	}
	if !strings.Contains(string(txt), "senza_risposta 3") || strings.Contains(string(txt), "mancante") {
		t.Errorf("nel riepilogo «mancante» si scrive «senza risposta» (R30 c):\n%s", txt)
	}
	for _, testo := range []string{"vi giro la richiesta", "In allegato i disegni", "Per il PN", "<table>", "acquisti@acme.example", "Ufficio acquisti"} {
		if strings.Contains(video, testo) || strings.Contains(string(txt), testo) {
			t.Errorf("a video o nel riepilogo c'è un testo di mail: %q", testo)
		}
	}
	// Le sezioni del 6.4.9 e del contratto.
	if len(r.SenzaCaso) != 1 || r.SenzaCaso[0].Caso != "ACME-SCENARIO" || len(r.Censimento) != 2 || len(r.CorrezioniManuali) != 1 ||
		r.ProfiloLimiti == nil || r.ProfiloLimiti.VersioneLimiti != "limiti-acme-1" || r.Prodotti == nil || r.Prodotti.Calcolata {
		t.Fatalf("sezioni: senza_caso %+v, censimento %d, correzioni %d, profilo %+v, prodotti %+v",
			r.SenzaCaso, len(r.Censimento), len(r.CorrezioniManuali), r.ProfiloLimiti, r.Prodotti)
	}
	for _, th := range r.Prodotti.Thread {
		for _, p := range th.Prodotti {
			for _, a := range p.Assi {
				if a.Calcolato && a.Asse != "smistamento" {
					t.Errorf("con gli export senza i componenti l'asse %s non è calcolato (T-12): %+v", a.Asse, a)
				}
			}
		}
	}
	if r.Fotografia == nil || len(r.Fotografia.Diagnostiche) == 0 || len(r.Fotografia.Limiti) == 0 {
		t.Errorf("le diagnostiche della fotografia e i limiti degli export stanno nel rapporto (D-V1-2): %+v", r.Fotografia)
	}
	var nomiControlli []string
	for _, c := range r.Controlli {
		nomiControlli = append(nomiControlli, c.Nome+":"+c.Stato)
	}
	if !strings.Contains(strings.Join(nomiControlli, " "), "export:eseguito") || !strings.Contains(strings.Join(nomiControlli, " "), "casi_contratto:eseguito") {
		t.Errorf("controlli: %v", nomiControlli)
	}
}

// TestRapportoBancoStabile: due corse sulla stessa scena danno gli stessi byte (nessun orologio, nessun ordine di mappa).
func TestRapportoBancoStabile(t *testing.T) {
	s := preparaBanco(t, mutaBanco{})
	var primo []byte
	for i := 0; i < 2; i++ {
		if _, err := EseguiBanco(context.Background(), s.opzioniExport(true), nil); err != nil {
			t.Fatal(err)
		}
		js, err := os.ReadFile(filepath.Join(s.uscita, "rapporto-exports.json"))
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			primo = js
		} else if !bytes.Equal(primo, js) {
			t.Fatal("due corse, due rapporti diversi")
		}
	}
}

// TestScriviRapportoBancoFuoriDalModulo: una cartella dentro il modulo è rifiutata e non si crea; una modalità di A1a
// non ha il nome del rapporto di A1c (ScriviRapporto di A1a resta com'è: F0-01 d).
func TestScriviRapportoBancoFuoriDalModulo(t *testing.T) {
	r := RapportoBanco{VersioneRapporto: VersioneRapportoBanco, Modalita: ModalitaDSN, Esito: EsitoNonEseguito, Motivo: "prova"}
	dentro := filepath.Join("testdata", "uscita-non-ammessa")
	if _, err := scriviRapportoBanco(dentro, r); err == nil {
		t.Fatal("cartella dentro il modulo accettata")
	}
	if _, err := os.Stat(dentro); !os.IsNotExist(err) {
		t.Fatalf("la cartella rifiutata è stata creata: %v", err)
	}
	r.Modalita = ModalitaCasi
	if _, err := scriviRapportoBanco(t.TempDir(), r); err == nil {
		t.Fatal("una modalità di A1a con il rapporto di A1c")
	}
	r.Modalita = ModalitaDSN
	p, err := scriviRapportoBanco(filepath.Join(t.TempDir(), "banco"), r)
	if err != nil || filepath.Base(p) != "rapporto-dsn.json" {
		t.Fatalf("rapporto dsn: %q %v", p, err)
	}
	if txt, _ := os.ReadFile(strings.TrimSuffix(p, ".json") + ".txt"); !strings.HasPrefix(string(txt), "ESITO: NON ESEGUITO — prova\n") {
		t.Fatalf("prima riga: %q", txt)
	}
}
