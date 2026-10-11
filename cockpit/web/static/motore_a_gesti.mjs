// motore_a_gesti.mjs — il ponte dei gesti della Distinta nuova del motore A (giro 5, A1d; contratto A1d §3.1-§3.3 e
// §5; v1.1 §4.9; piano di A1d backend §5.7; T-A1d-08, T-A1d-30, T-A1d-31, T-A1d-32; RAC-13, RAC-14, RAC-15).
//
// È del backend e ha un'API fissa (contratto §3.2): la pagina /thread/{id}/motore-a lo importa da motore_a.mjs
// («import * as Gesti from "./motore_a_gesti.mjs"», con l'importmap di P-10 che porta all'indirizzo con l'impronta) e
// non lo modifica.
//
//   esegui(url, campi)            → Promise<{esito: "ok"|"no"|"errore", testo}>; dopo "ok" rilegge #ma
//   leggi(url)                    → GET JSON di sola lettura della lista LETTURE → l'oggetto; un Error con il testo
//   confermaAlbero(firma, bozza)  → POST JSON di «Conferma l'albero» → {esito, testo}; dopo "ok" rilegge #ma (X-08)
//   aggiorna()                    → Promise<boolean>: rilegge #ma con htmx; true = riletta
//
// e intercetta da solo click e submit degli elementi con data-ma-gesto (POST), con data-ma-campi (JSON di campi fissi;
// gli array diventano campi ripetuti) e data-ma-domanda (confirm() prima dell'invio).
//
// Le regole (contratto §3.1; ELENCO BE-13):
// - è l'unico punto della pagina che chiama le rotte che scrivono, e solo quelle della lista chiusa ROTTE: stessa
//   origine, l'indirizzo senza parametri, il {id} uguale alla RFQ della pagina (senza badare alle maiuscole, RAC-15);
//   «Prepara i file» mai con auto (DB7-6). La RFQ si prende solo dal percorso della pagina: fuori da
//   /thread/{id}/motore-a nessun gesto parte (T-A1d-32);
// - ogni richiesta porta HX-Request: true e HX-Current-URL = l'indirizzo della pagina: così threadFrammento risponde
//   con il solo <div data-ma-esito="ok|no">testo</div> (contratto §3.1, T-A1d-07), mai con la vecchia interfaccia.
//   Le anteprime dei PDF (GET /allegato/{id}/anteprima) non passano di qui: le apre motore_a.mjs, senza HX-*;
// - il corpo di una risposta non entra mai nella pagina: DOMParser lo legge in un documento a parte, che non esegue
//   niente e non si inserisce; nella pagina va solo il testo, con textContent, in #ma-avviso;
// - gli esiti: 2xx con un solo <div data-ma-esito="ok|no"> → "ok" o "no"; 2xx senza → "errore" (non si sa se il
//   gesto è riuscito: mai un successo); 4xx e 5xx → "errore" con il testo; 401, HX-Redirect o un rimando seguito
//   fino a /login → la sessione è scaduta, si va al login. Il ponte non classifica le frasi: l'esito lo dice il
//   server (rispondiMotoreA, l'elenco chiuso di T-A1d-11);
// - dopo ogni "ok", anche di confermaAlbero, rilegge #ma con htmx (GET dell'indirizzo della pagina, contratto §3.3);
//   dopo "no" o "errore" no. Se la rilettura non riesce, #ma-avviso lo dice (T-A1d-31): mai ricaricare la pagina da
//   sé, che perderebbe l'avviso;
// - un gesto alla volta per elemento: il secondo clic mentre il primo è in corso non parte.
//
// Vincoli del file, perché il kit lo carichi anche togliendo gli export (piano del frontend §9.3): nessun import,
// nessun await fuori dalle funzioni, solo le globali del DOM (window, document, location, fetch, DOMParser, URL,
// URLSearchParams, FormData, confirm).

const INSTALLATO = "__maGesti";
const FATTO = "Fatto: il server non ha aggiunto altro."; // P-09: un "ok" senza testo
const NIENTE = "Niente è cambiato.";
const RILETTURA = "La pagina non si è riletta: ricaricala per vedere lo stato nuovo.";
const SESSIONE = "Sessione scaduta: si torna al login.";
const SENZA_ESITO =
  "La risposta non dice l'esito (manca data-ma-esito): non si sa se il gesto è riuscito. Ricarica la pagina per vedere lo stato.";

