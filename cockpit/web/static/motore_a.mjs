// Motore A, A1d (giro 5): il comportamento della Distinta nuova, /thread/{id}/motore-a. SOLO presentazione.
//
// Che cosa fa (piano del frontend §7): le schede dell'albero, i rami, la casella scelta e la sua anteprima persistente
// (anche come foglio dal basso sotto 860 px), le miniature e il disegno con pdf.js, la tastiera, il trascinamento che
// APRE il modulo «Sposta», la scelta del fieldset di «scelta» (FE-11), vista=documenti (FE-13), le finestre di
// anteprima (STEP strutturale, tipo, riepilogo dell'albero), il visore e i dati, il ripristino dopo la rilettura di #ma.
// Che cosa NON fa: non calcola stati, assi, fabbisogni, esenzioni, tipi, quantità, prontezza o fasi; non conta e non
// filtra dati per decidere che cosa mostrare (lo decide il template); non chiama rotte di scrittura: scrive solo il
// ponte motore_a_gesti.mjs (data-ma-gesto, Gesti.esegui, Gesti.confermaAlbero); non legge il corpo delle risposte dei
// gesti; non simula una riuscita e non concatena gesti; costruisce i nodi con el() e textContent, mai con l'HTML.
// Dopo un gesto riuscito il ponte rilegge #ma: lo stato nuovo arriva SOLO dal server (X-08 anche per l'albero).
// Le miniature e il disegno: una GET semplice di /allegato/{id}/anteprima?v=<sha>, senza le intestazioni di htmx (con
// quelle la rotta risponde 400) e mai in un riquadro incorporato (la risposta ha la CSP sandbox): solo pdf.js.
import * as Gesti from "./motore_a_gesti.mjs";

// ------------------------------------------------------------------ aiuti

const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));

// un elemento DOM con testo (mai una stringa HTML con dati del server). Una chiave on… vale solo con una funzione: un
// testo sotto on… non diventa mai un gestore scritto nell'attributo (revisione P3, S-02), si scarta.
function el(tag, attr, ...figli) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attr || {})) {
    if (v == null || v === false) continue;
    if (k === "class") e.className = v;
    else if (/^on/i.test(k)) { if (typeof v === "function") e.addEventListener(k.slice(2), v); }
    else e.setAttribute(k, v === true ? "" : String(v));
  }
  for (const f of figli.flat()) {
    if (f == null || f === false) continue;
    e.append(f instanceof Node ? f : document.createTextNode(String(f)));
  }
  return e;
}

// sessionStorage: può mancare (finestra privata, dati bloccati). La pagina funziona lo stesso.
const memoria = {
  leggi(k) { try { return k ? sessionStorage.getItem(k) : null; } catch (e) { return null; } },
  scrivi(k, v) {
    try {
      if (!k) return;
      if (v == null) sessionStorage.removeItem(k); else sessionStorage.setItem(k, v);
    } catch (e) { /* niente: è solo una comodità */ }
  },
};

