package web

import (
	"time"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/valutazione"
)

// motore_a_passaggio.go: la copia di app/bancoa/passaggio.go (piano 6.4.6, «Il passaggio»; R42 B, R53 B), per la
// Distinta nuova del motore A (giro 5, A1d; BE-08, T-A1d-04): transport non importa app (internal/README.md:183), e
// spostarla in core è l'opzione D scartata (R53 B). Si copia campo per campo, senza scelte; un campo nuovo da una
// parte si aggiunge nelle due copie. Le guardie sono in motore_a_passaggio_test.go: i byte JSON di sorgente e copia, e
// i corpi delle funzioni uguali a quelli del banco a meno dei nomi (maIn* per in*, maCopia* per copia*, valutazione per
// valut; RK-03).
//
// Il passaggio dai record piatti di valutazione ai DTO di confronto: i due pacchetti non si importano, chi chiama copia
// campo per campo, senza scelte. Le scelte (quali basi, quale lettura del vecchio, se le revisioni sono confrontabili)
// le ha già fatte valutazione, una volta sola.

// maInFile: un allegato del thread, il vecchio e il nuovo.
func maInFile(f valutazione.FileConfrontabile) confronto.File {
	return confronto.File{
		AllegatoID: f.AllegatoID,
		Vecchio:    maInVecchio(f.Vecchio),
		Nuovo:      maInNuovo(f.Nuovo),
	}
}

// maInFiles: tutti gli allegati del thread, nell'ordine di valutazione (confronto li riordina per AllegatoID).
func maInFiles(ff []valutazione.FileConfrontabile) []confronto.File {
	out := make([]confronto.File, 0, len(ff))
	for _, f := range ff {
		out = append(out, maInFile(f))
	}
	return out
}

// maInVecchio: il vecchio di un allegato, con i 19 campi di CP.2: CodiceLettoBase subito dopo CodiceLetto (F0-19),
// CodiceLettoMarcatore subito dopo CodiceLettoBase (il gemello di R114: il marcatore della lettura del vecchio motore,
// che la misura conta a parte in PrimaMarcatore e MarcatoreSoloDaUnLato; senza la copia un marcatore del solo codice
// deciso conterebbe come «da un lato solo») e RevisioneDa subito dopo CodiceLettoMarcatore (il gemello di R113 B
// ratificata, E2 §2.6: da dove viene la revisione vecchia interpretata, «codice» o «colonna»; senza la copia
// l'indicatore di revisione perderebbe la provenienza, e il banco non dividerebbe per stato la colonna letta).
func maInVecchio(v valutazione.VecchioPiatto) confronto.Vecchio {
	return confronto.Vecchio{
		Stato:                v.Stato,
		Fonte:                v.Fonte,
		Codice:               v.Codice,
		Rev:                  v.Rev,
		Base:                 v.Base,
		Marcatore:            v.Marcatore,
		Revisione:            v.Revisione,
		Leggibile:            v.Leggibile,
		MotivoLettura:        v.MotivoLettura,
		CodiceLetto:          v.CodiceLetto,
		CodiceLettoBase:      v.CodiceLettoBase,
		CodiceLettoMarcatore: v.CodiceLettoMarcatore,
		RevisioneDa:          v.RevisioneDa,
		Componente:           maCopiaUUID(v.Componente),
		Documento:            maCopiaUUID(v.Documento),
		ComponenteProposta:   maCopiaUUID(v.ComponenteProposta),
		SostituitoDa:         maCopiaUUID(v.SostituitoDa),
		DecisoIl:             maCopiaTempo(v.DecisoIl),
		Destinazione:         maCopiaStringhe(v.Destinazione),
	}
}

// maInNuovo: il nuovo di un allegato.
func maInNuovo(n valutazione.NuovoPiatto) confronto.Nuovo {
	out := confronto.Nuovo{
		Valutato:        n.Valutato,
		Motivo:          n.Motivo,
		Basi:            maCopiaStringhe(n.Basi),
		Collocazione:    n.Collocazione,
		Associazione:    n.Associazione,
		Disponibilita:   n.Disponibilita,
		Revisione:       n.Revisione,
		Revisioni:       n.Revisioni,
		MotivoRevisioni: n.MotivoRevisioni,
	}
	if n.Candidati != nil {
		out.Candidati = make([]confronto.Candidato, 0, len(n.Candidati))
		for _, c := range n.Candidati {
			out.Candidati = append(out.Candidati, maInCandidato(c))
		}
	}
	return out
}

// maInCandidato: un candidato di ancoraggio.
func maInCandidato(c valutazione.CandidatoPiatto) confronto.Candidato {
	return confronto.Candidato{
		Target:   c.Target,
		Livello:  c.Livello,
		Base:     c.Base,
		Autorita: c.Autorita,
		Radici:   maCopiaStringhe(c.Radici),
	}
}

// maInProdotti: i candidati prodotto della mail, nell'ordine di valutazione.
func maInProdotti(pp []valutazione.ProdottoConfrontabile) []confronto.ProdottoNuovo {
	out := make([]confronto.ProdottoNuovo, 0, len(pp))
	for _, p := range pp {
		out = append(out, maInProdotto(p))
	}
	return out
}

// maInProdotto: un candidato prodotto.
func maInProdotto(p valutazione.ProdottoConfrontabile) confronto.ProdottoNuovo {
	var q *int
	if p.Quantita != nil {
		v := *p.Quantita
		q = &v
	}
	return confronto.ProdottoNuovo{
		CodiceRichiesto: p.CodiceRichiesto,
		Base:            p.Base,
		Fase:            p.Fase,
		Quantita:        q,
		QuantitaDaCella: p.QuantitaDaCella,
	}
}

// ---- le copie dei campi (app/bancoa/banco.go, «le copie dei campi, per passaggio.go») ----

func maCopiaUUID(u *uuid.UUID) *uuid.UUID {
	if u == nil {
		return nil
	}
	v := *u
	return &v
}

func maCopiaTempo(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

func maCopiaStringhe(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string{}, s...)
}
