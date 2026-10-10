package web

// motore_a.go: la Distinta nuova del motore A (giro 5, A1d; contratto A1d §2, §3.1; piano 7.4.2-7.4.3).

import (
	"context"
	"time"

	"github.com/google/uuid"

	"promatec/cockpit/internal/core/confronto"
	"promatec/cockpit/internal/core/fotorfq"
	"promatec/cockpit/internal/core/inbox/classificazione/motorea"
	"promatec/cockpit/internal/core/valutazione"
)

// ErroreRegoleMotoreA: il testo della pagina quando le regole non si sono caricate (piano 7.6.2). Generico: il dettaglio
// è nel log dell'avvio (app/runtime/motore_a.go).
const ErroreRegoleMotoreA = "Regole del motore A non caricate: il dettaglio è nel log del server."

// AnteprimaMotoreA: che cosa serve alla pagina, preparato una volta all'avvio da app/runtime (piano 7.4.2).
//   - Accesa: [motore_a].anteprima.
//   - Regole: le grammatiche compilate dall'indice, con i limiti dell'indice; nil = non caricate, e Errore dice perché.
//   - Ingressi: gli ingressi dei casi, dal file che l'indice dichiara (R29 b = C); vuoti senza il file.
//   - Limite: il tempo massimo di un calcolo della pagina (limiti.anteprima.tempo_massimo_ms dell'indice, R43 B).
//   - Calcola: nil = il percorso di sempre (calcolaMotoreA); le prove lo sostituiscono con uno che sbaglia, va in panic
//     o non finisce in tempo.
type AnteprimaMotoreA struct {
	Accesa   bool
	Regole   *motorea.InsiemeRegole
	Ingressi valutazione.Ingressi
	Limite   time.Duration
	Errore   string
	Calcola  func(ctx context.Context, thread uuid.UUID) (fotorfq.Fotografia, valutazione.Esito, confronto.Esito, error)
}