// ROTTE:inizio — i gesti del contratto A1d §3.4, più il ripristino (v1.1 §4.6). POST con campi di form. {id} = la RFQ
// della pagina, {u} = un UUID. Fuori per costruzione: bom/applica, congela, revisione/*, prepara con auto.
const ROTTE = [
  "/thread/{id}/fascicolo/componente/{u}/step-strutturale",
  "/thread/{id}/fascicolo/componente/{u}/step-strutturale/revoca",
  "/thread/{id}/fascicolo/nodo/{u}/accetta",
  "/thread/{id}/fascicolo/nodo/{u}/scarta",
  "/thread/{id}/fascicolo/nodo/{u}/codice",
  "/thread/{id}/fascicolo/nodo/{u}/riapri",
  "/thread/{id}/fascicolo/componente/{u}/codice",
  "/thread/{id}/fascicolo/componente/{u}/modifica",
  "/thread/{id}/fascicolo/componente/{u}/tipo",
  "/thread/{id}/fascicolo/componente/{u}/collega",
  "/thread/{id}/fascicolo/componente/{u}/sposta",
  "/thread/{id}/fascicolo/componente/{u}/scollega",
  "/thread/{id}/fascicolo/componente/{u}/archivia",
  "/thread/{id}/fascicolo/componente/{u}/rimuovi",
  "/thread/{id}/fascicolo/componente/{u}/ripristina",
  "/thread/{id}/fascicolo/relazione/accetta",
  "/thread/{id}/fascicolo/relazione/scarta",
  "/thread/{id}/fascicolo/rimozione/accetta",
  "/thread/{id}/fascicolo/rimozione/scarta",
  "/thread/{id}/fascicolo/conferma",
  "/proposta/{u}/scarta",
  "/thread/{id}/fascicolo/assegna",
  "/thread/{id}/fascicolo/documento/{u}/sostituisci",
  "/thread/{id}/fascicolo/componente/{u}/deroga",
  "/thread/{id}/fascicolo/deroga/{u}/revoca",
  "/thread/{id}/riprova-copie",
  "/thread/{id}/fascicolo/prepara",
  "/thread/{id}/fascicolo/rianalizza",
  "/thread/{id}/smistamento/aggiorna",
];
// ROTTE:fine

// LETTURE:inizio — le GET JSON di sola lettura della pagina (con la query).
const LETTURE = [
  "/thread/{id}/motore-a/dati",
  "/thread/{id}/motore-a/anteprima/step-strutturale",
  "/thread/{id}/distinta/tipo",
  "/thread/{id}/distinta/albero/riepilogo",
];
// LETTURE:fine

// «Conferma l'albero»: fuori da ROTTE, la chiama solo confermaAlbero, con la RFQ della pagina (X-08).
const ALBERO = "/thread/{id}/distinta/albero/conferma";

const U = "[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}";
const PAGINA = new RegExp("^/thread/(" + U + ")/motore-a/?$");

// compila: una rotta della lista in una RegExp ancorata; {id} = un UUID catturato (la RFQ), {u} = un UUID qualunque, il
// resto alla lettera.
function compila(rotta) {
  let re = "^";
  for (const pezzo of rotta.split(/(\{id\}|\{u\})/)) {
    if (pezzo === "{id}") re += "(" + U + ")";
    else if (pezzo === "{u}") re += U;
    else re += pezzo.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  }
  return new RegExp(re + "$");
}

const RE_ROTTE = ROTTE.map(compila);
const RE_LETTURE = LETTURE.map(compila);

// threadID: la RFQ della pagina, solo dal percorso /thread/<uuid>/motore-a (la / finale tollerata); "" altrove.
function threadID() {
  const m = PAGINA.exec(location.pathname);
  return m ? m[1] : "";
}

// ammessa: l'indirizzo di un gesto o di una lettura, se è nella lista; altrimenti il motivo. Le rotte si confrontano
// sul percorso già normalizzato da URL.
function ammessa(url, elenco) {
  let u;
  try {
    u = new URL(String(url), location.href);
  } catch (e) {
    return { motivo: "indirizzo non valido" };
  }
  if (u.origin !== location.origin) return { motivo: "rotta di un'altra origine" };
  const t = threadID();
  if (!t) return { motivo: "il ponte vale solo nella pagina /thread/{id}/motore-a" };
  for (const re of elenco) {
    const m = re.exec(u.pathname);
    if (!m) continue;
    if (m[1] !== undefined && m[1].toLowerCase() !== t.toLowerCase()) return { motivo: "rotta di un'altra RFQ" };
    return { url: u };
  }
  return { motivo: "rotta non prevista dal contratto" };
}

