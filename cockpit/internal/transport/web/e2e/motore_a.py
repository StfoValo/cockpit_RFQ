# -*- coding: utf-8 -*-
"""L4 - giro 5, A1d: la Distinta nuova del motore A (/thread/<id>/motore-a) in un BROWSER VERO (piano A1d.3; BE-19;
V-40; piano A1d backend §9.3; piano A1d frontend §4.6 e §9).

Non lo si lancia a mano: lo avvia `motore_a_browser_test.go` (tag `integrazione browser`), con la scena di quel file
(la RFQ ACME delle L4 di A1d, due PDF veri, la scrittura sul NAS spenta, il flag acceso con le regole ACME) e la
«sonda»: un indirizzo che restituisce la fotografia delle righe della RFQ (componenti, legami, documenti, proposte,
copie sul NAS, righe degli STEP, deroghe, versioni, job) e che con POST /spegni spegne il flag.

La pagina e' dell'amministratore: lo script entra con la sigla e la password che gli da' il test. Legge la pagina solo
con i selettori stabili del piano del frontend (§4.6): #ma-avviso[data-esito], #ma, .ma[data-ma-thread][data-ma-prodotto]
[data-ma-vista], #ma-viste, #ma-prodotti, #ma-schede, li[role=treeitem][data-ma-chiave], [data-ma-mini],
#ma-anteprima .ma-anteprima[data-ma-pannello], [data-ma-gesto], [data-ma-azione=aggiorna], #ma-documenti.

Le prove (lettere, scelte dal test Go con --prove; si eseguono nell'ordine dato, sulla stessa RFQ):
  A  apertura da amministratore: nessun errore in console, nessun testo del programma a vista, la mappa del ponte
     prima del modulo, nessuna scrittura e nessuna POST all'apertura (DB7-6)
  B  cambio di scheda dell'albero e di prodotto; vista=documenti con l'evidenza e il fuoco sul titolo
  C  miniature: solo GET /allegato/<id>/anteprima, senza HX-Request (il registro delle richieste del browser)
  D  l'anteprima persistente: la casella scelta resta scelta dopo la rilettura di #ma e dopo il ricaricamento
  E  un gesto rifiutato con 200 («Riprova le copie» con la scrittura sul NAS spenta): «no», #ma non riletto, la sonda
     invariata
  F  un gesto riuscito («Scarta» della proposta aperta): «ok», #ma riletto, una riga cambiata nella sonda
  G  il flag spento a meta' prova: la pagina risponde 404 alla ricarica (va per ultima)

I clienti, i codici e i nomi dei file sono inventati (ACME, 712xxxx, acme.example).
"""
import argparse
import json
import os
import re
import sys
import time
import urllib.request

from playwright.sync_api import sync_playwright

PROVE = []


def prova(lettera, nome):
    def deco(fn):
        PROVE.append((lettera, nome, fn))
        return fn
    return deco


class Rotto(AssertionError):
    pass


def verifica(condizione, messaggio):
    if not condizione:
        raise Rotto(messaggio)


# ------------------------------------------------------------------ il database, dalla sonda

def differenza(prima, dopo):
    """La differenza fra due fotografie: per gli elenchi le righe in piu' (+) e in meno (-), per le mappe le chiavi
    cambiate (valore di prima -> valore di dopo)."""
    out = {}
    for k in sorted(set(prima) | set(dopo)):
        a, b = prima.get(k), dopo.get(k)
        if isinstance(a, list) or isinstance(b, list):
            a, b = a or [], b or []
            piu = [x for x in b if x not in a]
            meno = [x for x in a if x not in b]
            if piu or meno:
                out[k] = {"+": piu, "-": meno}
        else:
            a, b = a or {}, b or {}
            cambi = {x: (a.get(x), b.get(x)) for x in sorted(set(a) | set(b)) if a.get(x) != b.get(x)}
            if cambi:
                out[k] = cambi
    return out


# ------------------------------------------------------------------ la pagina

