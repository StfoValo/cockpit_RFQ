# Problemi riscontrati installando ed eseguendo il Cockpit su Linux

Relazione di una installazione da zero del repository su una macchina Linux, fatta per verificare
che il server giri davvero fuori da Windows. Riguarda **solo** ambiente, portabilità dei percorsi e
prove: nessuna osservazione sulla logica applicativa, che non è stata toccata né messa in
discussione.

| | |
|---|---|
| Commit provato | `84de8d5` (merge della PR #1, `blocco-7`) |
| Sistema | Linux x86_64 (kernel 7.0.11), nessun Windows disponibile |
| Go | 1.27.1 (la toolchain dichiarata in `go.mod`) |
| PostgreSQL | 18.4 |
| Python | 3.13 |

## Che cosa funziona

Prima dei problemi, perché il contorno è sano:

- `go build ./cmd/cockpit` e `go vet ./...` puliti su Linux, senza modifiche.
- Il server parte, applica le migrazioni `0001`→`0013` **in ordine e una per transazione** su un
  database già popolato, semina da `cockpit.toml` e si mette in ascolto.
- L'aggiornamento incrementale funziona davvero: un database fermo alla `0006` è stato portato alla
  `0013` senza errori e senza perdita di dati.
- `pytest workers` → **62 passati, 9 saltati**.
- Login, `/inbox`, `/cruscotto` rendono correttamente.

## Riepilogo dei problemi

| # | Problema | Effetto su Linux | Gravità |
|---|---|---|---|
| 1 | I percorsi del NAS si compongono sempre con `\` | **12 test rossi**; il controllo di integrità del NAS sbaglia a classificare i documenti | Alta |
| 2 | Le prove E2E del worker richiedono `pywin32` | 2 test rossi salvo conoscere una variabile d'ambiente non documentata | Media |
| 3 | `workers/requirements.txt` non è installabile fuori da Windows | `pip install -r` fallisce del tutto | Bassa |

---

## 1. I percorsi del NAS si compongono sempre con `\` (12 test rossi)

### Sintomo

Su Linux falliscono 12 prove, nove in `internal/jobs` e tre in `internal/web`:

```
--- FAIL: TestAllineaRifiutaUnConflitto
    integrita_db_test.go:321: l'errore non dice che si tratta di un conflitto:
    il file sul NAS non si legge: open /tmp/TestAllineaRifiutaUnConflitto.../002\ACMEE\WIP\2026 09 17 prova\ELENCO DISEGNI\disegno.pdf:
    no such file or directory

--- FAIL: TestUnFileGiaGiustoNonEeDaCopiare
    integrita_test.go:117: problema = "in_attesa", atteso gia_presente:
    il file e' li' ed e' byte per byte quello giusto
```

Si legge nel percorso: dentro una directory POSIX ci sono dei `\`. Il sistema non li considera
separatori, quindi l'intera stringa diventa **un solo nome di file**, che ovviamente non esiste.

Elenco completo:

- `internal/jobs`: `TestInCodaRiaccodatoDiventaScritto`, `TestFileAssenteMaDatabaseScrittoVieneSegnalato`,
  `TestHashDiversoEeUnConflittoEIlFileNonSiTocca`, `TestFileGiaCorrettoNienteCopiaInutile`,
  `TestAllineaRifiutaUnConflitto`, `TestUnFileConUnAltroContenutoEeUnConflitto`,
  `TestUnFileGiaGiustoNonEeDaCopiare`, `TestUnDocumentoScrittoConIlSuoFileNonEeUnProblema`,
  `TestUnaCartellaAlPostoDelFileSiDice`
- `internal/web`: `TestLaSchermataDiceSeHaMaiGuardato`, `TestUnConflittoNonSiRiaccodaEIlFileNonSiTocca`,
  `TestUnFileGiaCorrettoSiAllineaSenzaCopiare`

### Causa

Una sola funzione: `domain.UNC` in `internal/domain/path.go`.

```go
func UNC(radice, relativo string) string {
	radice = strings.TrimRight(radice, `\`)
	p := radice + `\` + strings.TrimLeft(relativo, `\`)
	...
}
```

`UNC` è l'unico punto in cui la forma canonica relativa (quella prodotta da `CartellaThread` e
`PathDocumento`, memorizzata in database, che per convenzione usa `\` — e giustamente non si tocca)
diventa un percorso su cui il server fa I/O vero. Compone sempre con `\` e aggiunge sempre il
prefisso long-path `\\?\`, che sono due cose corrette su Windows e prive di senso altrove.

Tutte le operazioni sul filesystem passano di lì — `internal/nas/nas.go` (`os.MkdirAll`, `os.Stat`,
`os.Rename`, `os.Create`) e `internal/jobs/integrita.go` (`os.Stat`) — quindi il difetto è
concentrato in un punto solo. Da notare che `filepath.Dir` e `filepath.Join`, usati subito dopo in
`nas.go`, sono già indipendenti dal sistema: cominciano a funzionare da soli non appena il
separatore che ricevono è quello giusto.

Le altre composizioni con `\` che si incontrano leggendo il codice — `percorsoDocumento` in
`integrita.go:325` e la riga `nas.go:62` — **non sono difetti**: uniscono due percorsi *relativi*
per ottenere ancora un percorso relativo in forma canonica, che solo dopo passa da `UNC`. Vanno
lasciate come sono.

### Perché conta oltre ai test

`internal/jobs/integrita.go` è il controllo che verifica se quanto scritto in database corrisponde
ai file davvero presenti sul NAS. Su Linux non trova mai nulla, quindi classifica come «in attesa» o
«file illeggibile» documenti che sono al loro posto e corretti. È esattamente il controllo che non
conviene avere silenziosamente cieco.

Conta perché il server è destinato a una **VM Linux** con il NAS montato via SMB (e il README
riporta già la riga `GOOS=linux GOARCH=amd64 go build`). Su un mount SMB Linux il percorso è
`/mnt/.../PREVENTIVI`, non una stringa UNC con i backslash.

Da notare infine che le prove interessate stanno a **L1** e non hanno né build tag né guardia su
`runtime.GOOS`: l'intestazione di `integrita_test.go` dice esplicitamente che bastano «una cartella
temporanea e un orologio finto». Sono pensate per girare ovunque a ogni commit, quindi una CI che
non sia su Windows è rossa.

### Riproduzione

```sh
COCKPIT_TEST_DSN="postgres://…/cockpit_test" go test -tags integrazione -p 1 ./internal/jobs/
```

---

## 2. Le prove E2E del worker richiedono `pywin32` (2 test rossi)

`internal/workerapi` avvia il worker vero (`python workers/prova_e2e.py`), che importa
`worker_outlook` e quindi `pywintypes`. Fuori da Windows quel modulo non esiste:

```
--- FAIL: TestE2EIlWorkerVeroChiudeIlJobDelServerVero
    ModuleNotFoundError: No module named 'pywintypes'
--- FAIL: TestE2EIlBattitoDelWorkerVeroRinnovaIlLease
    il job 2 non è mai arrivato a in_corso entro 30s
```

Che su Linux non possano girare è nell'ordine delle cose: provano il client vero, e il client vero
parla con COM. Il punto è che **falliscono invece di saltare**, e il secondo impiega 30 secondi di
timeout per farlo.

Una via d'uscita esiste già — `COCKPIT_TEST_SENZA_PYTHON=1`, in `e2e_worker_db_test.go:75` — ma non
è citata nel README accanto a `COCKPIT_TEST_DSN`, quindi chi installa da zero la scopre solo
leggendo il sorgente del test dopo aver visto rosso.

---

## 3. `workers/requirements.txt` non è installabile fuori da Windows

```
ERROR: Could not find a version that satisfies the requirement pywin32>=306 (from versions: none)
ERROR: No matching distribution found for pywin32>=306
```

`pip` fallisce sull'intero file, quindi **non installa neanche `pydantic` e `pymupdf`**, che su
Linux servirebbero: il worker di analisi legge PDF e STEP e non ha alcun bisogno di COM. Sulla VM
Linux prevista per il server gira proprio il worker di analisi.

Per installare l'ambiente ho dovuto elencare i pacchetti a mano.

---

## Nota di metodo

L5–L9 (COM, replay, end-to-end, caos, shadow) non sono verificabili da qui: richiedono Windows e
Outlook. Quanto sopra riguarda esclusivamente ciò che è osservabile su Linux, cioè L1–L4.