// aggiungi: i campi in URLSearchParams. Un oggetto (gli array diventano campi ripetuti; null e undefined si saltano),
// delle coppie, URLSearchParams o FormData (i file si saltano). Altro: TypeError, e il gesto non parte.
function aggiungi(p, campi) {
  if (campi === undefined || campi === null) return p;
  if (typeof campi !== "object") throw new TypeError("campi non validi");
  if (campi instanceof URLSearchParams || (typeof FormData !== "undefined" && campi instanceof FormData)) {
    for (const [k, v] of campi.entries()) if (typeof v === "string") p.append(k, v);
    return p;
  }
  if (Array.isArray(campi)) {
    for (const [k, v] of campi) p.append(String(k), String(v));
    return p;
  }
  for (const [k, v] of Object.entries(campi)) {
    if (v === undefined || v === null) continue;
    if (Array.isArray(v)) v.forEach((x) => p.append(k, String(x)));
    else p.append(k, String(v));
  }
  return p;
}

function intestazioni(extra) {
  return Object.assign({ "HX-Request": "true", "HX-Current-URL": location.href }, extra || {});
}

function messaggio(err) {
  return (err && err.message) || String(err);
}

// rimandoAlLogin: la sessione scaduta. aLogin (server.go) risponde 401 con HX-Redirect a una richiesta htmx; senza
// htmx un 302 che fetch segue fino a /login. Ritorna dove andare, oppure "".
function rimandoAlLogin(risp) {
  const red = risp.headers.get("HX-Redirect");
  if (red) return red;
  if (risp.status === 401) return "/login";
  try {
    if (risp.redirected && new URL(risp.url, location.href).pathname === "/login") return "/login";
  } catch (e) {
    // un indirizzo che non si legge non è il login
  }
  return "";
}

// vaiAlLogin: solo un percorso della stessa origine; un indirizzo di fuori diventa /login.
function vaiAlLogin(dove) {
  let destinazione = "/login";
  try {
    const u = new URL(dove || "/login", location.href);
    if (u.origin === location.origin) destinazione = u.pathname + u.search;
  } catch (e) {
    // resta /login
  }
  location.assign(destinazione);
}

// testoPiano: il testo di una risposta d'errore, per #ma-avviso. Un corpo HTML si legge con DOMParser (solo per
// leggere); spazi compattati, al più 400 caratteri.
function testoPiano(corpo) {
  if (!corpo) return "";
  let t = String(corpo);
  if (/^\s*</.test(t) && typeof DOMParser !== "undefined") {
    t = new DOMParser().parseFromString(t, "text/html").body.textContent || "";
  }
  t = t.replace(/\s+/g, " ").trim();
  return t.length > 400 ? t.slice(0, 400) + "…" : t;
}

// leggiEsito: l'esito di un gesto. Il corpo deve essere un solo <div data-ma-esito> e nient'altro: niente testo o
// elementi fuori, niente nella testa del documento letto. Un esito vuoto: "ok" → FATTO (P-09), "no" → NIENTE. Un altro
// valore → "errore". Un corpo d'altra forma (la vecchia interfaccia, una pagina intera) → null.
function leggiEsito(corpo) {
  if (typeof DOMParser === "undefined") return null;
  const doc = new DOMParser().parseFromString(String(corpo || ""), "text/html");
  if (!doc.body || (doc.head && doc.head.childNodes.length > 0)) return null;
  const el = doc.body.firstElementChild;
  if (!el || doc.body.childElementCount !== 1 || el.tagName !== "DIV" || !el.hasAttribute("data-ma-esito")) return null;
  for (const n of doc.body.childNodes) {
    if (n !== el && (n.nodeType !== 3 || n.textContent.trim() !== "")) return null;
  }
  const v = el.getAttribute("data-ma-esito");
  const testo = (el.textContent || "").replace(/\s+/g, " ").trim();
  if (v === "ok") return { esito: "ok", testo: testo || FATTO };
  if (v === "no") return { esito: "no", testo: testo || NIENTE };
  return { esito: "errore", testo: "Esito sconosciuto «" + v + "»: non si sa se il gesto è riuscito. " + testo };
}

// mostra: l'esito in #ma-avviso, solo testo.
function mostra(esito, testo) {
  const el = document.getElementById("ma-avviso");
  if (!el) return;
  el.textContent = testo || "";
  el.setAttribute("data-esito", esito);
  el.removeAttribute("data-rilettura");
  el.hidden = false;
}

function finisci(esito, testo) {
  mostra(esito, testo);
  return { esito, testo };
}