# I controlli della pagina che valgono sempre (RAC-23: #ma ha un solo figlio, la radice .ma; nessun pezzo del layout
# dentro #ma; nessun id doppio; nessun testo del programma a vista).
JS_SANA = r"""() => {
  const p = [];
  const conta = (s) => document.querySelectorAll(s).length;
  if (conta('#ma') !== 1) p.push('#ma: ' + conta('#ma'));
  if (conta('#ma-avviso') !== 1) p.push('#ma-avviso: ' + conta('#ma-avviso'));
  const ma = document.getElementById('ma');
  if (ma && (ma.children.length !== 1 || !ma.firstElementChild.matches('.ma[data-ma-thread][data-ma-prodotto][data-ma-vista]')))
    p.push('#ma deve avere un solo figlio, la radice .ma: ' + (ma ? ma.children.length : 0) + ' figli');
  if (conta('#ma #ma, #ma #ma-avviso, #ma header.barra, #ma nav.rail, #ma main, #ma html, #ma body')) p.push('pezzi del layout dentro #ma');
  const ids = {};
  for (const e of document.querySelectorAll('[id]')) ids[e.id] = (ids[e.id] || 0) + 1;
  const doppi = Object.entries(ids).filter(([, n]) => n > 1).map(([k, n]) => k + '×' + n);
  if (doppi.length) p.push('id doppi: ' + doppi.join(', '));
  if (conta('button button, a button, button a')) p.push('controlli dentro un controllo');
  const testo = document.body.innerText;
  for (const s of ['ZgotmplZ', 'undefined', 'NaN', '[object Object]', '%!', '<no value>', '{{', '}}', 'null ·', '· null']) {
    if (testo.includes(s)) p.push('testo strano a vista: «' + s + '»');
  }
  return p;
}"""

# La mappa del ponte (P-10; RAC-17): un solo <script type=importmap>, prima del modulo della pagina, con l'impronta.
JS_MAPPA = r"""() => {
  const mappe = document.querySelectorAll('script[type=importmap]');
  const modulo = document.querySelector('script[type=module][src*="motore_a.mjs"]');
  if (mappe.length !== 1 || !modulo) return 'mappe ' + mappe.length + ', modulo ' + !!modulo;
  if (!(mappe[0].compareDocumentPosition(modulo) & Node.DOCUMENT_POSITION_FOLLOWING)) return 'la mappa viene dopo il modulo';
  let m;
  try { m = JSON.parse(mappe[0].textContent); } catch (e) { return 'la mappa non e\' JSON: ' + e; }
  const v = (m.imports || {})['/static/motore_a_gesti.mjs'] || '';
  if (!/^\/static\/motore_a_gesti\.mjs\?v=[0-9a-f]+$/.test(v)) return 'la mappa porta il ponte a «' + v + '»';
  if (document.querySelector('script[src*="motore_a_gesti"]')) return 'il ponte e\' caricato anche da un <script>';
  return '';
}"""

# La casella scelta e il suo pannello: la chiave, se la casella ha aria-selected e se il pannello e' visibile.
JS_SCELTA = r"""(k) => {
  const li = [...document.querySelectorAll('#ma li[role=treeitem]')].filter((e) => e.dataset.maChiave === k);
  const pan = [...document.querySelectorAll('#ma-anteprima .ma-anteprima')].filter((e) => e.dataset.maPannello === k);
  const altri = [...document.querySelectorAll('#ma-anteprima .ma-anteprima')].filter((e) => e.dataset.maPannello !== k && !e.hidden);
  return {
    caselle: li.length,
    scelta: li.some((e) => e.getAttribute('aria-selected') === 'true'),
    pannelli: pan.length,
    visibile: pan.length === 1 && !pan[0].hidden && pan[0].offsetParent !== null,
    altri: altri.length,
  };
}"""

# La prima casella visibile dell'albero (nel pannello della scheda aperta).
JS_PRIMA_CASELLA = r"""() => {
  for (const li of document.querySelectorAll('#ma li[role=treeitem][data-ma-chiave]')) {
    if (li.offsetParent !== null) return li.dataset.maChiave;
  }
  return '';
}"""

RE_ANTEPRIMA = re.compile(r"^/allegato/[0-9a-f-]{36}/anteprima$")
# revisione P3, S-04: la query e' sempre v=<sha256>, mai vuota
RE_V = re.compile(r"^v=[0-9a-f]{64}$")


