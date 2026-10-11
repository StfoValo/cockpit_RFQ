package web

import (
	"fmt"
	"reflect"
)

// motore_a_etichette.go: le etichette della Distinta nuova del motore A (giro 5, A1d; BE-10; contratto A1d §2.3; v1.1
// §4.4; T-A1d-29; RAC-11, RAC-12). Il template non traduce i valori degli enum da sé, e il JavaScript non ha una
// mappatura sua: ogni testo viene da questa tabella del server, con {{etichetta "<dominio>" <valore>}}.

// etichetta è {{etichetta "<dominio>" <valore>}} (contratto A1d §2.3; v1.1 §4.4): il testo italiano di un valore di
// enum, da una tabella del server. Segue i puntatori (nil = vuoto) e legge ogni tipo con nome che sia una stringa
// (reflect). Un valore che la tabella non conosce torna così com'è, fra parentesi quadre; il vuoto torna «[]»: il
// template tratta il vuoto prima. Un valore che non è una stringa torna «[valore]». Mai HTML nei testi.
func etichetta(dominio string, valore any) string {
	v := reflect.ValueOf(valore)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "[]"
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return "[]"
	}
	if v.Kind() != reflect.String {
		return "[" + fmt.Sprint(v.Interface()) + "]"
	}
	s := v.String()
	if s == "" {
		return "[]"
	}
	if t, ok := etichetteMotoreA[dominio][s]; ok {
		return t
	}
	return "[" + s + "]" // anche per un dominio sconosciuto (T-A1d-29)
}

// conTipiDocumento: le voci di un dominio più una copia di quelle di tipo_documento, senza cambiare le voci proprie del
// dominio (motivo_conflitto; piano di A1d backend §5.5). Si chiama nell'inizializzatore della tabella: nessun init.
func conTipiDocumento(m map[string]string) map[string]string {
	for k, t := range etichetteTipoDocumentoMA {
		if _, ok := m[k]; !ok {
			m[k] = t
		}
	}
	return m
}

// etichetteTipoDocumentoMA: le voci di tipo_documento (enum tipo_documento, migrations/0001 e 0008), a parte perché
// motivo_conflitto le copia (conTipiDocumento).
var etichetteTipoDocumentoMA = map[string]string{
	"cad_3d":            "3D",
	"disegno_2d":        "2D",
	"sviluppo_dxf":      "DXF",
	"capitolato":        "capitolato",
	"distinta_cliente":  "distinta del cliente",
	"commerciale":       "documento commerciale",
	"offerta_fornitore": "offerta del fornitore",
	"offerta_promatec":  "offerta Promatec",
	"ordine_cliente":    "ordine del cliente",
	"corrispondenza":    "corrispondenza",
	"rumore":            "rumore",
	"altro":             "altro",
	"da_determinare":    "da determinare",
}