// avvisaRilettura: la pagina non si è riletta (T-A1d-31). L'avviso di prima resta, con la frase in più una volta sola.
function avvisaRilettura() {
  const el = document.getElementById("ma-avviso");
  if (!el) return;
  const prima = (el.textContent || "").trim();
  if (!prima.endsWith(RILETTURA)) el.textContent = prima ? prima + " " + RILETTURA : RILETTURA;
  el.setAttribute("data-rilettura", "no");
  el.hidden = false;
}

// postHTTP: un gesto con campi di form. Mai il corpo nella pagina: o il login, o {esito, testo}.
async function postHTTP(percorso, p) {
  const risp = await fetch(percorso, {
    method: "POST",
    credentials: "same-origin",
    headers: intestazioni({ "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" }),
    body: p.toString(),
  });
  const login = rimandoAlLogin(risp);
  if (login) return { login };
  const corpo = await risp.text();
  if (!risp.ok) return { esito: "errore", testo: testoPiano(corpo) || "Errore " + risp.status };
  return leggiEsito(corpo) || { esito: "errore", testo: SENZA_ESITO };
}

// ---------------------------------------------------------------- l'API (contratto §3.2)

export async function esegui(url, campi) {
  const a = ammessa(url, RE_ROTTE);
  if (!a.url) return finisci("errore", "Il gesto non è partito: " + a.motivo + ".");
  if (a.url.search !== "") return finisci("errore", "Il gesto non è partito: le rotte dei gesti non hanno parametri nell'indirizzo.");
  let p;
  try {
    p = aggiungi(new URLSearchParams(), campi);
  } catch (e) {
    return finisci("errore", "I campi del gesto non si leggono: niente è partito.");
  }
  if (/\/fascicolo\/prepara$/.test(a.url.pathname) && p.has("auto")) {
    return finisci("errore", "Il gesto non è partito: “Prepara i file” è solo il pulsante (DB7-6).");
  }
  // le rotte /proposta/ rispondono con la pagina del thread solo con ritorna_thread: sempre la RFQ della pagina
  if (a.url.pathname.startsWith("/proposta/")) p.set("ritorna_thread", threadID());
  let r;
  try {
    r = await postHTTP(a.url.pathname, p);
  } catch (err) {
    r = { esito: "errore", testo: "Il gesto non è partito: " + messaggio(err) };
  }
  if (r.login) {
    vaiAlLogin(r.login);
    return { esito: "errore", testo: SESSIONE };
  }
  mostra(r.esito, r.testo);
  if (r.esito === "ok") await aggiorna();
  return { esito: r.esito, testo: r.testo };
}

export async function leggi(url) {
  const a = ammessa(url, RE_LETTURE);
  if (!a.url) throw new Error("Lettura non partita: " + a.motivo + ".");
  const risp = await fetch(a.url.pathname + a.url.search, {
    credentials: "same-origin",
    headers: intestazioni({ Accept: "application/json" }),
  });
  const login = rimandoAlLogin(risp);
  if (login) {
    vaiAlLogin(login);
    throw new Error(SESSIONE);
  }
  const corpo = await risp.text();
  if (!risp.ok) throw new Error(testoPiano(corpo) || "Errore " + risp.status);
  if ((risp.headers.get("Content-Type") || "").toLowerCase().indexOf("json") < 0) throw new Error("La risposta non è JSON.");
  return JSON.parse(corpo);
}

export async function confermaAlbero(firma, bozza) {
  const t = threadID();
  if (!t) return finisci("errore", "La conferma non è partita: il ponte vale solo nella pagina /thread/{id}/motore-a.");
  let r;
  try {
    const risp = await fetch(ALBERO.replace("{id}", t), {
      method: "POST",
      credentials: "same-origin",
      headers: intestazioni({ "Content-Type": "application/json", Accept: "application/json" }),
      body: JSON.stringify({ firma, bozza }),
    });
    const login = rimandoAlLogin(risp);
    if (login) {
      r = { login };
    } else {
      const testo = await risp.text();
      const json = (risp.headers.get("Content-Type") || "").toLowerCase().indexOf("json") >= 0;
      let j = null;
      try {
        j = json ? JSON.parse(testo) : null;
      } catch (e) {
        j = null;
      }
      if (j === null || typeof j !== "object") j = null;
      if (risp.status === 200 && j) r = { esito: "ok", testo: String(j.testo || "") || FATTO };
      else if (risp.status === 422 && j) r = { esito: "no", testo: String(j.errore || "") || NIENTE };
      else r = { esito: "errore", testo: (j && String(j.errore || j.testo || "")) || testoPiano(testo) || "Errore " + risp.status };
    }
  } catch (err) {
    r = { esito: "errore", testo: "La conferma non è partita: " + messaggio(err) };
  }
  if (r.login) {
    vaiAlLogin(r.login);
    return { esito: "errore", testo: SESSIONE };
  }
  mostra(r.esito, r.testo);
  if (r.esito === "ok") await aggiorna(); // X-08: la rilettura la fa il ponte, non motore_a.mjs
  return { esito: r.esito, testo: r.testo };
}