class Banco:
    def __init__(self, page, a):
        self.page, self.a = page, a
        self.base = a.url + "/thread/" + a.thread + "/motore-a"
        self.richieste = []  # (metodo, url, intestazioni) di ogni richiesta del browser
        self.attesi = set()  # gli stati >= 400 attesi dalla prova in corso (G: 404)
        self.note = []

    # ---- il database
    def db(self):
        with urllib.request.urlopen(self.a.sonda) as r:
            return json.loads(r.read().decode("utf-8"))

    def spegni(self):
        req = urllib.request.Request(self.a.sonda + "/spegni", data=b"", method="POST")
        with urllib.request.urlopen(req) as r:
            verifica(r.status == 204, "la sonda non ha spento il flag: %d" % r.status)

    # ---- la pagina
    def apri(self, query=""):
        p = self.page
        p.goto(self.base + query)
        p.wait_for_load_state("domcontentloaded")
        self.pronta()

    def pronta(self):
        """Aspetta che la pagina abbia finito: #ma c'e', nessuna richiesta htmx in corso."""
        p = self.page
        p.wait_for_function("() => document.querySelector('#ma > .ma')", timeout=15000)
        p.wait_for_function("() => !document.querySelector('.htmx-request')", timeout=15000)
        p.wait_for_timeout(150)

    def radice(self, attributo):
        return self.page.locator("#ma > .ma").get_attribute(attributo)

    def marca(self):
        """Segna la radice di #ma: dopo una rilettura del ponte (htmx) la radice e' un nodo nuovo, senza il segno."""
        self.page.evaluate("() => { const r = document.querySelector('#ma > .ma'); if (r) r.dataset.maProvaVecchia = '1'; }")

    def riletta(self, timeout=15000):
        self.page.wait_for_function("() => { const r = document.querySelector('#ma > .ma'); return r && !r.dataset.maProvaVecchia; }",
                                    timeout=timeout)
        self.pronta()

    def ancora_vecchia(self):
        return self.page.evaluate("() => { const r = document.querySelector('#ma > .ma'); return !!(r && r.dataset.maProvaVecchia); }")

    def pulisci_avviso(self):
        self.page.evaluate("() => { const a = document.getElementById('ma-avviso'); if (a) { a.textContent = ''; delete a.dataset.esito; } }")

    def avviso(self, timeout=20000):
        """L'esito del gesto che il ponte scrive in #ma-avviso: (data-esito, testo)."""
        self.page.wait_for_function("() => { const a = document.getElementById('ma-avviso'); return a && (a.dataset.esito || '') !== ''; }",
                                    timeout=timeout)
        el = self.page.locator("#ma-avviso")
        return el.get_attribute("data-esito"), " ".join(el.inner_text().split())

    def prodotti(self):
        """Gli indirizzi dei prodotti della RFQ, come li da' la pagina (#ma-prodotti a)."""
        return self.page.evaluate("() => [...document.querySelectorAll('#ma-prodotti a[href]')].map((a) => a.getAttribute('href'))")

    def trova(self, selettore, viste=("", "?vista=documenti")):
        """Apre la pagina in ogni vista data e per ogni prodotto finche' il selettore c'e'. True se l'ha trovato."""
        for v in viste:
            self.apri(v)
            if self.page.locator(selettore).count():
                return True
            for href in self.prodotti():
                q = href[href.index("?"):] if "?" in href else ""
                if v and "vista=" not in q:
                    q = (q + "&" if q else "?") + v[1:]
                self.apri(q)
                if self.page.locator(selettore).count():
                    return True
        return False

    def clic(self, loc):
        """Un clic da persona se il controllo si vede; altrimenti l'evento di clic sull'elemento (un pulsante dentro un
        cassetto chiuso): lo intercetta lo stesso il ponte."""
        if loc.is_visible():
            loc.click()
        else:
            loc.dispatch_event("click")

    def sana(self, contesto):
        problemi = self.page.evaluate(JS_SANA)
        verifica(not problemi, "%s: la pagina non e' sana: %s" % (contesto, "; ".join(problemi)))

    def foto(self, nome):
        if self.a.foto:
            os.makedirs(self.a.foto, exist_ok=True)
            self.page.screenshot(path=os.path.join(self.a.foto, nome), full_page=True)

    def nota(self, testo):
        self.note.append(testo)