const radice = () => $("#ma > .ma");
const thread = () => (radice() || { dataset: {} }).dataset.maThread || "";
const prodotto = () => (radice() || { dataset: {} }).dataset.maProdotto || "";
const chiave = (nome) => (thread() ? `ma.${thread()}.${nome}` : null);
// CSS.escape può mancare (jsdom): il ripiego protegge virgolette e barre, basta per i valori fra virgolette
const cssStr = (s) => (window.CSS && CSS.escape ? CSS.escape(String(s)) : String(s).replace(/["\\]/g, "\\$&"));
const testoDi = (err) => (err && (err.message || err.testo)) || String(err || "errore sconosciuto");
const enc = encodeURIComponent;

// ------------------------------------------------------------------ rami dell'albero
// Ogni casella è un <li role=treeitem> con data-ma-chiave («<gruppo>|<rif>»: la stessa casella sotto due padri ha la
// stessa chiave) e data-ma-cammino (la posizione, unica, con il gruppo davanti). Un ramo chiuso nasconde il suo <ul>
// (CSS); qui si cambia solo aria-expanded.

const voci = (albero) => (albero ? $$("li[role='treeitem']", albero) : []);
const visibile = (li) => !li.parentElement.closest("li[aria-expanded='false']");
const nodoDi = (li) => li && li.querySelector(":scope > .ma-nodo-box > .ma-nodo");

function salvaRami() {
  const chiusi = $$("#ma .ma-dtree li[aria-expanded='false']").map((li) => li.dataset.maCammino);
  memoria.scrivi(chiave("chiusi." + prodotto()), JSON.stringify(chiusi));
}

function ripristinaRami(r) {
  let chiusi = [];
  try { chiusi = JSON.parse(memoria.leggi(chiave("chiusi." + prodotto())) || "[]"); } catch (e) { chiusi = []; }
  const insieme = new Set(Array.isArray(chiusi) ? chiusi : []);
  for (const li of $$(".ma-dtree li[aria-expanded]", r)) {
    if (insieme.has(li.dataset.maCammino)) li.setAttribute("aria-expanded", "false");
  }
}

function apriChiudi(li, aperto) {
  if (!li || !li.hasAttribute("aria-expanded")) return;
  li.setAttribute("aria-expanded", aperto ? "true" : "false");
  salvaRami();
}

function tuttiIRami(blocco, aperto) {
  if (!blocco) return;
  for (const li of $$(".ma-dtree li[aria-expanded]", blocco)) li.setAttribute("aria-expanded", aperto ? "true" : "false");
  salvaRami();
}

// tabindex «a rotazione»: una sola casella per albero entra nel giro del Tab
function muovi(li) {
  if (!li) return;
  for (const x of voci(li.closest(".ma-dtree"))) x.tabIndex = -1;
  li.tabIndex = 0;
  li.focus({ preventScroll: false });
  const n = nodoDi(li);
  if (n && n.scrollIntoView) n.scrollIntoView({ block: "nearest", inline: "nearest" });
}

function preparaTabindex(r) {
  for (const t of $$(".ma-dtree", r)) {
    const tutte = voci(t);
    tutte.forEach((x) => { x.tabIndex = -1; });
    const prima = tutte.find((x) => x.getAttribute("aria-selected") === "true" && visibile(x)) || tutte[0];
    if (prima) prima.tabIndex = 0;
  }
}

const padreDi = (li) => (li.parentElement && li.parentElement.closest("li[role='treeitem']")) || null;
const figliDi = (li) => $$(":scope > ul > li[role='treeitem']", li);
const fratelliDi = (li) => Array.from(li.parentElement.children).filter((x) => x.matches("li[role='treeitem']"));

// frecce come nell'organigramma: giù = primo figlio, su = padre, sinistra e destra = fratelli
function tastiAlbero(e) {
  const li = e.target;
  if (!(li instanceof HTMLElement) || !li.matches(".ma-dtree li[role='treeitem']")) return;
  const tutte = voci(li.closest(".ma-dtree")).filter(visibile);
  const fr = fratelliDi(li);
  const i = fr.indexOf(li);
  switch (e.key) {
    case "ArrowDown":
      if (li.getAttribute("aria-expanded") === "false") apriChiudi(li, true);
      muovi(figliDi(li)[0]);
      break;
    case "ArrowUp": muovi(padreDi(li)); break;
    case "ArrowLeft": muovi(fr[i - 1]); break;
    case "ArrowRight": muovi(fr[i + 1]); break;
    case "Home": muovi(tutte[0]); break;
    case "End": muovi(tutte[tutte.length - 1]); break;
    case "-": apriChiudi(li, false); break;
    case "+": apriChiudi(li, true); break;
    case "Enter":
    case " ":
      // il fuoco va sul titolo dell'anteprima: Tab porta subito a «Apri il visore» e ai gesti, Esc torna alla casella
      casellaPrima = li;
      seleziona(li.dataset.maChiave, { apri: true, fuoco: true });
      break;
    default:
      return;
  }
  e.preventDefault();
}

// la tela ricorda lo scorrimento di lato (per gruppo e prodotto) fra una rilettura e l'altra
function salvaTele() {
  for (const t of $$("#ma .ma-tela[data-ma-gruppo]")) memoria.scrivi(chiave(`tela.${prodotto()}.${t.dataset.maGruppo}`), String(t.scrollLeft || 0));
}
function ripristinaTele(r) {
  for (const t of $$(".ma-tela[data-ma-gruppo]", r)) {
    const m = memoria.leggi(chiave(`tela.${prodotto()}.${t.dataset.maGruppo}`));
    // la prima volta l'albero parte dalla radice (al centro, come nell'organigramma); poi resta dov'era
    if (m != null && m !== "") t.scrollLeft = Number(m) || 0;
    else if (t.scrollWidth > t.clientWidth) {
      const radiceNodo = t.querySelector(".ma-dtree > li > .ma-nodo-box > .ma-nodo");
      if (radiceNodo) {
        const rt = t.getBoundingClientRect();
        const rn = radiceNodo.getBoundingClientRect();
        t.scrollLeft = Math.max(0, rn.left - rt.left + t.scrollLeft - (t.clientWidth - rn.width) / 2);
      }
    }
  }
}

// ------------------------------------------------------------------ schede dell'albero (role=tablist, §7.3)
// Attivazione automatica: la scheda col fuoco è quella mostrata. Le chiavi dei gruppi sono opache (T-A1d-FE-01).

const schede = (r) => $$("#ma-schede [role='tab']", r || radice() || document);

function mostraScheda(gruppo, { fuoco = false } = {}) {
  const tutte = schede();
  const scelta = tutte.find((t) => t.dataset.maGruppo === gruppo);
  if (!scelta) return false;
  for (const t of tutte) {
    const su = t === scelta;
    t.setAttribute("aria-selected", su ? "true" : "false");
    t.tabIndex = su ? 0 : -1;
    const p = document.getElementById(t.getAttribute("aria-controls") || "");
    if (p) p.hidden = !su;
  }
  memoria.scrivi(chiave("scheda." + prodotto()), gruppo);
  if (fuoco) scelta.focus();
  return true;
}

function tastiSchede(e) {
  const tab = e.target.closest && e.target.closest("[role='tab']");
  if (!tab) return;
  const tutte = schede();
  const i = tutte.indexOf(tab);
  if (i < 0) return;
  let j;
  switch (e.key) {
    case "ArrowRight": j = (i + 1) % tutte.length; break;
    case "ArrowLeft": j = (i - 1 + tutte.length) % tutte.length; break;
    case "Home": j = 0; break;
    case "End": j = tutte.length - 1; break;
    default: return;
  }
  e.preventDefault();
  mostraScheda(tutte[j].dataset.maGruppo, { fuoco: true });
}

function ripristinaScheda() {
  const g = memoria.leggi(chiave("scheda." + prodotto()));
  if (g) mostraScheda(g);
}

// ------------------------------------------------------------------ la casella scelta e l'anteprima (§7.5)
// Nessuna scelta automatica all'apertura (D-PF-06): con le candidate sembrerebbe una preselezione.

let casellaPrima = null; // la casella da cui si è aperta l'anteprima: Esc ci riporta il fuoco
let foglioPrima = false; // l'anteprima era aperta come foglio prima della rilettura

const ws = () => $("#ma .ma-ws");
// Il foglio dal basso: lo STESSO criterio del CSS (§6.3, la finestra sotto 860 px), così il JS non tratta da foglio
// un'anteprima che il CSS lascia nel flusso (fra 860 e circa 1075 px con la colonna laterale del layout il contenitore
// è più stretto della finestra). Senza matchMedia (jsdom) vale la larghezza di .ma-ws fra 1 e 859 px, come scrive §7.5
// (in jsdom è 0: falso; le prove la impostano). L'incoerenza fra §6.3 e §7.5 è il DUBBIO D-FEA-04.
const FOGLIO_MQ = "(max-width: 859.98px)";
const mqFoglio = typeof window.matchMedia === "function" ? window.matchMedia(FOGLIO_MQ) : null;
function modoFoglio() {
  if (mqFoglio) return mqFoglio.matches;
  const w = ws();
  const l = w ? w.getBoundingClientRect().width : 0;
  return l > 0 && l < 860;
}

// Il foglio aperto è modale: il resto della pagina è inert (né Tab né lettore di schermo ci arrivano), l'anteprima
// diventa una finestra (role=dialog, aria-modal) e Tab gira dentro (tastiFoglio). Alla chiusura tutto torna com'era.
const FUORI_DAL_FOGLIO = ".ma > .ma-testa, .ma > .ma-navi, .ma-ws-centro, .ma-ws-smistare, #ma-documenti";
function apriFoglio(w) {
  w.classList.add("ma-ws--foglio");
  memoria.scrivi(chiave("foglio"), "1");
  const r = radice();
  if (!r) return;
  for (const x of $$(FUORI_DAL_FOGLIO, r)) x.setAttribute("inert", "");
  const lato = $("#ma-anteprima", r);
  if (lato) { lato.setAttribute("role", "dialog"); lato.setAttribute("aria-modal", "true"); }
}
function togliModale(r) {
  if (!r) return;
  for (const x of $$("[inert]", r)) x.removeAttribute("inert");
  const lato = $("#ma-anteprima", r);
  if (lato) { lato.removeAttribute("role"); lato.removeAttribute("aria-modal"); }
}
const foglioAperto = () => { const w = ws(); return !!(w && w.classList.contains("ma-ws--foglio")); };

// un elemento che a vista non c'è (hidden, o dentro un details chiuso fuori dal suo summary): non prende il fuoco
const nascosto = (x) => !!x.closest("[hidden]") || (!!x.closest("details:not([open])") && !x.closest("details:not([open]) > summary"));
const FOCALIZZABILI = "a[href], button:not([disabled]), input:not([disabled]):not([type='hidden']), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex='-1'])";

// Tab nel foglio aperto: dall'ultimo controllo si torna al primo e viceversa
function tastiFoglio(e, t) {
  const lato = $("#ma-anteprima");
  if (!lato) return;
  const f = $$(FOCALIZZABILI, lato).filter((x) => !nascosto(x));
  if (!f.length) return;
  const primo = f[0];
  const ultimo = f[f.length - 1];
  const dentro = t && lato.contains(t);
  if (e.shiftKey && (!dentro || t === primo)) { ultimo.focus(); e.preventDefault(); }
  else if (!e.shiftKey && (!dentro || t === ultimo)) { primo.focus(); e.preventDefault(); }
}

function seleziona(k, { apri = true, fuoco = false } = {}) {
  const r = radice();
  const w = ws();
  if (!r || !w || !k) return;
  const comparse = $$(`li[role='treeitem'][data-ma-chiave="${cssStr(k)}"]`, r);
  const pannello = $(`#ma-anteprima .ma-anteprima[data-ma-pannello="${cssStr(k)}"]`, r);
  if (!pannello) return;
  // la scheda del gruppo della casella (dal pannello che la contiene, la chiave non si spezza)
  const tp = comparse.length ? comparse[0].closest("[role='tabpanel']") : null;
  if (tp && tp.hidden) mostraScheda(tp.dataset.maGruppo);
  for (const x of $$("li[role='treeitem'][aria-selected='true']", r)) x.setAttribute("aria-selected", "false");
  comparse.forEach((x) => x.setAttribute("aria-selected", "true"));
  if (comparse[0]) {
    for (const x of voci(comparse[0].closest(".ma-dtree"))) x.tabIndex = -1;
    comparse[0].tabIndex = 0;
  }
  for (const p of $$("#ma-anteprima .ma-anteprima", r)) p.hidden = p !== pannello;
  memoria.scrivi(chiave("nodo." + prodotto()), k);
  if (apri && modoFoglio()) apriFoglio(w);
  mostraDisegno(pannello);
  if (fuoco) {
    const t = $(".ma-anteprima-t", pannello);
    if (t) t.focus({ preventScroll: !modoFoglio() });
  }
}

// ✕ dell'anteprima: si torna allo stato vuoto (nessuna casella scelta) e il fuoco torna alla casella
function chiudiAnteprima() {
  const r = radice();
  if (!r) return;
  const sel = (casellaPrima && casellaPrima.isConnected && casellaPrima) || $("li[role='treeitem'][aria-selected='true']", r);
  for (const x of $$("li[role='treeitem'][aria-selected='true']", r)) x.setAttribute("aria-selected", "false");
  for (const p of $$("#ma-anteprima .ma-anteprima", r)) p.hidden = true;
  chiudiFoglio();
  memoria.scrivi(chiave("nodo." + prodotto()), null);
  memoria.scrivi(chiave("disegno." + prodotto()), null);
  if (sel) sel.focus();
}

// Esc: si chiude solo il foglio (la scelta resta, piano §3.2); il clic sullo sfondo chiude l'anteprima (§7.7)
function chiudiFoglio() {
  const w = ws();
  if (w) w.classList.remove("ma-ws--foglio");
  memoria.scrivi(chiave("foglio"), null);
  togliModale(radice());
}

function ripristinaSelezione(r) {
  const k = memoria.leggi(chiave("nodo." + prodotto()));
  if (!k || !$(`#ma-anteprima .ma-anteprima[data-ma-pannello="${cssStr(k)}"]`, r)) return;
  seleziona(k, { apri: foglioPrima || memoria.leggi(chiave("foglio")) === "1" });
  foglioPrima = false;
}

// ------------------------------------------------------------------ pdf.js: il caricatore, la GET del PDF (§7.4)

let pdfjsPromessa = null;
function pdfjs() { // un solo caricamento dinamico (la prova con jsdom lo sostituisce)
  const m = $("#ma");
  const src = m && m.dataset.maPdfjs;
  const worker = m && m.dataset.maPdfjsWorker;
  if (!src || !worker) return Promise.resolve(null);
  if (!pdfjsPromessa) {
    const cartella = new URL(".", new URL(src, location.href)).href; // cmaps/, wasm/, iccs/, standard_fonts/
    pdfjsPromessa = import(src)
      .then((lib) => { lib.GlobalWorkerOptions.workerSrc = worker; return { lib, cartella }; })
      .catch(() => { pdfjsPromessa = null; return null; });
  }
  return pdfjsPromessa;
}

// un solo PDFWorker per miniature e palco
let lavoratorePdf = null;
function lavoratore(lib) {
  if (!lavoratorePdf) lavoratorePdf = new lib.PDFWorker({ name: "ma-anteprime" });
  return lavoratorePdf;
}

function opzioniPdf(cartella, extra) {
  return Object.assign({
    isEvalSupported: false,
    enableXfa: false,
    verbosity: 0,
    wasmUrl: cartella + "wasm/",
    iccUrl: cartella + "iccs/",
    cMapUrl: cartella + "cmaps/",
    cMapPacked: true,
    standardFontDataUrl: cartella + "standard_fonts/",
  }, extra || {});
}

// sempre con ?v=<sha> (revisione P3, S-04): senza l'impronta non si chiede niente
const indirizzoPdf = (a, v) => {
  if (!a || !v) throw new Error("manca l'impronta del file");
  return "/allegato/" + enc(a) + "/anteprima?v=" + enc(v);
};

// La GET del PDF (D-PF-08): fetch semplice, stessa origine, nessuna intestazione in più. Il motivo di un rifiuto
// (404, 409, 415) è il testo della risposta; un rinvio al login vuol dire sessione scaduta.
async function scaricaPdf(a, v) {
  let risp;
  try {
    risp = await fetch(indirizzoPdf(a, v), { credentials: "same-origin" });
  } catch (err) {
    throw new Error("anteprima non disponibile: " + testoDi(err));
  }
  if (risp.redirected && /\/login(\?|$)/.test(new URL(risp.url, location.href).pathname + "?")) {
    throw new Error("sessione scaduta: ricarica la pagina");
  }
  if (!risp.ok) {
    let t = "";
    try { t = (await risp.text()).trim().slice(0, 200); } catch (e) { t = ""; }
    throw new Error("anteprima non disponibile: " + (t || "errore " + risp.status));
  }
  const tipo = (risp.headers && risp.headers.get && risp.headers.get("Content-Type")) || "";
  if (!/application\/pdf/i.test(tipo)) throw new Error("la risposta non è un PDF");
  return new Uint8Array(await risp.arrayBuffer());
}

// apre il documento con pdf.js; il chiamante lo distrugge
async function apriDocumento(a, v) {
  const p = await pdfjs();
  if (!p) throw new Error("il visore dei PDF non si carica");
  const dati = await scaricaPdf(a, v);
  const task = p.lib.getDocument(opzioniPdf(p.cartella, { data: dati, worker: lavoratore(p.lib) }));
  const doc = await task.promise;
  return { lib: p.lib, task, doc };
}

// la pagina 1 in un canvas largo «larghezza» pixel (o dentro un riquadro largo x alto)
async function pagina1(lib, doc, larghezza, altezza) {
  const pag = await doc.getPage(1);
  const v1 = pag.getViewport({ scale: 1 });
  const scala = altezza ? Math.min(larghezza / v1.width, altezza / v1.height) : larghezza / v1.width;
  const viewport = pag.getViewport({ scale: scala });
  const canvas = el("canvas", { width: Math.max(1, Math.ceil(viewport.width)), height: Math.max(1, Math.ceil(viewport.height)) });
  const annotationMode = lib.AnnotationMode ? lib.AnnotationMode.DISABLE : undefined;
  await pag.render({ canvas, viewport, annotationMode }).promise;
  return canvas;
}

// ------------------------------------------------------------------ miniature (lo schema di fascicolo.mjs, §7.4)
// Solo [data-ma-mini][data-ma-allegato] (il template li mette solo con un'anteprima disponibile). Coda di al più DUE
// documenti insieme; cache allegato|sha con tetto 300 (la più vecchia esce con revokeObjectURL); un errore si
// ritenta dopo 60 s. Niente immagini in sessionStorage.

const MAX_MINI = 2;
const TETTO_MINI = 300;
const cacheMini = new Map(); // allegato|sha → objectURL
const fallite = new Map(); // allegato|sha → {quando, testo}
const inCorso = new Set();
let codaMini = [];
let miniAttive = 0;
let osservatore = null;

const chiaveMini = (x) => `${x.dataset.maAllegato}|${x.dataset.maSha || ""}`;

function metti(k, url) {
  if (cacheMini.has(k)) cacheMini.delete(k);
  cacheMini.set(k, url);
  while (cacheMini.size > TETTO_MINI) {
    const [vecchia, u] = cacheMini.entries().next().value;
    cacheMini.delete(vecchia);
    try { URL.revokeObjectURL(u); } catch (e) { /* niente */ }
  }
}

function mettiMini(span, url) {
  if (!span.isConnected) return;
  const img = span.querySelector("img");
  if (img && img.getAttribute("src") === url) return;
  span.replaceChildren(el("img", { src: url, alt: "" }));
  span.dataset.maStato = "pronta";
}

function miniNonDisponibile(span, testo) {
  span.replaceChildren(document.createTextNode(testo || "anteprima non disponibile"));
  span.dataset.maStato = "errore";
  span.classList.add("ma-mini--na");
  span.title = testo || "anteprima non disponibile";
}

function daFare(k) {
  const f = fallite.get(k);
  if (f && Date.now() - f.quando > 60000) fallite.delete(k);
  return !cacheMini.has(k) && !inCorso.has(k) && !fallite.has(k);
}

function miniature(r) {
  if (osservatore) osservatore.disconnect();
  codaMini = [];
  const tutte = $$("[data-ma-mini][data-ma-allegato]", r);
  const daOsservare = [];
  for (const s of tutte) {
    const k = chiaveMini(s);
    if (cacheMini.has(k)) { mettiMini(s, cacheMini.get(k)); continue; }
    if (fallite.has(k) && !daFare(k)) { miniNonDisponibile(s, fallite.get(k).testo); continue; }
    daOsservare.push(s);
  }
  if (typeof IntersectionObserver !== "function") {
    for (const s of daOsservare) codaMini.push(s);
    lavora();
    return;
  }
  osservatore = new IntersectionObserver((voci) => {
    for (const v of voci) {
      if (!v.isIntersecting) continue;
      osservatore.unobserve(v.target);
      codaMini.push(v.target);
    }
    lavora();
  }, { root: null, rootMargin: "200px" });
  for (const s of daOsservare) osservatore.observe(s);
}

function lavora() {
  while (miniAttive < MAX_MINI && codaMini.length) {
    const s = codaMini.shift();
    if (!s.isConnected) continue;
    const k = chiaveMini(s);
    if (cacheMini.has(k)) { mettiMini(s, cacheMini.get(k)); continue; }
    if (!daFare(k)) continue;
    miniAttive++;
    inCorso.add(k);
    miniatura(s, k).finally(() => {
      inCorso.delete(k);
      miniAttive--;
      lavora();
    });
  }
}

async function miniatura(span, k) {
  const [a, v] = [span.dataset.maAllegato, span.dataset.maSha];
  let aperto = null;
  try {
    aperto = await apriDocumento(a, v);
    const canvas = await pagina1(aperto.lib, aperto.doc, 320, 226);
    const blob = await new Promise((ok, ko) => {
      try { canvas.toBlob((b) => (b ? ok(b) : ko(new Error("immagine vuota"))), "image/png"); } catch (err) { ko(err); }
    });
    const url = URL.createObjectURL(blob);
    metti(k, url);
    for (const x of $$("#ma [data-ma-mini][data-ma-allegato]")) if (chiaveMini(x) === k) mettiMini(x, url);
  } catch (err) {
    const testo = testoDi(err);
    fallite.set(k, { quando: Date.now(), testo });
    for (const x of $$("#ma [data-ma-mini][data-ma-allegato]")) if (chiaveMini(x) === k) miniNonDisponibile(x, testo);
  } finally {
    if (aperto && aperto.task) aperto.task.destroy().catch(() => {});
  }
}

// ------------------------------------------------------------------ il palco: il disegno nel pannello (§7.5)
// Larghezza = larghezza del palco × devicePixelRatio, arrotondata a 40 px (per la cache). In cache il canvas si
// rimette subito (nessuna richiesta, nessuno sfarfallio dopo la rilettura). Un solo documento aperto.

const TETTO_PALCO = 12;
const cachePalco = new Map(); // allegato|sha|larghezza → {canvas, pagine}
let docAperto = null; // {chiave: allegato|sha, task, doc, lib}
let giroPalco = 0;
let osservaPalco = null;
let larghezzaPalco = 0;
let attesaPalco = 0;

function larghezzaDi(palco) {
  const l = palco.getBoundingClientRect ? palco.getBoundingClientRect().width : 0;
  const w = (l || palco.clientWidth || 320) * (window.devicePixelRatio || 1);
  return Math.max(40, Math.round(w / 40) * 40);
}

// il disegno da mostrare: l'alternativo scelto (se il server lo rende ancora per QUESTA casella) o il principale
function mostraDisegno(pannello) {
  const palco = $(".ma-palco[data-ma-palco]", pannello);
  if (!palco) return;
  let btn = null;
  try {
    const s = JSON.parse(memoria.leggi(chiave("disegno." + prodotto())) || "null");
    if (s && s.nodo === pannello.dataset.maPannello && s.allegato) {
      btn = $$("[data-ma-azione='disegno'][data-ma-allegato]", pannello).find((b) => b.dataset.maAllegato === s.allegato && !b.disabled) || null;
    }
  } catch (e) { btn = null; }
  if (btn) segnaDisegno(pannello, btn);
  const a = btn ? btn.dataset.maAllegato : palco.dataset.maAllegato;
  const v = btn ? btn.dataset.maSha : palco.dataset.maSha;
  const nome = btn ? btn.dataset.maFile : palco.dataset.maFile;
  if (!a) return;
  disegnaPalco(pannello, palco, a, v, nome);
  osserva(pannello, palco);
}

// aria-pressed, la revisione visibile e «Apri il visore» seguono il disegno mostrato; il principale non cambia
function segnaDisegno(pannello, btn) {
  for (const b of $$("[data-ma-azione='disegno']", pannello)) b.setAttribute("aria-pressed", b === btn ? "true" : "false");
  for (const dl of $$("dl[data-ma-rev-di]", pannello)) dl.hidden = dl.dataset.maRevDi !== btn.dataset.maRev;
  const vis = $("[data-ma-visore]", pannello);
  if (vis && btn.dataset.maAllegato) {
    vis.dataset.maAllegato = btn.dataset.maAllegato;
    vis.dataset.maSha = btn.dataset.maSha || "";
    vis.dataset.maFile = btn.dataset.maFile || "";
  }
  const f = $("[data-ma-palco-file]", pannello);
  if (f) f.textContent = btn.dataset.maFile || "";
}

function scegliDisegno(btn) {
  const pannello = btn.closest(".ma-anteprima");
  if (!pannello || btn.disabled || !btn.dataset.maAllegato) return;
  const palco = $(".ma-palco[data-ma-palco]", pannello);
  memoria.scrivi(chiave("disegno." + prodotto()), JSON.stringify({ nodo: pannello.dataset.maPannello, allegato: btn.dataset.maAllegato }));
  segnaDisegno(pannello, btn);
  if (palco) disegnaPalco(pannello, palco, btn.dataset.maAllegato, btn.dataset.maSha, btn.dataset.maFile);
}

async function disegnaPalco(pannello, palco, a, v, nome) {
  const mio = ++giroPalco;
  const larghezza = larghezzaDi(palco);
  larghezzaPalco = larghezza;
  const k = `${a}|${v || ""}|${larghezza}`;
  const pagine = $("[data-ma-pagine]", pannello);
  const c = cachePalco.get(k);
  if (c) {
    palco.replaceChildren(c.canvas);
    palco.dataset.maMostra = a;
    if (pagine) pagine.textContent = `pagina 1 di ${c.pagine}`;
    return;
  }
  palco.replaceChildren(el("p", { class: "ma-palco-testo" }, "Apro il disegno…"));
  try {
    const kd = `${a}|${v || ""}`;
    if (!docAperto || docAperto.chiave !== kd) {
      if (docAperto) { docAperto.task.destroy().catch(() => {}); docAperto = null; }
      const aperto = await apriDocumento(a, v);
      if (mio !== giroPalco) { aperto.task.destroy().catch(() => {}); return; }
      docAperto = { chiave: kd, ...aperto };
    }
    const { lib, doc } = docAperto;
    const canvas = await pagina1(lib, doc, larghezza);
    if (mio !== giroPalco) return;
    canvas.className = "ma-palco-canvas";
    canvas.setAttribute("role", "img");
    canvas.setAttribute("aria-label", `${nome || "disegno"}, pagina 1 di ${doc.numPages}`);
    cachePalco.set(k, { canvas, pagine: doc.numPages });
    while (cachePalco.size > TETTO_PALCO) cachePalco.delete(cachePalco.keys().next().value);
    if (palco.isConnected) {
      palco.replaceChildren(canvas);
      palco.dataset.maMostra = a;
    } else {
      // la rilettura di #ma è arrivata nel frattempo: il pannello nuovo prende il canvas dalla cache
      const p2 = $(`#ma-anteprima .ma-anteprima[data-ma-pannello="${cssStr(pannello.dataset.maPannello)}"]:not([hidden]) .ma-palco[data-ma-palco]`);
      if (p2) p2.replaceChildren(canvas);
    }
    const pg = pagine && pagine.isConnected ? pagine : $(`#ma-anteprima .ma-anteprima:not([hidden]) [data-ma-pagine]`);
    if (pg) pg.textContent = `pagina 1 di ${doc.numPages}`;
  } catch (err) {
    if (mio === giroPalco && palco.isConnected) palco.replaceChildren(el("p", { class: "ma-palco-testo ma-errore" }, testoDi(err)));
  }
}

// se il palco cambia larghezza oltre il 10 %, si ridisegna dopo 150 ms (ResizeObserver può mancare)
function osserva(pannello, palco) {
  if (typeof ResizeObserver !== "function") return;
  if (osservaPalco) osservaPalco.disconnect();
  osservaPalco = new ResizeObserver(() => {
    clearTimeout(attesaPalco);
    attesaPalco = setTimeout(() => {
      if (!palco.isConnected || pannello.hidden) return;
      const l = larghezzaDi(palco);
      if (larghezzaPalco && Math.abs(l - larghezzaPalco) / larghezzaPalco <= 0.1) return;
      mostraDisegno(pannello);
    }, 150);
  });
  osservaPalco.observe(palco);
}

// ------------------------------------------------------------------ vista=documenti (FE-13)
// Il fuoco e lo scorrimento vanno a #ma-documenti all'apertura, dopo una navigazione (un collegamento di vista o di
// prodotto) e dopo Indietro/Avanti; NON dopo la rilettura di un gesto né dopo «Aggiorna», che porterebbero via chi
// lavora sull'albero o nell'anteprima (il piano §0.1 e §3.2 dicono «dopo ogni rilettura»: DUBBIO D-FEA-03).

function evidenziaDocumenti(r) {
  if (!r || r.dataset.maVista !== "documenti") return;
  const sez = $("#ma-documenti", r);
  const t = $("#ma-documenti-t", r);
  if (sez && sez.scrollIntoView) sez.scrollIntoView({ block: "start" });
  if (t) t.focus({ preventScroll: true });
}

// ------------------------------------------------------------------ «scelta» di Associa con un documento (FE-11)
// Si accende e si mostra solo il fieldset del componente scelto; gli altri restano spenti e nascosti.

function aggiornaScelte(sel) {
  const f = sel.form || sel.closest("form");
  if (!f) return;
  let acceso = false; // un solo fieldset acceso: due «scelta» e due «motivo» nello stesso modulo non partono mai
  for (const fs of $$("fieldset[data-ma-scelta-per]", f)) {
    const su = !acceso && sel.value !== "" && fs.dataset.maSceltaPer === sel.value;
    if (su) acceso = true;
    fs.disabled = !su;
    fs.hidden = !su;
  }
}

function preparaScelte(r) {
  for (const s of $$("select[data-ma-scelte]", r)) aggiornaScelte(s);
}

// ------------------------------------------------------------------ trascinare = aprire il modulo «Sposta»
// Si trascina solo una casella con data-ma-sposta (il template la mette solo nelle schede bom e senza, con Scrive);
// si lascia solo su una casella con data-ma-drop. Il JS non sa che cosa può stare sotto che cosa: apre una finestra
// con lo STESSO modulo (data-ma-gesto …/sposta, campi da, a, qta) e il ponte lo manda. qta va scritta: obbligatoria,
// almeno 1, intera, mai già messa (difetto 1).

let trascinato = null;

function confermaSposta(n, t) {
  const cod = n.dataset.maCod || "il componente";
  const daCod = n.dataset.maDaCod || "la radice";
  const verso = t.dataset.maCod || "il componente scelto";
  const w = dialogo("Sposta il componente", { sopra: "Sposta · conferma" });
  const qta = el("input", { name: "qta", type: "number", required: true, min: "1", step: "1", inputmode: "numeric" });
  const form = el("form", { class: "ma-gesto ma-avviso-conf", "data-ma-gesto": n.dataset.maSposta },
    el("p", {}, "Spostare ", el("b", { class: "ma-mono" }, cod), " da sotto ", el("b", { class: "ma-mono" }, daCod), " a sotto ", el("b", { class: "ma-mono" }, verso), "?"),
    el("p", { class: "ma-muto ma-piccolo" }, "Che cosa può stare sotto che cosa lo decide il server: se non è ammesso, il rifiuto lo dice e niente cambia."),
    el("input", { type: "hidden", name: "da", value: n.dataset.maDa || "" }),
    el("input", { type: "hidden", name: "a", value: t.dataset.maDrop }),
    el("fieldset", {},
      el("label", { class: "ma-campo ma-campo--corto" }, el("span", {}, "Qtà"), qta),
      el("button", { class: "ma-btn ma-btn--pri" }, "Sposta"),
      el("button", { type: "button", class: "ma-btn", onclick: () => w.chiudi() }, "Annulla")),
    el("small", { class: "ma-muto" }, "Scrivi la quantità: non c'è un valore già messo."));
  // il ponte intercetta il submit (data-ma-gesto) e legge i campi subito; qui si chiude solo la finestra
  form.addEventListener("submit", () => { setTimeout(() => w.chiudi(), 0); });
  w.corpo.append(form);
  qta.focus();
}

// ------------------------------------------------------------------ sezioni che si aprono e si chiudono

function ripristinaDettagli(r) {
  for (const d of $$("details[data-ma-ricorda]", r)) {
    const v = memoria.leggi(chiave("aperti." + d.dataset.maRicorda));
    if (v === "1") d.open = true;
    else if (v === "0") d.open = false;
  }
}

// ------------------------------------------------------------------ finestre (§7.6)
// Le finestre del JS hanno la classe ma-finestra e stanno in document.body, FUORI da #ma: la rilettura del ponte
// non le distrugge. I cassetti resi dal template (dialog#ma-isp-<i>, dialog#ma-ds-<i>) restano in #ma.

function dialogo(titolo, { largo = false, cassetto = false, sopra = "" } = {}) {
  const prima = document.activeElement;
  const d = el("dialog", { class: "ma-finestra ma-dialogo" + (largo ? " ma-dialogo--largo" : "") + (cassetto ? " ma-dialogo--cassetto" : ""), "aria-label": titolo });
  const corpo = el("div", { class: "ma-dialogo-corpo" });
  const piede = el("div", { class: "ma-dialogo-piede" });
  const x = el("button", { type: "button", class: "ma-btn ma-btn--sm ma-btn--ghost", "aria-label": "Chiudi" }, "✕");
  const testa = sopra ? el("div", {}, el("span", { class: "ma-lab" }, sopra), el("h2", {}, titolo)) : el("h2", {}, titolo);
  d.append(el("div", { class: "ma-dialogo-guscio" }, el("header", { class: "ma-dialogo-testa" }, testa, x), corpo, piede));
  x.addEventListener("click", () => d.close());
  d.addEventListener("close", () => {
    d.remove();
    if (prima && prima.isConnected) prima.focus();
  });
  document.body.append(d);
  if (typeof d.showModal === "function") d.showModal(); else d.setAttribute("open", "");
  return { d, corpo, piede, chiudi: () => { if (d.open) d.close(); else if (d.isConnected) d.remove(); } };
}

let apertoDa = null;
function apriCassetto(btn) {
  const d = document.getElementById(btn.dataset.maCassetto || "");
  if (!d || typeof d.showModal !== "function" || d.open) return;
  apertoDa = btn;
  d.showModal();
}

const attesa = (t) => el("p", { class: "ma-attesa" }, t);
const frasi = (a) => (Array.isArray(a) && a.length ? el("ul", { class: "ma-frasi" }, a.map((f) => el("li", {}, String(f)))) : null);

// una scelta che arriva dall'anteprima ({chiave, etichetta}, deleghe anche {spenta}): mai spuntata in partenza,
// salvo la radice già data dal server (data-ma-sorgente)
function scelte(legenda, nome, voci, tipo, scelta) {
  if (!Array.isArray(voci) || !voci.length) return null;
  return el("fieldset", { class: "ma-scelte" }, el("legend", {}, legenda),
    voci.map((v) => el("label", { class: "ma-scelta" + (v.spenta ? " ma-scelta--spenta" : "") },
      el("input", { type: tipo, name: nome, value: v.chiave, checked: tipo === "radio" && scelta === v.chiave, disabled: !!v.spenta }),
      el("span", {}, v.etichetta || v.chiave,
        v.spenta ? el("small", { class: "ma-muto" }, " · non si può: " + v.spenta) : null,
        Array.isArray(v.revoca) && v.revoca.length ? el("small", { class: "ma-muto" }, " · revoca: " + v.revoca.join(", ")) : null))));
}

// i figli diretti dell'anteprima dello STEP: le chiavi di fascicolo.NodoEffetto come sono (RAC-10, nessuna mappatura)
function figliStep(f) {
  if (!Array.isArray(f) || !f.length) return null;
  const riga = (n) => el("li", {},
    el("span", { class: "ma-mono" }, n.codice || n.nome || n.chiave || ""),
    n.nome && n.codice ? " · " + n.nome : "",
    n.qta != null && n.qta !== "" ? ` · ×${n.qta}` : "",
    n.stato ? el("span", { class: "ma-motivo" }, String(n.stato)) : null,
    n.deciso ? el("span", { class: "ma-motivo" }, "deciso") : null,
    n.scartato ? el("span", { class: "ma-motivo" }, "scartato") : null,
    Array.isArray(n.figli) && n.figli.length ? el("ul", { class: "ma-frasi" }, n.figli.map(riga)) : null);
  return el("div", { class: "ma-step-figli" }, el("span", { class: "ma-lab" }, "Figli diretti"), el("ul", { class: "ma-frasi" }, f.map(riga)));
}

// ------------------------------------------------------------------ Autorizza lo STEP strutturale (contratto §3.4 A)
// Prima l'anteprima del server (campi v1.1: gia, autorizzabile, inviabile, figli, deleghe[].spenta e .revoca);
// «Autorizza» acceso solo con inviabile, la firma, la casella «autorizzo» spuntata da chi guarda e la presa d'atto
// se chiesta. Lo manda il ponte (Gesti.esegui); dopo un «ok» rilegge il ponte.

async function apriStep(btn) {
  const t = thread();
  const ds = btn.dataset;
  const cid = ds.maComponente;
  const rif = {};
  if (ds.maDocumento) rif.documento = ds.maDocumento;
  else if (ds.maNodo) rif.nodo = ds.maNodo;
  let sorgente = ds.maSorgente || "";
  const w = dialogo(ds.maFile || "STEP strutturale", { cassetto: true, sopra: "Autorizza lo STEP strutturale · conferma della fonte" });
  const errore = el("p", { class: "ma-errore", role: "alert", hidden: true });
  const annulla = el("button", { type: "button", class: "ma-btn", onclick: w.chiudi }, "Chiudi senza autorizzare");
  const invia = el("button", { type: "button", class: "ma-btn ma-btn--pri", disabled: true }, "Autorizza");
  w.piede.append(errore, annulla, invia);
  let a = null;

  function controlla() {
    const ok = w.corpo.querySelector("input[name=autorizzo]");
    const pa = w.corpo.querySelector("input[name=presa_d_atto]");
    invia.disabled = !(a && a.inviabile === true && a.firma && !a.spento && !a.errore && ok && ok.checked && (!pa || pa.checked));
  }

  async function carica() {
    invia.disabled = true;
    w.corpo.replaceChildren(attesa("Leggo l'anteprima…"));
    const q = new URLSearchParams({ componente: cid, ...rif });
    if (sorgente) q.set("sorgente", sorgente);
    try {
      a = await Gesti.leggi(`/thread/${t}/motore-a/anteprima/step-strutturale?${q}`);
    } catch (err) {
      a = null;
      w.corpo.replaceChildren(el("p", { class: "ma-errore" }, "L'anteprima non si legge: " + testoDi(err)));
      return;
    }
    const pezzi = [
      el("p", { class: "ma-nota" }, `Conferma della fonte di ${ds.maCodice || "questo prodotto"}: non conferma l'albero e non conferma i documenti.`),
      a.errore ? el("p", { class: "ma-errore" }, a.errore) : null,
      a.spento ? el("p", { class: "ma-spento" }, "Non si può autorizzare: " + a.spento) : null,
      a.gia ? el("p", { class: "ma-nota--ver" }, a.gia) : null,
      frasi(a.frasi),
      figliStep(a.figli),
      scelte("Radice da usare", "sorgente", a.sorgenti, "radio", sorgente),
      scelte("Raggruppamenti", "raggruppamento", a.raggruppamenti, "checkbox"),
      scelte("Deleghe", "delega", a.deleghe, "checkbox"),
    ];
    if (a.presa_d_atto) {
      pezzi.push(el("label", { class: "ma-scelta" }, el("input", { type: "checkbox", name: "presa_d_atto", value: "1" }),
        el("span", {}, typeof a.presa_d_atto === "string" ? "Prendo atto: " + a.presa_d_atto : "Prendo atto di quanto scritto sopra.")));
    }
    if (a.inviabile === true && a.firma) {
      pezzi.push(el("label", { class: "ma-scelta ma-firma" }, el("input", { type: "checkbox", name: "autorizzo", value: "1" }),
        el("span", {}, a.bottone || "Autorizzo lo STEP strutturale")));
    } else if (!a.spento && !a.errore) {
      pezzi.push(el("p", { class: "ma-spento" }, "Il server dice che da qui non si può inviare: niente da firmare."));
    }
    w.corpo.replaceChildren(...pezzi.filter(Boolean));
    controlla();
  }

  w.corpo.addEventListener("change", (e) => {
    if (e.target.name === "sorgente") { sorgente = e.target.value; carica(); return; } // la firma cambia con la radice
    controlla();
  });

  invia.addEventListener("click", async () => {
    if (!a || invia.disabled) return;
    invia.disabled = true;
    errore.hidden = true;
    const campi = { ...rif, firma: a.firma, autorizza: "1" };
    if (sorgente) campi.sorgente = sorgente;
    if (w.corpo.querySelector("input[name=presa_d_atto]:checked")) campi.presa_d_atto = "1";
    const rg = $$("input[name=raggruppamento]:checked", w.corpo).map((x) => x.value);
    const dg = $$("input[name=delega]:checked:not(:disabled)", w.corpo).map((x) => x.value);
    if (rg.length) campi.raggruppamento = rg;
    if (dg.length) campi.delega = dg;
    let esito;
    try { esito = await Gesti.esegui(`/thread/${t}/fascicolo/componente/${enc(cid)}/step-strutturale`, campi); } catch (err) { esito = { esito: "errore", testo: testoDi(err) }; }
    if (esito && esito.esito === "ok") { w.chiudi(); return; } // il ponte scrive l'avviso e rilegge la pagina
    errore.textContent = (esito && esito.testo) || "L'autorizzazione non è riuscita: niente è cambiato.";
    errore.hidden = false;
    carica(); // si rilegge l'anteprima: la firma può essere cambiata, e la casella torna vuota
  });

  carica();
}

// ------------------------------------------------------------------ il tipo: prima l'effetto, poi la firma

async function apriTipo(form) {
  const t = thread();
  const cid = form.dataset.maComponente;
  const sel = form.elements.tipo;
  const tipo = sel && sel.value;
  if (!tipo) { if (sel) sel.focus(); return; }
  const nomeTipo = sel.options[sel.selectedIndex].textContent;
  const w = dialogo(`${form.dataset.maCodice || "Componente"}: tipo ${nomeTipo}`, { cassetto: true, sopra: "Cambio del tipo · prima l'effetto" });
  const errore = el("p", { class: "ma-errore", role: "alert", hidden: true });
  const annulla = el("button", { type: "button", class: "ma-btn", onclick: w.chiudi }, "Chiudi senza cambiare");
  const invia = el("button", { type: "button", class: "ma-btn ma-btn--pri", disabled: true }, "Cambia il tipo");
  w.piede.append(errore, annulla, invia);
  let a = null;

  async function carica() {
    invia.disabled = true;
    w.corpo.replaceChildren(attesa("Leggo l'effetto del cambio…"));
    try {
      a = await Gesti.leggi(`/thread/${t}/distinta/tipo?componente=${enc(cid)}&tipo=${enc(tipo)}`);
    } catch (err) {
      a = null;
      w.corpo.replaceChildren(el("p", { class: "ma-errore" }, "L'anteprima non si legge: " + testoDi(err)));
      return;
    }
    w.corpo.replaceChildren(...[
      a.errore ? el("p", { class: "ma-errore" }, a.errore) : null,
      a.spento ? el("p", { class: "ma-spento" }, "Non si può cambiare: " + a.spento) : null,
      frasi(a.frasi) || el("p", { class: "ma-muto" }, "Il server non segnala altri effetti."),
    ].filter(Boolean));
    invia.textContent = a.bottone || "Cambia il tipo";
    invia.disabled = !(a.firma && !a.spento && !a.errore);
  }

  invia.addEventListener("click", async () => {
    if (!a || invia.disabled) return;
    invia.disabled = true;
    errore.hidden = true;
    let esito;
    try { esito = await Gesti.esegui(`/thread/${t}/fascicolo/componente/${enc(cid)}/tipo`, { tipo, firma: a.firma }); } catch (err) { esito = { esito: "errore", testo: testoDi(err) }; }
    if (esito && esito.esito === "ok") { w.chiudi(); return; }
    errore.textContent = (esito && esito.testo) || "Il cambio non è riuscito: niente è cambiato.";
    errore.hidden = false;
    carica();
  });

  carica();
}

// ------------------------------------------------------------------ Conferma l'albero: prima il riepilogo (R4)

// i titoli delle liste del riepilogo (nomi di campi JSON, non valori di enum)
const RIEPILOGO = [
  ["nuovi", "Componenti nuovi"], ["ritrovati", "Componenti ritrovati"], ["tipi", "Tipi"], ["revisioni", "Revisioni"],
  ["legami_nuovi", "Legami nuovi"], ["legami_tolti", "Legami tolti"], ["quantita", "Quantità"], ["cascata", "Tolti a cascata"],
  ["fuori", "Fuori dall'albero"], ["scartate", "Proposte scartate"], ["rimozioni", "Rimozioni"],
  ["commerciali", "Commerciali"], ["vicini", "Codici quasi uguali"], ["quantita_discordi", "Quantità discordi"], ["senza_codice", "Senza codice"],
];

// una voce del riepilogo in una riga: prima il codice e il tipo (se ci sono), poi le altre chiavi; gli oggetti annidati
// si scrivono allo stesso modo fra parentesi, mai come JSON (revisione P3, M6). Le etichette dei valori non ci sono:
// le chiavi restano quelle del server (D-A1d-P3C-04).
const PRIMA = ["codice", "tipo"];
function descrivi(x) {
  if (x == null) return "";
  if (typeof x !== "object") return String(x);
  if (Array.isArray(x)) return x.map(descrivi).join(", ");
  const voci = Object.entries(x)
    .filter(([, v]) => v != null && v !== "" && !(Array.isArray(v) && !v.length));
  const posto = (k) => (PRIMA.includes(k) ? PRIMA.indexOf(k) : PRIMA.length);
  voci.sort(([a], [b]) => posto(a) - posto(b));
  return voci
    .map(([k, v]) => `${k}: ${typeof v === "object" && !Array.isArray(v) ? "(" + descrivi(v) + ")" : descrivi(v)}`)
    .join(" · ");
}

async function apriAlbero() {
  const t = thread();
  const w = dialogo("Riepilogo dell'albero", { cassetto: true, largo: true, sopra: "Conferma dell'albero" });
  const errore = el("p", { class: "ma-errore", role: "alert", hidden: true });
  const annulla = el("button", { type: "button", class: "ma-btn", onclick: w.chiudi }, "Chiudi senza confermare");
  const invia = el("button", { type: "button", class: "ma-btn ma-btn--pri", disabled: true }, "Conferma l'albero");
  w.piede.append(errore, annulla, invia);
  let r = null;

  async function carica() {
    invia.disabled = true;
    w.corpo.replaceChildren(attesa("Leggo il riepilogo…"));
    try {
      r = await Gesti.leggi(`/thread/${t}/distinta/albero/riepilogo`);
    } catch (err) {
      r = null;
      w.corpo.replaceChildren(el("p", { class: "ma-errore" }, "Il riepilogo non si legge: " + testoDi(err)));
      return;
    }
    // le liste con qualcosa si aprono e chiudono; quelle vuote stanno in una riga sola
    const liste = [];
    const vuote = [];
    for (const [k, titolo] of RIEPILOGO) {
      const v = Array.isArray(r[k]) ? r[k] : [];
      if (!v.length) { vuote.push(titolo); continue; }
      liste.push(el("details", { class: "ma-riep" },
        el("summary", {}, el("span", {}, titolo), el("b", {}, String(v.length))),
        el("ul", {}, v.map((i) => el("li", {}, descrivi(i))))));
    }
    const blocchi = Array.isArray(r.blocchi) ? r.blocchi : [];
    w.corpo.replaceChildren(...[
      el("p", { class: "ma-nota" }, "Si conferma l'albero com'è adesso, dopo i gesti fatti nodo per nodo. Vale per tutta la RFQ, non solo per il prodotto scelto. È la conferma della struttura: non conferma la fonte e non conferma i documenti."),
      el("p", { class: "ma-nota" }, "Accettare i nodi uno per uno non rende la BOM verificata: lo fa solo questa conferma."),
      blocchi.length ? el("div", { class: "ma-errore" }, el("b", {}, "Prima di confermare va risolto:"), el("ul", { class: "ma-blocchi" }, blocchi.map((b) => el("li", {}, descrivi(b))))) : null,
      r.confermabile === true ? null : el("p", { class: "ma-spento" }, "Il server dice che l'albero non si può ancora confermare."),
      ...liste,
      vuote.length ? el("p", { class: "ma-muto ma-piccolo" }, "Nessuna voce in: " + vuote.join(" · ") + ".") : null,
    ].filter(Boolean));
    invia.disabled = !(r.confermabile === true && r.firma);
  }

  invia.addEventListener("click", async () => {
    if (!r || invia.disabled) return;
    invia.disabled = true;
    errore.hidden = true;
    let esito;
    // la bozza resta vuota (DB7-5): si conferma l'albero com'è, con la firma del riepilogo appena letto. Dopo un «ok»
    // rilegge il ponte (X-08): qui si chiude solo la finestra.
    try { esito = await Gesti.confermaAlbero(r.firma, { formato: 1, base: r.albero_firma }); } catch (err) { esito = { esito: "errore", testo: testoDi(err) }; }
    if (esito && esito.esito === "ok") { w.chiudi(); return; }
    errore.textContent = (esito && esito.testo) || "La conferma non è riuscita: niente è cambiato.";
    errore.hidden = false;
    carica(); // rifiutata (422): si rilegge il riepilogo
  });

  carica();
}

// ------------------------------------------------------------------ il visore e i dati (sola lettura)

// Il visore dei disegni: barra con il nome del file, zoom, «Scarica il PDF» e «Chiudi»; al centro tutte le pagine
// (pdf.js, per indirizzo); a destra le note, che restano nella pagina del Fascicolo. Il worker viene da pdfjs().
async function apriPdf(btn) {
  const a = btn.dataset.maAllegato;
  const v = btn.dataset.maSha;
  if (!a || !v) return;
  const url = indirizzoPdf(a, v);
  const nome = btn.dataset.maFile || "Disegno";
  const prima = document.activeElement;
  const d = el("dialog", { class: "ma-finestra ma-visore", "aria-label": `Disegno ${nome}` });
  const sotto = el("span", {}, "disegno 2D");
  const zoomTesto = el("span", { class: "ma-z", "aria-live": "polite" }, "Adatta");
  const meno = el("button", { type: "button", "aria-label": "Rimpicciolisci" }, "−");
  const piu = el("button", { type: "button", "aria-label": "Ingrandisci" }, "+");
  const adatta = el("button", { type: "button" }, "Adatta");
  const chiudi = el("button", { type: "button", class: "ma-btn" }, "Chiudi");
  const scena = el("div", { class: "ma-scena", role: "document", "aria-label": `Anteprima di ${nome}` }, attesa("Apro il PDF…"));
  d.append(el("div", { class: "ma-visore-box" },
    el("div", { class: "ma-visore-barra" },
      el("div", { class: "ma-visore-titolo" }, el("b", {}, nome), sotto),
      el("span", { class: "ma-sp" }),
      el("span", { class: "ma-gruppo", role: "group", "aria-label": "Zoom" }, meno, zoomTesto, piu, adatta),
      el("a", { class: "ma-btn", href: url, download: nome }, "Scarica il PDF"),
      chiudi),
    el("div", { class: "ma-visore-corpo" }, scena,
      el("aside", { class: "ma-lato-note", "aria-label": "Note sul disegno" },
        el("span", { class: "ma-lab" }, "Note sul disegno"),
        el("p", { class: "ma-spento" }, "⊘ Non ancora disponibili qui: le note sui PDF restano nella pagina del Fascicolo."))),
    el("div", { class: "ma-visore-piede" }, el("span", {}, "Esc per chiudere"), el("span", {}, "Il PDF è quello del file, così come è arrivato."),
      v ? el("span", { class: "ma-mono" }, `sha256 ${v.slice(0, 12)}…`) : null)));
  chiudi.addEventListener("click", () => d.close());
  document.body.append(d);
  if (typeof d.showModal === "function") d.showModal();
  chiudi.focus();

  let task = null;
  d.addEventListener("close", () => {
    if (task) task.destroy().catch(() => {});
    d.remove();
    if (prima && prima.isConnected) prima.focus();
  });

  const p = await pdfjs();
  if (!p) { scena.replaceChildren(el("p", { class: "ma-errore" }, "Il visore dei PDF non si carica: usa «Scarica il PDF».")); return; }
  const { lib, cartella } = p;
  task = lib.getDocument(opzioniPdf(cartella, { url, worker: lavoratore(lib) }));
  let doc;
  try { doc = await task.promise; } catch (err) {
    if (d.open) scena.replaceChildren(el("p", { class: "ma-errore" }, "Il PDF non si apre: " + testoDi(err)));
    return;
  }
  // zoom: si ridisegna alla scala chiesta (0 = adatta alla larghezza della scena); solo presentazione
  const SCALE = [0.5, 0.75, 1, 1.25, 1.5, 2, 3];
  let zoom = 0;
  let giro = 0;
  async function disegna() {
    const mio = ++giro;
    const pagine = [];
    const largh = Math.max(320, (scena.clientWidth || 900) - 48);
    for (let n = 1; n <= doc.numPages && d.open && mio === giro; n++) {
      const pag = await doc.getPage(n);
      const v1 = pag.getViewport({ scale: 1 });
      const scala = zoom ? zoom * 1.333 : largh / v1.width;
      const dpr = window.devicePixelRatio || 1;
      const vp = pag.getViewport({ scale: scala * dpr });
      const c = el("canvas", { class: "ma-pdf-pagina", role: "img", width: Math.ceil(vp.width), height: Math.ceil(vp.height), "aria-label": `Pagina ${n} di ${doc.numPages}` });
      c.style.width = Math.floor(vp.width / dpr) + "px";
      await pag.render({ canvas: c, viewport: vp }).promise;
      if (mio !== giro) return;
      pagine.push(c);
      scena.replaceChildren(...pagine);
    }
    if (!pagine.length && mio === giro) scena.replaceChildren(el("p", { class: "ma-muto" }, "Il PDF non ha pagine."));
    sotto.textContent = `disegno 2D · ${doc.numPages === 1 ? "1 pagina" : doc.numPages + " pagine"}`;
  }
  function cambia(z) {
    zoom = z;
    zoomTesto.textContent = zoom ? Math.round(zoom * 100) + "%" : "Adatta";
    disegna().catch((err) => { if (d.open) scena.replaceChildren(el("p", { class: "ma-errore" }, "Il PDF non si disegna: " + testoDi(err))); });
  }
  meno.addEventListener("click", () => { const i = SCALE.indexOf(zoom); cambia(zoom ? SCALE[Math.max(0, i - 1)] : 0.75); });
  piu.addEventListener("click", () => { const i = SCALE.indexOf(zoom); cambia(zoom ? SCALE[Math.min(SCALE.length - 1, i + 1)] : 1.25); });
  adatta.addEventListener("click", () => cambia(0));
  cambia(0);
}

// «{ } Dati»: il JSON della pagina (GET /motore-a/dati attraverso il ponte, mai con htmx), in sola lettura
async function apriDati() {
  const t = thread();
  const w = dialogo("Dati e chiamate · sola lettura", { largo: true });
  w.corpo.append(
    el("p", { class: "ma-spento" }, "Contiene dati del cliente e testi dei documenti: solo per l'admin. Non si salva e non si copia nelle prove."),
    attesa("Leggo i dati…"));
  try {
    const j = await Gesti.leggi(`/thread/${t}/motore-a/dati`);
    w.corpo.lastChild.replaceWith(el("pre", { class: "ma-json" }, JSON.stringify(j, null, 2)));
  } catch (err) {
    w.corpo.lastChild.replaceWith(el("p", { class: "ma-errore" }, "I dati non si leggono: " + testoDi(err)));
  }
}

// ------------------------------------------------------------------ eventi (§7.7), installati una volta sola

function azione(x) {
  switch (x.dataset.maAzione) {
    case "ramo": {
      const li = x.closest("li[role='treeitem']");
      apriChiudi(li, li && li.getAttribute("aria-expanded") === "false");
      break;
    }
    case "rami": {
      const sez = x.closest("#ma-schede-albero");
      tuttiIRami(sez && $("[role='tabpanel']:not([hidden])", sez), x.dataset.maAperti === "1");
      break;
    }
    case "scheda": mostraScheda(x.dataset.maGruppo); break;
    case "vai-scheda": {
      if (mostraScheda(x.dataset.maGruppo)) {
        const s = $("#ma-schede-albero");
        if (s && s.scrollIntoView) s.scrollIntoView({ block: "start" });
        const tab = schede().find((t) => t.dataset.maGruppo === x.dataset.maGruppo);
        if (tab) tab.focus({ preventScroll: true });
      }
      break;
    }
    case "disegno": scegliDisegno(x); break;
    case "chiudi-anteprima": chiudiAnteprima(); break;
    case "chiudi-dialogo": { const d = x.closest("dialog"); if (d && d.open) d.close(); break; }
    case "ispeziona": apriCassetto(x); break;
    case "aggiorna": Gesti.aggiorna(); break;
    case "dati": apriDati(); break;
    case "albero": apriAlbero(); break;
    case "step": apriStep(x); break;
    case "pdf": apriPdf(x); break;
    default: break;
  }
}

let vaiA = "";
let fuoco = null;
let navigazione = false; // l'ultimo clic era su un collegamento di vista o di prodotto (hx-get): vedi evidenziaDocumenti

function installa() {
  if (window.__maPagina) return;
  window.__maPagina = true;

  document.addEventListener("click", (e) => {
    const bersaglio = e.target instanceof Element ? e.target : null;
    if (!bersaglio) return;
    // un menu aperto si chiude cliccando altrove
    for (const m of $$("#ma details[data-ma-menu][open]")) if (!m.contains(bersaglio)) m.open = false;
    // l'avviso del ponte si chiude con un clic (resta finché non lo si legge)
    const av = bersaglio.closest("#ma-avviso");
    if (av) { av.textContent = ""; av.removeAttribute("data-esito"); return; }
    // una sezione ricordata aperta o chiusa da chi guarda
    const s = bersaglio.closest("#ma details[data-ma-ricorda] > summary");
    if (s) s.parentElement.dataset.maUtente = "1";
    const ancora = bersaglio.closest("#ma a[hx-push-url*='#']");
    if (ancora) vaiA = new URL(ancora.getAttribute("hx-push-url"), location.href).hash;
    if (bersaglio.closest("#ma a[hx-get]")) navigazione = true;

    const x = bersaglio.closest("[data-ma-azione]");
    if (x && x.closest("#ma") && !x.matches("form")) {
      if (x.disabled) return;
      e.preventDefault();
      azione(x);
      return;
    }
    // un clic sulla casella (non sui suoi controlli) la sceglie
    const n = bersaglio.closest("#ma .ma-dtree .ma-nodo");
    if (n && !bersaglio.closest("a, button, input, select, textarea, label, summary")) {
      const li = n.closest("li[role='treeitem']");
      casellaPrima = li;
      seleziona(li.dataset.maChiave, { apri: true, fuoco: modoFoglio() });
      return;
    }
    // il foglio dal basso: un clic sullo sfondo (fuori dall'anteprima) chiude l'anteprima (§7.7) e il fuoco torna
    // alla casella; Esc invece chiude solo il foglio e la scelta resta
    if (foglioAperto() && !bersaglio.closest("#ma-anteprima") && !bersaglio.closest("dialog") && !bersaglio.closest("#ma-avviso")) chiudiAnteprima();
  });

  document.addEventListener("submit", (e) => {
    const f = e.target instanceof Element ? e.target.closest("form[data-ma-azione]") : null;
    if (!f || !f.closest("#ma")) return;
    e.preventDefault();
    if (f.dataset.maAzione === "tipo") apriTipo(f);
  });

  document.addEventListener("change", (e) => {
    const s = e.target instanceof Element ? e.target.closest("#ma select[data-ma-scelte]") : null;
    if (s) aggiornaScelte(s);
  });

  document.addEventListener("toggle", (e) => {
    const d = e.target;
    if (!(d instanceof HTMLDetailsElement) || !d.closest("#ma")) return;
    // si ricorda solo una scelta di chi guarda (clic o tastiera sul titolo), non l'apertura iniziale né quella del codice
    if (!d.dataset.maUtente) return;
    delete d.dataset.maUtente;
    if (d.dataset.maRicorda) memoria.scrivi(chiave("aperti." + d.dataset.maRicorda), d.open ? "1" : "0");
  }, true);

  document.addEventListener("keydown", (e) => {
    const t = e.target instanceof Element ? e.target : null;
    if (t && t.closest("dialog")) return; // la finestra si chiude da sola con Esc
    if (t && t.closest("#ma details[data-ma-ricorda] > summary") && (e.key === "Enter" || e.key === " ")) {
      t.closest("details").dataset.maUtente = "1";
      return;
    }
    if (e.key === "Tab" && foglioAperto()) { tastiFoglio(e, t); return; }
    if (e.key !== "Escape") {
      if (t && t.closest("#ma #ma-schede")) { tastiSchede(e); return; }
      if (t && t.closest("#ma .ma-dtree")) tastiAlbero(e);
      return;
    }
    const menu = $$("#ma details[data-ma-menu][open]").pop();
    if (menu) {
      menu.open = false;
      const s = $("summary", menu);
      if (s) s.focus();
      e.preventDefault();
      return;
    }
    // l'avviso del ponte resta finché non lo si legge: Esc lo chiude (prima del foglio), come il clic
    const av = $("#ma-avviso");
    if (av && av.textContent.trim() !== "") {
      av.textContent = "";
      av.removeAttribute("data-esito");
      e.preventDefault();
      return;
    }
    // Esc nell'anteprima: il fuoco torna alla casella; sul telefono si chiude anche il foglio
    const nellAnteprima = t && t.closest("#ma-anteprima");
    if (nellAnteprima || foglioAperto()) {
      chiudiFoglio();
      const sel = (casellaPrima && casellaPrima.isConnected && casellaPrima) || $("#ma li[role='treeitem'][aria-selected='true']");
      if (sel) sel.focus();
      e.preventDefault();
    }
  });

  document.addEventListener("close", (e) => {
    const d = e.target;
    if (!(d instanceof HTMLDialogElement) || !d.matches("#ma dialog[id]")) return;
    if (apertoDa && apertoDa.isConnected) apertoDa.focus();
    apertoDa = null;
  }, true);

  // trascinare: apre la conferma di «Sposta», niente altro
  document.addEventListener("dragstart", (e) => {
    const n = e.target instanceof Element ? e.target.closest("#ma .ma-nodo[data-ma-sposta]") : null;
    if (!n) return;
    trascinato = n;
    n.classList.add("ma-trascinato");
    try { e.dataTransfer.effectAllowed = "move"; e.dataTransfer.setData("text/plain", n.dataset.maCod || ""); } catch (err) { /* niente */ }
  });
  document.addEventListener("dragover", (e) => {
    if (!trascinato) return;
    const t = e.target instanceof Element ? e.target.closest("#ma .ma-nodo[data-ma-drop]") : null;
    for (const x of $$("#ma .ma-nodo.ma-drop")) if (x !== t) x.classList.remove("ma-drop");
    if (!t || t === trascinato || t.dataset.maDrop === (trascinato.dataset.maDrop || "")) return;
    e.preventDefault();
    t.classList.add("ma-drop");
  });
  document.addEventListener("dragend", () => {
    for (const x of $$("#ma .ma-nodo.ma-drop, #ma .ma-nodo.ma-trascinato")) x.classList.remove("ma-drop", "ma-trascinato");
    trascinato = null;
  });
  document.addEventListener("drop", (e) => {
    const t = e.target instanceof Element ? e.target.closest("#ma .ma-nodo[data-ma-drop]") : null;
    const n = trascinato;
    if (!n || !t || t === n || t.dataset.maDrop === (n.dataset.maDrop || "")) return;
    e.preventDefault();
    for (const x of $$("#ma .ma-nodo.ma-drop, #ma .ma-nodo.ma-trascinato")) x.classList.remove("ma-drop", "ma-trascinato");
    trascinato = null;
    confermaSposta(n, t);
  });

  // la posizione di lato delle tele si ricorda quando chi guarda le scorre
  document.addEventListener("scroll", (e) => {
    if (e.target instanceof HTMLElement && e.target.matches("#ma .ma-tela")) salvaTele();
  }, true);

  // dopo ogni rilettura di #ma (§7.6)
  document.body.addEventListener("htmx:beforeSwap", (e) => {
    if (!e.detail || !e.detail.target || e.detail.target.id !== "ma") return;
    foglioPrima = foglioAperto();
    fuoco = ricordaFuoco();
    salvaTele();
  });
  document.body.addEventListener("htmx:afterSwap", (e) => {
    if (!e.detail || !e.detail.target || e.detail.target.id !== "ma") return;
    // una navigazione se la richiesta è partita da un collegamento hx-get; senza requestConfig (prove), dal clic
    const rc = e.detail.requestConfig;
    const sorgente = rc && rc.elt instanceof Element ? rc.elt : null;
    const nav = rc ? !!(sorgente && sorgente.matches("a[hx-get]")) : navigazione;
    navigazione = false;
    if (apertoDa && !apertoDa.isConnected) apertoDa = null; // il cassetto è sparito con lo scambio, senza «close»
    avvia();
    ripristinaFuoco(fuoco);
    fuoco = null;
    if (vaiA) {
      const dove = document.getElementById(vaiA.slice(1));
      if (dove && dove.scrollIntoView) dove.scrollIntoView({ block: "start" });
      vaiA = "";
    }
    if (nav) evidenziaDocumenti(radice());
  });
  // Indietro e Avanti: htmx rimette la pagina dalla sua copia senza afterSwap su #ma (il canvas non si copia)
  document.body.addEventListener("htmx:historyRestore", () => {
    navigazione = false;
    avvia();
    evidenziaDocumenti(radice());
  });
  // la finestra si allarga oltre 860 px con il foglio aperto: il foglio non c'è più, la pagina torna usabile
  if (mqFoglio) {
    const cambia = () => { if (!mqFoglio.matches && foglioAperto()) chiudiFoglio(); };
    if (typeof mqFoglio.addEventListener === "function") mqFoglio.addEventListener("change", cambia);
    else if (typeof mqFoglio.addListener === "function") mqFoglio.addListener(cambia);
  }
}

// ------------------------------------------------------------------ il fuoco dopo la rilettura (§7.6)
// Prima dello scambio si annota DOVE era il fuoco in una forma che sopravvive alla rilettura: l'«ancora» stabile che
// lo contiene (il pannello «<gruppo>|<rif>», la scheda di R8 per allegato, la riga di R9 per voce), la «firma»
// dell'elemento (cammino della casella, gruppo della scheda, rotta del gesto, azione con le sue chiavi, sezione
// ricordata) con la sua posizione fra gli uguali, e il controllo dentro un modulo (name). Dopo lo scambio si cerca lo
// stesso elemento; se non c'è più (il gesto l'ha tolto), i ripieghi in ordine: il titolo del pannello, il pulsante
// della riga, il titolo della sezione, la vista corrente. Mai body.

const ANCORE = "[data-ma-pannello], article[data-ma-allegato], tr[data-ma-voce]";

function selettoreAncora(x) {
  if (x.dataset.maPannello != null) return `[data-ma-pannello="${cssStr(x.dataset.maPannello)}"]`;
  if (x.matches("article[data-ma-allegato]")) return `article[data-ma-allegato="${cssStr(x.dataset.maAllegato)}"]`;
  return `tr[data-ma-voce="${cssStr(x.dataset.maVoce || "")}"]`;
}

const datoAttr = (k) => "data-" + k.replace(/[A-Z]/g, (m) => "-" + m.toLowerCase());

function firmaDi(x) {
  if (x.matches("li[role='treeitem']")) return `li[data-ma-cammino="${cssStr(x.dataset.maCammino || "")}"]`;
  if (x.matches("[role='tab']")) return `[role='tab'][data-ma-gruppo="${cssStr(x.dataset.maGruppo || "")}"]`;
  if (x.dataset.maGesto) return `[data-ma-gesto="${cssStr(x.dataset.maGesto)}"]`;
  if (x.dataset.maAzione) {
    let s = `[data-ma-azione="${cssStr(x.dataset.maAzione)}"]`;
    for (const k of ["maGruppo", "maAllegato", "maAperti", "maCassetto"]) {
      if (x.dataset[k] != null) s += `[${datoAttr(k)}="${cssStr(x.dataset[k])}"]`;
    }
    return s;
  }
  if (x.matches("summary")) {
    const d = x.parentElement;
    if (d && d.dataset.maRicorda) return `details[data-ma-ricorda="${cssStr(d.dataset.maRicorda)}"] > summary`;
    if (d && d.hasAttribute("data-ma-menu")) return "details[data-ma-menu] > summary";
    if (d && d.id) return `#${cssStr(d.id)} > summary`;
  }
  if (x.id) return `#${cssStr(x.id)}`;
  return null;
}

function titoloDi(a) {
  const sez = a.closest("section[aria-labelledby]");
  if (sez) return `#${cssStr(sez.getAttribute("aria-labelledby").split(/\s+/)[0])}`;
  const d = a.closest("details[id]");
  return d ? `#${cssStr(d.id)} > summary` : null;
}

function ricordaFuoco() {
  const m = $("#ma");
  let a = document.activeElement;
  if (!m || !(a instanceof Element) || !m.contains(a)) return null;
  // un cassetto del template (dialog in #ma) sparisce con lo scambio: il fuoco torna al pulsante che lo apre
  const dlg = a.closest("dialog[id]");
  if (dlg) {
    apertoDa = null;
    const apri = $(`[data-ma-cassetto="${cssStr(dlg.id)}"]`, m);
    if (!apri) return { ancora: null, firma: null, i: 0, dentro: null, ripieghi: ["#ma-viste [aria-current]", "[data-ma-azione='aggiorna']"] };
    a = apri;
  }
  const ric = { ancora: null, firma: null, i: 0, dentro: null, ripieghi: [] };
  const anc = a.closest(ANCORE);
  if (anc) ric.ancora = selettoreAncora(anc);
  const link = a.closest("a[hx-get]");
  if (link) {
    // dopo la navigazione: la voce corrente dello stesso elenco (viste, prodotti, tabella del riepilogo)
    const cont = link.parentElement && link.parentElement.closest("[id]");
    if (cont) ric.ripieghi.push(`#${cssStr(cont.id)} [aria-current]`);
  } else {
    let x = a;
    const g = a.closest("[data-ma-gesto]");
    if (g && g !== a) {
      x = g;
      ric.dentro = a.getAttribute("name") ? `[name="${cssStr(a.getAttribute("name"))}"]` : (a.matches("button") ? "button" : null);
    }
    ric.firma = firmaDi(x);
    if (ric.firma) ric.i = Math.max(0, $$(ric.firma, anc || m).indexOf(x));
    if (x.matches("li[role='treeitem']")) {
      ric.ripieghi.push(`li[data-ma-chiave="${cssStr(x.dataset.maChiave || "")}"]`, "#ma-schede [aria-selected='true']");
    }
    if (a.closest("#ma-anteprima")) {
      if (anc) ric.ripieghi.push(`${ric.ancora}:not([hidden]) .ma-anteprima-t`);
      ric.ripieghi.push("#ma-anteprima .ma-anteprima:not([hidden]) .ma-anteprima-t");
    } else if (anc) {
      ric.ripieghi.push(`${ric.ancora} [data-ma-azione='ispeziona']`, `${ric.ancora} button:not([disabled])`);
    }
  }
  const tit = titoloDi(a);
  if (tit) ric.ripieghi.push(tit);
  ric.ripieghi.push("#ma-viste [aria-current]", "[data-ma-azione='aggiorna']");
  return ric;
}

function prova(f) {
  if (!f || nascosto(f)) return false;
  if (f.matches("form")) f = $("button:not([type='button'])", f) || $("button", f);
  if (!f) return false;
  if (!f.matches("a[href], button, input, select, textarea, summary, [tabindex]")) f.setAttribute("tabindex", "-1");
  f.focus({ preventScroll: true });
  return document.activeElement === f;
}

function ripristinaFuoco(ric) {
  const m = $("#ma");
  if (!ric || !m) return;
  if (ric.firma) {
    const dove = ric.ancora ? $(ric.ancora, m) : m;
    if (dove) {
      const uguali = $$(ric.firma, dove);
      const x = uguali[Math.min(ric.i, uguali.length - 1)];
      if (x && prova((ric.dentro && $(ric.dentro, x)) || x)) return;
      if (x && ric.dentro && prova(x)) return;
    }
  }
  for (const s of ric.ripieghi) if (prova($(s, m))) return;
}

// ------------------------------------------------------------------ avvio

function avvia() {
  const r = radice();
  if (!r) return;
  // dopo Indietro/Avanti la copia di htmx può avere il foglio aperto e la pagina inert: si riparte puliti
  const w = ws();
  if (w) w.classList.remove("ma-ws--foglio");
  togliModale(r);
  ripristinaDettagli(r);
  ripristinaRami(r);
  ripristinaScheda(r);
  preparaTabindex(r);
  ripristinaTele(r);
  preparaScelte(r);
  ripristinaSelezione(r);
  miniature(r);
}

installa();
avvia();
evidenziaDocumenti(radice());