// aggiorna: rilegge #ma con htmx (contratto §3.3). Riuscita = il primo figlio di #ma è un nodo nuovo: htmx 2.0.4
// inserisce i nodi nuovi e toglie i vecchi; una 4xx o 5xx non sostituisce niente; un 401 con HX-Redirect lo segue
// htmx. Altrimenti l'avviso (T-A1d-31).
//
// Una rilettura alla volta (R-05 della revisione di A1d-P2): htmx 2.0.4, con la coda predefinita «last», mette in coda
// una seconda richiesta sullo stesso #ma mentre la prima è in volo e ne risolve subito la promessa; due gesti finiti
// vicini darebbero un falso «La pagina non si è riletta». Chi chiama mentre una rilettura è in volo aspetta che finisca
// e poi ne parte una sola, condivisa da tutti quelli che sono arrivati nel frattempo.
let rilettura = null; // la rilettura in volo
let prossima = null; // quella che parte dopo, condivisa
export function aggiorna() {
  if (!rilettura) {
    rilettura = rileggi().finally(() => {
      rilettura = null;
    });
    return rilettura;
  }
  if (!prossima) {
    prossima = rilettura.then(() => {
      prossima = null;
      return aggiorna();
    });
  }
  return prossima;
}

async function rileggi() {
  const ma = document.getElementById("ma");
  const hx = typeof window !== "undefined" ? window.htmx : undefined;
  if (!ma || !hx || typeof hx.ajax !== "function") {
    avvisaRilettura();
    return false;
  }
  const prima = ma.firstElementChild;
  try {
    await hx.ajax("GET", location.href, { target: "#ma", swap: "innerHTML" });
  } catch (e) {
    avvisaRilettura();
    return false;
  }
  const dopo = ma.firstElementChild;
  const riuscita = dopo !== null && dopo !== prima;
  if (!riuscita) avvisaRilettura();
  return riuscita;
}

// ---------------------------------------------------------------- l'intercettazione

async function lancia(el, fd, url) {
  if (!url || el.getAttribute("data-ma-in-corso") === "1") return; // doppio clic: il primo è ancora in corso
  const domanda = el.getAttribute("data-ma-domanda");
  if (domanda && !window.confirm(domanda)) return;
  const p = new URLSearchParams();
  if (fd) aggiungi(p, fd);
  const fissi = el.getAttribute("data-ma-campi");
  if (fissi) {
    try {
      aggiungi(p, JSON.parse(fissi));
    } catch (e) {
      mostra("errore", "I campi del gesto non si leggono (data-ma-campi): niente è partito.");
      return;
    }
  }
  const bottoni = el.tagName === "FORM" ? [...el.querySelectorAll("button, input[type=submit]")] : [el];
  const spenti = bottoni.filter((b) => !b.disabled);
  el.setAttribute("data-ma-in-corso", "1");
  el.setAttribute("aria-busy", "true");
  spenti.forEach((b) => {
    b.disabled = true;
  });
  try {
    await esegui(url, p);
  } finally {
    el.removeAttribute("data-ma-in-corso");
    el.removeAttribute("aria-busy");
    spenti.forEach((b) => {
      if (b.isConnected) b.disabled = false;
    });
  }
}

function alClick(e) {
  const el = e.target && e.target.closest ? e.target.closest("[data-ma-gesto]") : null;
  if (!el || el.tagName === "FORM") return; // i form passano dal submit
  e.preventDefault();
  if (el.disabled || el.getAttribute("aria-disabled") === "true") return;
  lancia(el, null, el.getAttribute("data-ma-gesto"));
}

function alSubmit(e) {
  const f = e.target;
  if (!f || f.tagName !== "FORM" || !f.hasAttribute("data-ma-gesto")) return;
  e.preventDefault();
  let fd;
  try {
    fd = new FormData(f, e.submitter || undefined);
  } catch (err) {
    fd = new FormData(f);
  }
  lancia(f, fd, f.getAttribute("data-ma-gesto"));
}

if (typeof window !== "undefined" && typeof document !== "undefined" && !window[INSTALLATO]) {
  window[INSTALLATO] = true;
  document.addEventListener("click", alClick);
  document.addEventListener("submit", alSubmit);
}