# ------------------------------------------------------------------ le prove

@prova("A", "apertura da amministratore: pagina sana, mappa del ponte, nessuna scrittura e nessuna POST")
def prova_a(b):
    prima = b.db()
    del b.richieste[:]
    b.apri()
    b.sana("apertura")
    verifica(b.radice("data-ma-thread") == b.a.thread, "la radice dice il thread %s" % b.radice("data-ma-thread"))
    verifica(b.radice("data-ma-vista") == "distinta", "la vista di partenza e' %s" % b.radice("data-ma-vista"))
    verifica(b.page.locator("#ma[data-ma-pdfjs][data-ma-pdfjs-worker]").count() == 1, "#ma senza gli indirizzi di pdf.js")
    errore = b.page.evaluate(JS_MAPPA)
    verifica(not errore, "la mappa del ponte: %s" % errore)
    verifica(b.page.evaluate("() => !!window.__maGesti"), "il ponte non si e' installato (window.__maGesti)")
    b.page.wait_for_timeout(1000)  # un poll o un prepara partirebbero qui
    post = [u for (m, u, _) in b.richieste if m != "GET"]
    verifica(not post, "l'apertura ha mandato richieste che scrivono: %s" % post)
    d = differenza(prima, b.db())
    verifica(not d, "aprire la pagina ha scritto nel database: %s" % json.dumps(d, ensure_ascii=False))
    b.foto("A_apertura.png")


@prova("B", "cambio di scheda e di prodotto, poi vista=documenti con l'evidenza")
def prova_b(b):
    prima = b.db()
    b.apri()
    p = b.page
    schede = p.locator("#ma-schede [role=tab]")
    if schede.count() >= 2:
        seconda = schede.nth(1)
        seconda.click()
        verifica(seconda.get_attribute("aria-selected") == "true", "la scheda scelta non e' selezionata")
        pannello = seconda.get_attribute("aria-controls")
        verifica(pannello and p.locator("#" + pannello).is_visible(), "il pannello della scheda %s non si vede" % pannello)
        altri = p.evaluate("(id) => [...document.querySelectorAll('#ma [role=tabpanel]')].filter((e) => e.id !== id && !e.hidden).length", pannello)
        verifica(altri == 0, "con la seconda scheda si vedono %d altri pannelli" % altri)
        b.sana("cambio di scheda")
    else:
        b.nota("B: il prodotto di partenza ha %d schede: il cambio di scheda non si prova" % schede.count())

    prodotti = p.locator("#ma-prodotti a")
    verifica(prodotti.count() >= 2, "la scena ha due prodotti, la pagina ne mostra %d" % prodotti.count())
    corrente = b.radice("data-ma-prodotto")
    altro = None
    for i in range(prodotti.count()):
        if prodotti.nth(i).get_attribute("aria-current") != "true":
            altro = prodotti.nth(i)
            break
    verifica(altro is not None, "nessun prodotto oltre a quello corrente")
    b.marca()
    altro.click()
    b.riletta()
    nuovo = b.radice("data-ma-prodotto")
    verifica(nuovo and nuovo != corrente, "il prodotto non e' cambiato: %s -> %s" % (corrente, nuovo))
    verifica(p.locator("#ma-prodotti a[aria-current=true]").count() == 1, "prodotti correnti: %d" % p.locator("#ma-prodotti a[aria-current=true]").count())
    verifica("prodotto=" in p.url, "l'indirizzo non dice il prodotto: %s" % p.url)
    b.sana("cambio di prodotto")

    link = p.locator("#ma-viste a[href*='vista=documenti']")
    verifica(link.count() == 1, "link della vista documenti: %d" % link.count())
    b.marca()
    link.click()
    b.riletta()
    verifica(b.radice("data-ma-vista") == "documenti", "la vista e' %s" % b.radice("data-ma-vista"))
    corrente_vista = p.locator("#ma-viste a[aria-current=page]")
    verifica(corrente_vista.count() == 1 and "vista=documenti" in (corrente_vista.get_attribute("href") or ""), "la vista corrente nei link non e' documenti")
    verifica(p.locator("#ma-documenti.ma-evidenza").count() == 1, "il riepilogo documentale non e' evidenziato")
    p.wait_for_function("() => document.activeElement && document.activeElement.id === 'ma-documenti-t'", timeout=5000)
    b.sana("vista documenti")
    # la stessa vista dall'indirizzo, a pagina intera
    b.apri("?vista=documenti")
    verifica(p.locator("#ma-documenti.ma-evidenza").count() == 1, "vista=documenti dall'indirizzo: nessuna evidenza")
    d = differenza(prima, b.db())
    verifica(not d, "schede, prodotti e viste hanno scritto nel database: %s" % json.dumps(d, ensure_ascii=False))