// etichetteMotoreA: dominio → valore → testo. I testi li propone il frontend e li firma il backend (contratto §2.3);
// l'elenco completo, con la costante di ogni valore, è nel piano di A1d backend (appendice A): 62 domini, 319 voci con
// origine_categoria (RAC-11; i suoi tre testi sono quelli del kit, RAC-12). Le differenze fra questi testi e quelli del
// kit restano da firmare (D-RAC-01). motivo_grafo è testo libero del caricatore: la tabella ha i due testi noti, gli
// altri tornano fra quadre (T-A1d-29, D-A1d-10). La prova TestEtichettaCopreOgniCostante legge le costanti dai
// sorgenti dei pacchetti (appendice B): una costante nuova senza il suo testo la fa cadere.
var etichetteMotoreA = map[string]map[string]string{
	// contratto §2.3. Campi: ProdottoValutato.Stato.
	"stato_prodotto": {
		"non_pronto":         "non pronto",
		"pronto_fattibilita": "pronto per la fattibilità",
		"da_riesaminare":     "da riesaminare",
	},
	// contratto §2.3. Campi: ProdottoValutato.Motivi[], ProdottoBloccato.Motivi[].
	"motivo_prodotto": {
		"target_non_confermato":                            "prodotto della RFQ non confermato",
		"fonte_strutturale_step_mancante_o_non_confermata": "STEP strutturale mancante o non confermato",
		"fonte_superata":                                   "STEP strutturale superato",
		"nomenclatura_non_verificata":                      "codici non verificati",
		"gerarchia_non_verificata":                         "albero non verificato",
		"smistamento_non_verificato":                       "smistamento dei file non verificato",
		"documentazione_incompleta":                        "documenti incompleti",
		"documentazione_non_calcolabile":                   "documenti non calcolabili",
		"conflitto":                                        "conflitto da risolvere",
		"revisione_tecnica_aperta":                         "revisione tecnica aperta",
	},
	// contratto §2.3. Campi: FonteProdotto.Stato (vuoto con Calcolata falso: «[]»).
	"stato_fonte": {
		"assente":               "nessuna fonte",
		"in_attesa_di_conferma": "in attesa di conferma",
		"confermata":            "confermata",
	},
	// contratto §2.3. Campi: FonteProdotto.Motivo.
	"motivo_fonte": {
		"estrazione_riuscita":          "STEP letto",
		"dati_insufficienti":           "dati insufficienti",
		"analisi_in_corso":             "analisi in corso",
		"non_analizzata":               "STEP non ancora analizzato",
		"riferimento_superato":         "lo STEP confermato è superato",
		"da_scegliere":                 "più STEP: si sceglie quale",
		"proposta_3d_aperta":           "STEP proposto, non confermato",
		"indicata_sul_portale":         "indicato sul portale",
		"solo_altro_3d":                "solo un altro file 3D",
		"nessuna_fonte":                "nessuno STEP",
		"documento_candidato":          "STEP candidato",
		"estrazione_fallita":           "lettura dello STEP non riuscita",
		"solo_nodo_interno":            "solo come parte di un altro STEP",
		"prodotto_senza_componente":    "prodotto senza componente",
		"non_determinabile":            "non determinabile",
		"step_presente_non_analizzato": "STEP presente, non analizzato",
	},
	// contratto §2.3. Campi: VerificaAsse.Stato (BOM.Nomenclatura, BOM.Gerarchia).
	"stato_verifica_asse": {
		"da_verificare":    "da verificare",
		"verificata":       "verificata",
		"conflitto":        "in conflitto",
		"non_verificabile": "non verificabile",
	},
	// contratto §2.3. Campi: VerificaSmistamento.Stato.
	"stato_smistamento": {
		"da_verificare": "da verificare",
		"verificato":    "verificato",
		"conflitto":     "in conflitto",
		"in_revisione":  "in revisione",
	},
	// contratto §2.3. Campi: VerificaSmistamento.Motivi[], NodoBOM.Motivi[], FileDaSmistare.Motivo.
	"motivo_smistamento": {
		"file_da_smistare":                "file da smistare",
		"nessun_candidato":                "nessun componente candidato",
		"associazione_ambigua":            "associazione ambigua",
		"associazione_discordante":        "associazione discordante",
		"collocazione_non_determinabile":  "posizione non determinabile",
		"fuori_richiesta_non_confermato":  "fuori richiesta, non confermato",
		"associazione_non_confermata":     "associazione non confermata",
		"associazione_in_conflitto":       "associazione in conflitto",
		"nuovo_file_su_componente_deciso": "file nuovo su un componente già deciso",
		"revisione_tecnica_aperta":        "revisione tecnica aperta",
		"identita_documento_in_conflitto": "identità del documento in conflitto",
	},
	// contratto §2.3. Campi: CompletezzaDocumentale.Stato.
	"stato_documenti": {
		"non_calcolabile": "non calcolabile",
		"incompleta":      "incompleta",
		"completa":        "completa",
	},
	// contratto §2.3. Campi: VoceFabbisogno.Esito.
	"esito_fabbisogno": {
		"presente":      "presente",
		"da_verificare": "da verificare",
		"manca":         "manca",
	},
	// contratto §2.3. Campi: VoceFabbisogno.Motivo, Disegno2D.MotivoValidita.
	"motivo_fabbisogno": {
		"contenuto_non_analizzato":    "contenuto non analizzato",
		"contenuto_non_aperto":        "contenuto non aperto",
		"contenuto_non_letto":         "contenuto non letto",
		"contenuto_non_verificabile":  "contenuto non verificabile",
		"formato_non_configurato":     "formato non configurato",
		"associazione_non_confermata": "associazione non confermata",
		"nessun_documento":            "nessun documento",
		"derogato_non_sostituisce_2d": "derogato: la deroga non sostituisce il 2D",
		"sul_portale":                 "sul portale",
	},
	// contratto §2.3. Campi: FileInterpretato.Disponibilita.
	"disponibilita": {
		"disponibile": "disponibile",
		"mancante":    "non ancora scaricato",
		"pendente":    "in preparazione",
		"parziale":    "letto in parte",
		"senza_testo": "senza testo (scansione)",
		"illeggibile": "illeggibile",
		"errore":      "errore",
	},
	// contratto §2.3. Campi: ProdottoValutato.Struttura.
	"stato_struttura": {
		"nessuna":                "nessuna struttura",
		"struttura_candidata":    "struttura candidata",
		"bom_di_lavoro_proposta": "BOM di lavoro",
	},
	// contratto §2.3 (EB7-2). Campi: NodoBOM.Tipo, VoceFabbisogno.TipoComponente, Indice.Componente(…).Tipo.
	"tipo_componente": {
		"finito":       "prodotto finito",
		"sottoassieme": "sottoassieme",
		"sciolto":      "particolare",
		"commerciale":  "commerciale",
	},
	// contratto §2.3 (EB7-2, P7e). Campi: NodoBOM.OrigineTipo.
	"origine_tipo": {
		"confermato":            "confermato",
		"proposto_dalla_riga":   "proposto dalla riga",
		"proposto_dalla_regola": "proposto dalla regola",
	},
	// contratto §2.3. Campi: Classificazione.Categoria, Classificazione.Proposta, VoceFabbisogno.Categoria.
	"categoria": {
		"fabbricato":      "fabbricato",
		"commerciale":     "commerciale",
		"minuteria":       "minuteria",
		"non_determinata": "non determinata",
	},
	// RAC-11 (testi del kit, RAC-12). Campi: Classificazione.Origine.
	"origine_categoria": {
		"confermato":        "confermata",
		"derivato_dal_tipo": "dal tipo",
		"proposto":          "proposta",
	},
	// contratto §2.3. Campi: NodoBOM.Associazione (*), Disegno2D.Provenienza, AssociazioneFile.Origine, Conflitto.OrigineDecisione, CodiceDeciso.Origine.
	"origine_dato": {
		"grezzo":     "letto dal file",
		"proposto":   "proposto",
		"manuale":    "manuale",
		"confermato": "confermato",
		"scenario":   "scenario",
	},
	// contratto §2.3 (EB7-4). Campi: VoceFabbisogno.StatoNas.
	"stato_nas": {
		"in_coda": "copia sul NAS in coda",
		"scritto": "copiato sul NAS",
		"errore":  "copia sul NAS in errore",
	},
	// contratto §2.3 (testi del piano 7.4.3). Campi: confronto.EsitoFile.Badge.
	"badge": {
		"uguale":                    "uguale",
		"nuovo_ancoraggio":          "nuovo ancoraggio",
		"diverso":                   "diverso",
		"regressione_su_confermata": "regressione su proposta confermata",
		"fuori_richiesta":           "fuori richiesta",
	},
	// contratto §2.3. Campi: StatoFascicolo.MotivoNonCongelabile.
	"motivo_non_congelabile": {
		"thread_non_valutato":                      "thread non valutato",
		"nessun_target":                            "nessun prodotto",
		"bom_versione_con_prodotti_non_verificati": "BOM con prodotti non verificati",
		"prodotti_non_pronti":                      "prodotti non pronti",
	},
	// contratto §2.3 (testi del piano 7.6.2). Campi: EsitoThread.Motivo.
	"motivo_thread": {
		"senza_grammatica_a":       "nessuna grammatica A per questo cliente",
		"grammatica_scartata":      "grammatica A non valida per questo cliente",
		"ragione_sociale_discorde": "ragione sociale discorde",
		"errore_valutazione":       "errore nel calcolo",
	},
	// contratto §2.3. Campi: Conflitto.Tipo.
	"tipo_conflitto": {
		"codice":             "codice",
		"associazione":       "associazione",
		"arco":               "legame",
		"nuovo_file":         "file nuovo",
		"identita_documento": "identità del documento",
	},
	// contratto §2.3. Campi: AssociazioneFile.Perimetro.
	"perimetro": {
		"dentro":               "nel perimetro",
		"inline":               "immagine nel testo",
		"elemento_outlook":     "elemento di Outlook",
		"contenitore_estratto": "archivio estratto",
		"collegamento":         "collegamento",
		"messaggio_in_uscita":  "messaggio in uscita",
		"altra_controparte":    "altra controparte",
	},
	// template. Campi: Conflitto.Asse.
	"asse_conflitto": {
		"nomenclatura": "codici",
		"gerarchia":    "albero",
		"smistamento":  "smistamento",
	},
	// template. Campi: AssociazioneFile.Associazione.
	"associazione": {
		"non_valutata":     "non valutata",
		"nessun_candidato": "nessun candidato",
		"candidato_unico":  "candidato unico",
		"ambiguo":          "ambigua",
		"discordante":      "discordante",
	},
	// template. Campi: ProdottoValutato.Autorita, ComponenteDeciso.Autorita.
	"autorita": {
		"confermata": "confermata",
		"proposta":   "proposta",
		"scenario":   "scenario",
	},
	// template. Campi: Disegno2D.Cartiglio.
	"cartiglio": {
		"letto":                 "cartiglio letto",
		"non_leggibile":         "cartiglio non leggibile",
		"formato_senza_lettura": "formato senza lettura del cartiglio",
	},
	// template. Campi: AssociazioneFile.Collocazione.
	"collocazione": {
		"radice":            "radice",
		"figlio":            "figlio",
		"fuori_richiesta":   "fuori richiesta",
		"non_determinabile": "non determinabile",
	},
	// template. Campi: StrutturaCandidata.Compatibilita, Disegno2D.CompatibilitaCodice/Revisione, DocumentoCandidato.Compatibilita.
	"compatibilita": {
		"uguale":               "uguale",
		"equivalente":          "equivalente",
		"compatibile_parziale": "compatibile in parte",
		"discordante":          "discordante",
		"non_determinabile":    "non determinabile",
	},
	// template. Campi: DiscordanzaRevisione.Tra.
	"discordanza": {
		"documento_componente": "revisione del documento contro quella del componente",
		"proposta_registrata":  "revisione proposta contro quella registrata",
		"proposta_confermata":  "revisione proposta contro quella confermata",
	},
	// template. Campi: CodiceDocumentale.Esito.
	"esito_riconciliazione": {
		"concorda":               "concorda",
		"completamento_proposto": "completamento proposto",
		"correzione_proposta":    "correzione proposta",
		"discordante":            "discordante",
		"non_verificabile":       "non verificabile",
	},
	// template. Campi: FonteProdotto.EsitoVista (v_step_prodotto), FabbisognoInformativo.EsitoVista (v_fascicolo).
	"esito_vista": {
		"presente_analizzato":     "STEP presente, letto",
		"presente_parziale":       "STEP presente, letto in parte",
		"presente_non_analizzato": "STEP presente, non analizzato",
		"riferimento_superato":    "riferimento superato",
		"da_scegliere":            "da scegliere",
		"da_confermare":           "da confermare",
		"sul_portale":             "sul portale",
		"solo_altro_3d":           "solo un altro 3D",
		"mancante":                "mancante",
		"radice_senza_qualifica":  "radice senza qualifica di prodotto",
		"ok":                      "presente",
		"ok_in_coda":              "presente, copia sul NAS in coda",
		"ok_errore_nas":           "presente, copia sul NAS in errore",
		"derogato":                "derogato",
		"manca":                   "manca",
	},
	// template. Campi: FonteProdotto.Estrazione, DocumentoCandidato.Estrazione.
	"estrazione": {
		"riuscita":       "riuscita",
		"fallita":        "non riuscita",
		"in_corso":       "in corso",
		"non_analizzata": "non analizzata",
	},
	// template. Campi: EvidenzaRevisione.Fonte, RevisioneProposta.Fonte, DiscordanzaRevisione.FonteA/FonteB.
	"fonte_revisione": {
		"cartiglio":   "cartiglio",
		"step_entita": "nodo dello STEP",
		"nome_file":   "nome del file",
		"documento":   "documento registrato",
		"assente":     "nessuna fonte",
		"decisione":   "decisione sul componente",
		"componente":  "revisione registrata del componente",
		"confermata":  "revisione confermata",
	},
	// template. Campi: AbbinamentoPerBase.Motivo, AbbinamentoPerBase.MotivoDecisione.
	"motivo_abbinamento": {
		"abbinamento_per_base":                 "abbinato per base",
		"componenti_stessa_base":               "più componenti con la stessa base",
		"abbinamento_accanto_alla_riga_decisa": "abbinato accanto alla riga decisa",
		"nodo_scartato":                        "nodo scartato",
		"decisione_non_compatibile":            "decisione non compatibile",
		"decisione_non_verificabile":           "decisione non verificabile",
	},
	// template. Campi: VerificaAsse.Motivo.
	"motivo_asse": {
		"conflitto":                          "conflitto",
		"target_senza_componente":            "prodotto senza componente",
		"senza_figli_nessun_gesto":           "senza figli, nessun gesto",
		"nessun_gesto":                       "nessun gesto",
		"nessuna_struttura":                  "nessuna struttura",
		"da_decidere":                        "decisioni aperte",
		"nomenclatura_radice_non_verificata": "codice della radice non verificato",
		"fonte_non_confermata":               "fonte non confermata",
		"bom_di_lavoro_assente":              "BOM di lavoro assente",
	},
	// template. Campi: Classificazione.Motivo.
	"motivo_classificazione": {
		"finito_chiede_sempre_il_2d": "un prodotto finito chiede sempre il 2D",
		"conferma_non_ammessa":       "conferma non ammessa",
		"tipo_non_determinato":       "tipo non determinato",
	},
	// template. Campi: Conflitto.Motivo. Per un conflitto nuovo_file il motivo è il tipo del documento
	// (valutazione/associazioni.go, Motivo: x.tipo): conTipiDocumento aggiunge le voci di tipo_documento (piano di A1d
	// backend §5.5).
	"motivo_conflitto": conTipiDocumento(map[string]string{
		"quantita":           "quantità diversa",
		"rimozione":          "legame tolto dallo STEP",
		"componente_diverso": "componente diverso",
		"codice_confermato":  "diverso dal codice confermato",
		"codice":             "codice diverso",
		"revisione":          "revisione diversa",
	}),
	// template. Campi: CodiceDocumentale.Motivo.
	"motivo_documentale": {
		"cartiglio_non_letto":         "cartiglio non letto",
		"nodo_senza_identita":         "nodo senza identità",
		"cartiglio_altro_namespace":   "cartiglio di un altro spazio di codici",
		"cartiglio_non_completo":      "codice del cartiglio non completo",
		"base_non_confrontabile":      "base non confrontabile",
		"revisione_cartiglio_assente": "revisione assente nel cartiglio",
		"revisione_cartiglio_ambigua": "revisione del cartiglio ambigua",
		"letture_discordanti":         "letture discordanti",
		"identita_discordanti":        "identità discordanti",
	},
	// template. Campi: CompletezzaDocumentale.Motivo.
	"motivo_documenti": {
		"non_determinabile":       "non determinabile",
		"target_senza_componente": "prodotto senza componente",
		"voce_mancante":           "manca un documento",
		"voce_non_presente":       "un documento non è presente",
		"perimetro_aperto":        "perimetro aperto",
	},
	// template. Campi: FileInterpretato.Motivo.
	"motivo_file": {
		"contenitore":             "contenitore",
		"natura_non_file":         "non è un file",
		"documento_non_leggibile": "documento non leggibile",
		"thread_non_valutato":     "thread non valutato",
	},
	// template (testo libero: T-A1d-29, D-A1d-10). Campi: StrutturaCandidata.MotivoGrafo, DocumentoCandidato.MotivoGrafo.
	"motivo_grafo": {
		"completezza del grafo non dichiarata dal documento":          "completezza del grafo non dichiarata dal documento",
		"completezza non determinabile: nessun motivo dal caricatore": "completezza non determinabile: nessun motivo dal caricatore",
	},
	// template. Campi: CatenaCodice.MotivoLettura, VecchioPiatto.MotivoLettura.
	"motivo_lettura": {
		"nessuna_lettura":     "nessuna lettura",
		"letture_discordanti": "letture discordanti",
		"codice_vuoto":        "codice vuoto",
		"senza_grammatica":    "senza grammatica",
		"basi_diverse":        "basi diverse",
	},
	// template. Campi: StatoFascicolo.MotivoNonCongelato.
	"motivo_non_congelato": {
		"gesto_non_registrato": "il congelamento non è ancora un gesto",
	},
	// template. Campi: CompletezzaDocumentale.MotivoPerimetro.
	"motivo_perimetro": {
		"target_senza_componente": "prodotto senza componente",
		"fonte_non_confermata":    "fonte non confermata",
		"bom_di_lavoro_assente":   "BOM di lavoro assente",
		"nodi_da_decidere":        "nodi da decidere",
		"archi_da_decidere":       "legami da decidere",
		"rimozioni_aperte":        "rimozioni aperte",
		"non_determinabile":       "non determinabile",
	},
	// template. Campi: FabbisognoPrevisto.Motivo.
	"motivo_previsto": {
		"nodo_proposto":               "nodo proposto",
		"nodo_deciso_fuori_perimetro": "nodo deciso fuori dal perimetro",
		"rimozione_aperta":            "rimozione aperta",
	},
	// template. Campi: CatenaCodice.MotivoProposto.
	"motivo_proposto": {
		"non_composto":                   "codice non composto",
		"nessuna_lettura":                "nessuna lettura",
		"letture_discordanti":            "letture discordanti",
		"forma_cartiglio_assente":        "forma del cartiglio assente",
		"forme_cartiglio_multiple":       "più forme del cartiglio",
		"parte_non_determinata":          "parte non determinata",
		"qualificatore_non_trasferibile": "qualificatore non trasferibile",
		"revisione_non_determinata":      "revisione non determinata",
		"revisione_ambigua":              "revisione ambigua",
		"lettura_non_completa":           "lettura non completa",
	},
	// template. Campi: VoceFabbisogno.NotaRegola, FabbisognoInformativo.NotaRegola.
	"nota_regola": {
		"schema_senza_2d":                 "lo schema non chiede il 2D",
		"regola_cliente_senza_2d":         "il cliente non chiede il 2D",
		"regola_cliente_2d_non_bloccante": "per il cliente il 2D non è bloccante",
	},
	// template. Campi: DocumentoCandidato.Origine.
	"origine_candidato": {
		"documento_del_prodotto": "documento del prodotto",
		"motore_a":               "proposto dal motore A",
		"proposta_aperta":        "proposta aperta",
	},
	// template. Campi: GruppoDisegni2D.RegolaPrimario.
	"regola_primario": {
		"corrente":      "corrente",
		"compatibilita": "compatibilità",
		"formato":       "formato",
		"recente":       "più recente",
		"sha256":        "ordine del contenuto (sha256)",
	},
	// template. Campi: RelazioneDisegni.Tipo.
	"relazione_disegni": {
		"rappresentazione_alternativa": "rappresentazione alternativa",
		"revisione_diversa":            "revisione diversa",
		"elaborato_aggiuntivo":         "elaborato aggiuntivo",
		"non_determinata":              "non determinata",
	},
	// template. Campi: Disegno2D.RevProvenienza, CodiceDeciso.RevProvenienza, EvidenzaDecisione.RevProvenienza.
	"rev_provenienza": {
		"non_registrata":               "non registrata",
		"coincide_con_formazione_step": "coincide con la formazione dello STEP",
		"decisione_tracciata":          "decisione tracciata",
	},
	// template. Campi: Classificazione.Ruolo.
	"ruolo": {
		"prodotto":   "prodotto",
		"componente": "componente",
	},
	// template. Campi: ProdottoValutato.Identita.
	"stato_identita_target": {
		"da_confermare": "da confermare",
		"confermata":    "confermata",
	},
	// template. Campi: Disegno2D.StatoRevisione, IdentitaNodo.StatoRevisione.
	"stato_revisione": {
		"assente":    "nessuna revisione",
		"proposta":   "revisione proposta",
		"registrata": "revisione registrata",
		"confermata": "revisione confermata",
		"candidata":  "revisione candidata",
	},
	// template. Campi: RigaDecisaLegacy.Stato (e RigaComponenteProposta.Stato).
	"stato_riga": {
		"aperta":     "aperta",
		"confermata": "confermata",
		"scartata":   "scartata",
		"duplicato":  "duplicato",
	},
	// template. Campi: CongelamentoLegacy.StatoCorrente (bom_versione.stato).
	"stato_versione": {
		"bozza":     "bozza",
		"congelata": "congelata",
	},
	// template. Campi: VoceFabbisogno.TipoDocumento, FabbisognoPrevisto.TipoDocumento, Indice.Correnti(…).
	"tipo_documento": etichetteTipoDocumentoMA,
	// template. Campi: Disegno2D.Validita.
	"validita_2d": {
		"valido":                  "valido",
		"da_verificare":           "da verificare",
		"formato_non_configurato": "formato non configurato",
	},
	// ELENCO BE-10 (testa). Campi: testaMotoreA.StatoThread (fotorfq.Thread.Stato).
	"stato_thread": {
		"APERTA": "aperta",
		"CHIUSA": "chiusa",
	},
	// ELENCO BE-10 (testa). Campi: faseRegistrata.Semaforo (v_thread_fase, migrations/0001).
	"semaforo": {
		"bianco": "senza tempi",
		"rosso":  "oltre i tempi",
		"giallo": "in scadenza",
		"verde":  "nei tempi",
	},
	// ELENCO BE-10 (testa; il testo di catalogo è Testa.Fase.Etichetta). Campi: faseRegistrata.Nome (enum fase). Testi
	// neutri (T-A1d-40): le descrizioni del catalogo delle fasi non entrano qui.
	"fase": {
		"RICEVUTA":        "ricevuta",
		"ATTESA_DISEGNI":  "in attesa dei disegni",
		"FATTIBILITA":     "fattibilità",
		"SCHEDA_COSTO":    "scheda costo",
		"OFFERTE_FORN":    "offerte dei fornitori",
		"OFFERTA_INVIATA": "offerta inviata",
		"ACCETTATA":       "accettata",
		"DISTINTA_ERP":    "distinta nel gestionale",
		"ORDINE":          "ordine",
		"PRODUZIONE":      "in produzione",
		"PERSA":           "persa",
		"RESPINTA":        "respinta",
		"SCADUTA":         "scaduta",
	},
}