@prova("C", "miniature: solo GET /allegato/<id>/anteprima, senza HX-Request")
def prova_c(b):
    p = b.page
    del b.richieste[:]
    viste = 0
    for v in ("", "?vista=documenti"):
        b.apri(v)
        indirizzi = [""] + [h[h.index("?"):] for h in b.prodotti() if "?" in h]
        for q in indirizzi:
            if q:
                if v and "vista=" not in q:
                    q = q + "&" + v[1:]
                b.apri(q)
            n = p.locator("[data-ma-mini][data-ma-allegato]").count()
            if not n:
                continue
            viste += 1
            # le miniature si chiedono quando entrano nella vista (IntersectionObserver)
            for i in range(n):
                try:
                    p.locator("[data-ma-mini][data-ma-allegato]").nth(i).scroll_into_view_if_needed(timeout=2000)
                except Exception:
                    pass
            p.wait_for_timeout(2500)
    verifica(viste, "nessuna miniatura nella pagina, in nessun prodotto e in nessuna vista: la scena non prova niente")
    allegati = [(m, u, h) for (m, u, h) in b.richieste if "/allegato/" in u]
    verifica(allegati, "nessuna richiesta di un'anteprima: le miniature non sono partite")
    for m, u, h in allegati:
        percorso, _, query = u.partition("?")
        percorso = re.sub(r"^https?://[^/]+", "", percorso)
        verifica(m == "GET", "una miniatura ha chiesto %s %s" % (m, u))
        verifica(RE_ANTEPRIMA.match(percorso) and RE_V.match(query), "una miniatura ha chiesto un indirizzo diverso: %s" % u)
        verifica("hx-request" not in {k.lower() for k in h}, "una miniatura ha chiesto con HX-Request: %s" % u)
    p.wait_for_function("() => document.querySelector('[data-ma-mini] img, [data-ma-mini] canvas')", timeout=15000)
    b.foto("C_miniature.png")


@prova("D", "l'anteprima persistente dopo la rilettura di #ma e dopo il ricaricamento")
def prova_d(b):
    p = b.page
    trovata = b.trova("#ma li[role=treeitem][data-ma-chiave]")
    verifica(trovata, "nessuna casella nell'albero, in nessun prodotto")
    k = p.evaluate(JS_PRIMA_CASELLA)
    verifica(k, "nessuna casella visibile")
    i = p.evaluate("(k) => [...document.querySelectorAll('#ma li[role=treeitem]')].findIndex((e) => e.dataset.maChiave === k && e.offsetParent !== null)", k)
    casella = p.locator("#ma li[role=treeitem]").nth(i)
    nodo = casella.locator(":scope > .ma-nodo")
    (nodo if nodo.count() else casella).first.click()  # il clic sulla casella, non sui suoi controlli (§7.7)
    p.wait_for_function("(k) => [...document.querySelectorAll('#ma-anteprima .ma-anteprima')].some((e) => e.dataset.maPannello === k && !e.hidden)", arg=k, timeout=10000)
    s = p.evaluate(JS_SCELTA, k)
    verifica(s["scelta"] and s["visibile"] and s["pannelli"] == 1 and s["altri"] == 0, "la casella %s scelta: %s" % (k, s))
    url = p.url

    # la rilettura di #ma dal pulsante «Aggiorna» (Gesti.aggiorna, htmx): la scelta resta
    agg = p.locator("#ma [data-ma-azione=aggiorna]")
    verifica(agg.count() >= 1, "nessun pulsante «Aggiorna» nella pagina")
    b.marca()
    b.clic(agg.first)
    b.riletta()
    p.wait_for_function("(k) => [...document.querySelectorAll('#ma-anteprima .ma-anteprima')].some((e) => e.dataset.maPannello === k && !e.hidden)", arg=k, timeout=10000)
    s = p.evaluate(JS_SCELTA, k)
    verifica(s["scelta"] and s["visibile"] and s["altri"] == 0, "dopo la rilettura di #ma la casella %s: %s" % (k, s))
    b.sana("dopo la rilettura")

    # il ricaricamento della stessa scheda del browser (sessionStorage): la scelta resta
    p.reload()
    p.wait_for_load_state("domcontentloaded")
    b.pronta()
    verifica(p.url == url, "il ricaricamento ha cambiato l'indirizzo: %s -> %s" % (url, p.url))
    p.wait_for_function("(k) => [...document.querySelectorAll('#ma-anteprima .ma-anteprima')].some((e) => e.dataset.maPannello === k && !e.hidden)", arg=k, timeout=10000)
    s = p.evaluate(JS_SCELTA, k)
    verifica(s["scelta"] and s["visibile"] and s["altri"] == 0, "dopo il ricaricamento la casella %s: %s" % (k, s))
    b.sana("dopo il ricaricamento")
    b.foto("D_anteprima.png")


@prova("E", "un gesto rifiutato con 200 (Riprova le copie, NAS spento): «no», #ma non riletto, la sonda invariata")
def prova_e(b):
    p = b.page
    sel = '#ma [data-ma-gesto$="/riprova-copie"]'
    verifica(b.trova(sel, ("?vista=documenti", "")), "nessun «Riprova le copie» nella pagina, in nessun prodotto")
    btn = p.locator(sel)
    verifica(btn.count() == 1, "«Riprova le copie» nella pagina: %d, atteso uno solo" % btn.count())
    verifica(btn.is_enabled(), "«Riprova le copie» spento per l'amministratore")
    prima = b.db()
    b.pulisci_avviso()
    b.marca()
    with p.expect_response(lambda r: r.url.endswith("/riprova-copie") and r.request.method == "POST", timeout=20000) as risp:
        b.clic(btn)
    verifica(risp.value.status == 200, "riprova-copie: %d" % risp.value.status)
    esito, testo = b.avviso()
    verifica(esito == "no", "il rifiuto e' mostrato come %s: «%s»" % (esito, testo))
    verifica("Copie NON rimesse in coda" in testo, "il testo del rifiuto: «%s»" % testo)
    p.wait_for_timeout(500)
    verifica(b.ancora_vecchia(), "dopo un «no» il ponte ha riletto #ma")
    d = differenza(prima, b.db())
    verifica(not d, "il rifiuto ha scritto nel database: %s" % json.dumps(d, ensure_ascii=False))
    b.sana("dopo il rifiuto")
    b.foto("E_rifiuto.png")


@prova("F", "un gesto riuscito (Scarta della proposta aperta): «ok», #ma riletto, una riga cambiata")
def prova_f(b):
    p = b.page
    sel = '#ma [data-ma-gesto="/proposta/%s/scarta"]' % b.a.proposta
    verifica(b.trova(sel, ("?vista=documenti", "")), "nessuno «Scarta» della proposta %s nella pagina" % b.a.proposta)
    btn = p.locator(sel).first
    verifica(btn.is_enabled() or not btn.is_visible(), "«Scarta» spento per l'amministratore")
    prima = b.db()
    b.pulisci_avviso()
    b.marca()
    with p.expect_response(lambda r: ("/proposta/%s/scarta" % b.a.proposta) in r.url and r.request.method == "POST", timeout=20000) as risp:
        b.clic(btn)
    verifica(risp.value.status == 200, "scarta: %d" % risp.value.status)
    esito, testo = b.avviso()
    verifica(esito == "ok", "lo scarto e' %s: «%s»" % (esito, testo))
    b.riletta()
    dopo = b.db()
    d = differenza(prima, dopo)
    verifica(list(d) == ["proposte"], "lo scarto ha cambiato %s: %s" % (sorted(d), json.dumps(d, ensure_ascii=False)))
    cambi = d["proposte"]
    verifica(len(cambi) == 1, "proposte cambiate: %s" % cambi)
    nome, (a, z) = next(iter(cambi.items()))
    verifica(a and a.startswith("aperta|") and z and z.startswith("scartata|"), "la proposta di %s: %s -> %s" % (nome, a, z))
    verifica(p.locator(sel).count() == 0, "dopo lo scarto «Scarta» c'e' ancora")
    b.sana("dopo lo scarto")
    # il ricaricamento dice lo stesso
    p.reload()
    p.wait_for_load_state("domcontentloaded")
    b.pronta()
    verifica(p.locator(sel).count() == 0, "dopo il ricaricamento «Scarta» c'e' ancora")
    b.foto("F_scarto.png")


@prova("G", "il flag spento a meta' prova: la pagina risponde 404 alla ricarica")
def prova_g(b):
    p = b.page
    b.apri()
    b.spegni()
    b.attesi.add(404)
    try:
        risp = p.reload()
        verifica(risp is not None and risp.status == 404, "a flag spento la ricarica risponde %s" % (risp.status if risp else None))
        verifica(p.locator("#ma").count() == 0, "a flag spento la pagina c'e' ancora")
    finally:
        b.attesi.discard(404)


def main():
    ap = argparse.ArgumentParser()
    for k in ["url", "sonda", "thread", "proposta"]:
        ap.add_argument("--" + k, required=True)
    ap.add_argument("--ip", default="10.0.0.5:51000")
    ap.add_argument("--sigla", default="AD")
    ap.add_argument("--password", default="prova-ad")
    ap.add_argument("--canale", default="msedge")
    ap.add_argument("--vedi", action="store_true")
    ap.add_argument("--prove", default="ABCDEFG")
    ap.add_argument("--foto", default="")
    a = ap.parse_args()
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

    falliti = []
    fatte = 0
    note = {l for l, _, _ in PROVE}
    ignote = [l for l in a.prove if l not in note]
    if ignote:
        print("prove sconosciute: %s" % ignote)
        return 2
    with sync_playwright() as pw:
        browser = pw.chromium.launch(channel=a.canale, headless=not a.vedi)
        ctx = browser.new_context(extra_http_headers={"X-Prova-IP": a.ip}, viewport={"width": 1440, "height": 900})
        page = ctx.new_page()
        b = Banco(page, a)
        errori = []
        page.on("dialog", lambda d: d.accept())
        page.on("pageerror", lambda e: errori.append("pageerror: %s" % e))
        page.on("console", lambda m: errori.append("console: %s" % m.text) if m.type == "error" and "Failed to load resource" not in m.text else None)
        page.on("request", lambda r: b.richieste.append((r.method, r.url, dict(r.headers))))
        page.on("response", lambda r: errori.append("risposta %d: %s %s" % (r.status, r.request.method, r.url))
                if r.status >= 400 and r.status not in b.attesi and not r.url.endswith("/favicon.ico") else None)

        page.goto(a.url + "/login")
        page.fill("input[name=sigla]", a.sigla)
        page.fill("input[name=password]", a.password)
        page.click("button[type=submit]")
        page.wait_for_url("**/inbox*", timeout=10000)
        per_lettera = {l: (l, n, f) for l, n, f in PROVE}
        for lettera, nome, fn in [per_lettera[l] for l in a.prove]:  # nell'ordine chiesto: sono una sequenza
            fatte += 1
            inizio = time.time()
            prima = len(errori)
            try:
                fn(b)
                if len(errori) > prima:
                    raise Rotto("errori nella pagina: %s" % errori[prima:])
                print("  %s  %-90s PASSATO   (%.1fs)" % (lettera, nome, time.time() - inizio))
            except Exception as e:
                del errori[prima:]
                falliti.append(lettera)
                print("  %s  %-90s FALLITO   %s" % (lettera, nome, e))
                if a.foto:
                    try:
                        b.foto("errore_%s.png" % lettera)
                    except Exception:
                        pass
        for n in b.note:
            print("  nota: %s" % n)
        if errori:
            print("  errori nella pagina: %s" % errori)
            falliti.append("javascript")
        browser.close()
    print("\n%d prove nel browser: %d passate, %d fallite" % (fatte, fatte - len([f for f in falliti if f != "javascript"]), len(falliti)))
    return 1 if falliti else 0


if __name__ == "__main__":
    sys.exit(main())
