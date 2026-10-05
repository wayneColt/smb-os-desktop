/* SMB OS Desktop \u2014 app.js
   One classic script for index.html (no build step, no modules, no third-party code).
   The page is a desktop: icons, movable windows, the slab dock and the Apex command surface. Every number it shows
   comes from the local runtime (aios) over same-origin /api; when aios does not answer, the page says
   "NO SIGNAL from aios" and shows no numbers at all. Every non-GET request carries X-AIOS: 1.
   Classes are the vendored design system's wx-* components (./ds); layout lives in app.css.
   Pure functions are exported for node tests when `module` exists; the DOM half runs only in a browser. */

(function () {
  "use strict";

  /* ------------------------- pure functions ------------------------- */

  var FILTER_FIELDS = ["label", "alias", "hint", "name"];
  var TIER_WORDS = ["read", "reversible", "needs APPROVE"];
  var STEP_NAMES = ["Declared", "Approved", "Actuated", "Observed"];
  var DASH = "\u2014";

  function str(v) { return v == null ? "" : String(v); }
  function isObj(v) { return v !== null && typeof v === "object" && !Array.isArray(v); }

  function filterItems(query, items) {
    if (!Array.isArray(items)) return [];
    var tokens = str(query).toLowerCase().split(/\s+/).filter(Boolean);
    if (!tokens.length) return items.slice();
    return items.filter(function (it) {
      if (!isObj(it)) return false;
      var hay = FILTER_FIELDS.map(function (f) { return str(it[f]); }).join(" ").toLowerCase();
      return tokens.every(function (t) { return hay.indexOf(t) !== -1; });
    });
  }

  var ACRONYMS = { ar: "AR", kpi: "KPI", kpis: "KPIs", evv: "EVV", id: "ID", ot: "OT", gp: "GP", pto: "PTO", ro: "RO" };
  function humanize(key) {
    var s = str(key).replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/[_\-]+/g, " ").trim().toLowerCase();
    if (!s) return "";
    s = s.split(/\s+/).map(function (w) { return ACRONYMS[w] || w; }).join(" ");
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  function toNum(v) {
    if (typeof v === "number") return v;
    if (typeof v === "string" && v.trim() !== "") return Number(v);
    return NaN;
  }

  function fmtNumber(n, maxFrac) {
    if (typeof n !== "number" || !isFinite(n)) return DASH;
    var mf = maxFrac != null ? maxFrac : (Number.isInteger(n) ? 0 : 2);
    var s = Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: 0, maximumFractionDigits: mf });
    return (n < 0 ? "\u2212" : "") + s;
  }

  function fmtMoney(n) {
    if (typeof n !== "number" || !isFinite(n)) return DASH;
    var s = Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
    return (n < 0 ? "\u2212$" : "$") + s;
  }

  // A value with its unit, as aios reported it ("$" and "%" are drawn; any other unit is appended).
  function fmtValue(v, unit) {
    var n = toNum(v);
    if (!isFinite(n)) return DASH;
    var u = str(unit).trim();
    if (u === "$" || u.toUpperCase() === "USD") return fmtMoney(n);
    if (u === "%") return fmtNumber(n, 1) + "%";
    var s = fmtNumber(n, Math.abs(n) >= 100 ? 0 : 2);
    return u ? s + " " + u : s;
  }

  // Generic field formatting for outputs whose shape the page does not special-case.
  function fmtField(key, v) {
    if (typeof v === "boolean") return v ? "yes" : "no";
    if (typeof v !== "number" || !isFinite(v)) return str(v);
    var k = str(key).toLowerCase();
    var countish = /hour|count|pct|percent|rate|days|qty|quantity|visit|cars?$|seq|ratio|util|minute|shift|punch|_n$|^n$|number/.test(k);
    if (/(pct|percent)$/.test(k)) return fmtNumber(v, 1) + "%";              // aios sends percentages already scaled (54.1)
    if (/rate$/.test(k)) return fmtNumber(Math.abs(v) <= 1 ? v * 100 : v, 2) + "%"; // agreement rates arrive as fractions (0.06)
    if (!countish && /amount|revenue|royalt|ad_?fund|fee|balance|sales|price|cost|wage|debit|credit|deposit|payment|gross|net|total|dollar|usd|invoice|pay$/.test(k)) return fmtMoney(v);
    return fmtNumber(v);
  }

  // Better / worse against target, stated in words and a glyph so colour never carries it alone.
  function kpiCue(k) {
    var v = toNum(k && k.value), t = toNum(k && k.target);
    if (!k || !isFinite(v) || !isFinite(t)) return { dir: "unknown", cls: "nosignal", word: "NO TARGET", glyph: "?", text: "no target to compare", delta: null };
    var higher = str(k.better).toLowerCase() !== "lower";
    var d = v - t;
    var rule = higher ? "higher is better" : "lower is better";
    if (Math.abs(d) < 1e-9) return { dir: "on", cls: "pass", word: "ON TARGET", glyph: "=", text: "on target \u00B7 " + rule, delta: 0 };
    var good = higher ? d > 0 : d < 0;
    var amount = fmtValue(Math.abs(d), k.unit);
    return {
      dir: good ? "better" : "worse",
      cls: good ? "verified" : "miss",
      word: good ? "BETTER" : "WORSE",
      glyph: d > 0 ? "\u25B2" : "\u25BC",
      text: (d > 0 ? "\u25B2 " + amount + " above" : "\u25BC " + amount + " below") + " target \u00B7 " + rule,
      delta: d
    };
  }

  function tierTag(t) {
    var n = Number(t);
    if (t === null || t === undefined || t === "" || !isFinite(n) || n < 0 || n > 2 || n % 1 !== 0) return { cls: "nosignal", text: "Tier ?" };
    return { cls: "tier" + n, text: "Tier " + n + " \u00B7 " + TIER_WORDS[n] };
  }

  function hashPrefix(h, n) {
    var s = str(h);
    var lim = n || 12;
    return s.length > lim ? s.slice(0, lim) + "\u2026" : s;
  }

  // Lists from aios may arrive bare or wrapped ({"proposals": [...]}); anything else is no list.
  function listOf(json, key) {
    if (Array.isArray(json)) return json.filter(Boolean);
    if (isObj(json) && Array.isArray(json[key])) return json[key].filter(Boolean);
    return [];
  }

  function sevTag(sev) {
    var s = str(sev).toLowerCase();
    if (s === "crit" || s === "critical") return { cls: "miss", text: "CRIT" };
    if (s === "warn" || s === "warning") return { cls: "hold", text: "WARN" };
    if (s === "info") return { cls: "reported", text: "INFO" };
    return { cls: "nosignal", text: s ? s.toUpperCase() : "NO_SIGNAL" };
  }

  // The GateInstrument's four steps. A step lights only after the one before it (never skips).
  function gateView(steps) {
    var out = [];
    var prevLit = true;
    for (var i = 0; i < STEP_NAMES.length; i++) {
      var s = (Array.isArray(steps) && isObj(steps[i])) ? steps[i] : {};
      var lit = s.lit === true && prevLit;
      var waiting = !lit && prevLit && s.waiting === true;
      var receipt = s.lit === true && !lit ? "NO_SIGNAL \u00B7 an earlier step is dark" : str(s.receipt || "NO_SIGNAL \u00B7 not reached");
      out.push({ name: STEP_NAMES[i], lit: lit, waiting: waiting, lamp: lit ? "on" : waiting ? "now" : "unknown",
        cls: lit ? "is-lit" : waiting ? "is-waiting" : "", receipt: receipt });
      prevLit = lit;
    }
    return out;
  }

  function actuatedText(output) {
    if (output == null) return "NO_SIGNAL \u00B7 no output returned";
    if (isObj(output)) {
      var p = output.files || output.outbox || output.path || output.file || output.written || output.wrote;
      if (Array.isArray(p)) {
        if (p.length > 1) {
          var dir = str(p[0]).indexOf("/") !== -1 ? str(p[0]).slice(0, str(p[0]).lastIndexOf("/") + 1) : "";
          return "wrote " + p.length + " files" + (dir ? " to " + dir : "");
        }
        p = p[0];
      }
      if (p) return "wrote " + str(p);
    }
    return "ran \u00B7 output returned";
  }

  // Steps for one tier-2 proposal: computed from what aios returned, never from a timer.
  function proposalSteps(p, outcome) {
    var h = hashPrefix(p && p.hash, 12);
    var steps = [{ lit: !!(p && p.hash), receipt: "proposal \u00B7 " + h }];
    var o = outcome || null;
    if (o && o.status === "done") {
      var r = isObj(o.receipt) ? o.receipt : {};
      steps.push({ lit: true, receipt: "APPROVE typed \u00B7 " + h });
      steps.push({ lit: o.output != null, receipt: actuatedText(o.output) });
      steps.push({ lit: !!r.hash, receipt: r.hash ? "receipt #" + str(r.seq) + " \u00B7 " + hashPrefix(r.hash, 12) : "NO_SIGNAL \u00B7 no receipt returned" });
    } else if (o && o.status === "used") {
      steps.push({ lit: false, receipt: "REFUSED \u00B7 already used" });
    } else if (o && o.status === "declined") {
      steps.push({ lit: false, receipt: "declined \u00B7 nothing ran" });
    } else {
      steps.push({ lit: false, waiting: true, receipt: "waiting for APPROVE" });
    }
    return gateView(steps);
  }

  function proposalSig(list) {
    return listOf(list, "proposals").map(function (p) { return str(p.hash); }).join(",");
  }

  function fmtWhen(iso, now) {
    var d = new Date(str(iso));
    if (!iso || isNaN(d.getTime())) return str(iso) || DASH;
    var ref = now instanceof Date ? now : new Date();
    var time = d.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" });
    if (d.toDateString() === ref.toDateString()) return time;
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric" }) + ", " + time;
  }

  // The worst measure against its target (gap in % of target, signed so negative is worse); null if none is comparable.
  function worstVariance(kpis) {
    var worst = null;
    (Array.isArray(kpis) ? kpis : []).forEach(function (k) {
      var v = toNum(k && k.value), t = toNum(k && k.target);
      if (!isFinite(v) || !isFinite(t) || t === 0) return;
      var gap = (v - t) / Math.abs(t) * 100 * (str(k.better).toLowerCase() === "lower" ? -1 : 1);
      if (!worst || gap < worst.gap) worst = { k: k, gap: gap };
    });
    return worst;
  }

  // Shifts that start on the pack's as-of day at one location (the day is read from the timestamp as written).
  function shiftsToday(st, locId) {
    if (!isObj(st) || !Array.isArray(st.schedule)) return [];
    var day = str(st.as_of);
    return st.schedule.filter(function (x) { return x && x.location === locId && str(x.start).slice(0, 10) === day; });
  }

  var pure = {
    worstVariance: worstVariance, shiftsToday: shiftsToday,
    str: str, filterItems: filterItems, humanize: humanize, fmtNumber: fmtNumber, fmtMoney: fmtMoney,
    fmtValue: fmtValue, fmtField: fmtField, kpiCue: kpiCue, tierTag: tierTag, hashPrefix: hashPrefix,
    listOf: listOf, sevTag: sevTag, gateView: gateView, proposalSteps: proposalSteps, proposalSig: proposalSig,
    actuatedText: actuatedText, fmtWhen: fmtWhen
  };
  if (typeof module !== "undefined" && module && module.exports) module.exports = pure;
  if (typeof window === "undefined" || typeof document === "undefined") return;

  /* ------------------------- theme (applied before first paint) ------------------------- */

  var THEME_KEY = "smbos.theme";
  function readTheme() {
    try { var t = window.localStorage.getItem(THEME_KEY); return t === "light" || t === "dark" ? t : "system"; }
    catch (e) { return "system"; }
  }
  function applyTheme(t) {
    var r = document.documentElement;
    if (t === "light" || t === "dark") r.setAttribute("data-theme", t); else r.removeAttribute("data-theme");
  }
  function setTheme(t) {
    try { if (t === "system") window.localStorage.removeItem(THEME_KEY); else window.localStorage.setItem(THEME_KEY, t); } catch (e) { /* per-viewer convenience only */ }
    applyTheme(t);
    renderDockTheme();
    if (wins.settings) paint("settings");
    announce(t === "dark" ? "Night desk" : t === "light" ? "Paper desk" : "Theme follows the system");
  }
  var darkMq = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
  function effectiveTheme() {
    var t = readTheme();
    if (t !== "system") return t;
    return darkMq && darkMq.matches ? "dark" : "light";
  }
  applyTheme(readTheme());

  /* ------------------------- state ------------------------- */

  var NOT_READ = "NO_SIGNAL \u00B7 not yet read";
  var POLL_MS = 5000;
  var S = {
    link: { up: null, why: "" },
    health: null, packs: [], st: null, agents: [], proposals: [], propSig: "",
    loaded: false, lastRuns: {}, locked: false, lockDrawn: false, lockedAt: null, lockReceipt: null
  };
  var views = {
    today: {}, payroll: {}, reconcile: {}, receivables: {}, rollup: {},
    approvals: { errors: {}, decided: [], focus: null },
    inbox: { filter: "all" }, receipts: {}, ask: { history: [] }, settings: {},
    apex: { runs: {}, receipts: null }, capabilities: {}
  };
  var inflight = new Set();
  var refs = {};
  var wins = {};
  var topZ = 10;
  var narrowMq = window.matchMedia ? window.matchMedia("(max-width: 699px)") : null;
  function isNarrow() { return !!(narrowMq && narrowMq.matches); }

  /* ------------------------- DOM helpers (createElement + textContent only) ------------------------- */

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = String(text);
    return n;
  }
  function add(parent) {
    for (var i = 1; i < arguments.length; i++) if (arguments[i]) parent.appendChild(arguments[i]);
    return parent;
  }
  function clear(n) { while (n && n.firstChild) n.removeChild(n.firstChild); return n; }
  function btn(cls, text, onClick, label, focusKey) {
    var b = el("button", cls, text);
    b.type = "button";
    if (label) b.setAttribute("aria-label", label);
    if (focusKey) b.dataset.focusKey = focusKey;
    if (onClick) b.addEventListener("click", onClick);
    return b;
  }
  function tag(cls, text) { return el("span", "wx-tag " + cls, text); }
  function tierEl(t) { var x = tierTag(t); return tag(x.cls, x.text); }
  function lamp(kind) { var l = el("span", "wx-lamp " + kind); l.setAttribute("aria-hidden", "true"); return l; }
  function hud(tagName, cls, key, text) { var n = el(tagName, cls, text); n.setAttribute("data-hud", key); return n; }
  function empty(text) { return el("p", "empty", text || NOT_READ); }
  function eyebrow(text, level) { return el(level || "h3", "wx-eyebrow", text); }
  function section(title, cls) {
    var s = el("section", "sec" + (cls ? " " + cls : ""));
    add(s, eyebrow(title));
    return s;
  }
  var uid = 0;
  function nextId(prefix) { uid += 1; return prefix + "-" + uid; }

  function announce(text) {
    var live = document.getElementById("live");
    if (!live) return;
    live.textContent = "";
    window.setTimeout(function () { live.textContent = str(text); }, 30);
  }

  /* ------------------------- talking to aios ------------------------- */

  // One uniform result: {ok, status, json, text} | {nosignal:true, why} | {aborted:true}.
  function api(method, path, body) {
    var ctrl = new AbortController();
    inflight.add(ctrl);
    var headers = { "Accept": "application/json" };
    var init = { method: method, headers: headers, signal: ctrl.signal, cache: "no-store", credentials: "same-origin" };
    if (method !== "GET") {
      headers["X-AIOS"] = "1";
      headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(body === undefined ? {} : body);
    }
    return window.fetch(path, init).then(function (res) {
      return res.text().then(function (text) {
        var json = null;
        try { json = text ? JSON.parse(text) : null; } catch (e) { json = null; }
        setLink(true, "");
        return { ok: res.ok, status: res.status, json: json, text: text };
      });
    }, function (err) {
      if (err && err.name === "AbortError") return { aborted: true, status: 0 };
      setLink(false, err && err.message ? err.message : "no answer");
      return { nosignal: true, status: 0, why: err && err.message ? err.message : "no answer" };
    }).finally(function () { inflight.delete(ctrl); });
  }

  function errText(r) {
    if (!r) return "no answer";
    var j = r.json;
    var msg = isObj(j) && j.error ? str(j.error) : str(r.text).slice(0, 200);
    var code = isObj(j) && j.code ? " (" + str(j.code) + ")" : "";
    return "HTTP " + r.status + (msg ? " \u00B7 " + msg : "") + code;
  }

  function setLink(up, why) {
    var was = S.link.up;
    S.link.up = up;
    S.link.why = why || "";
    if (was === up) return;
    renderLink();
    if (up && was === false) reloadCore(true);
    else if (!up) { renderDock(); renderIcons(); repaintAll(); }
  }

  function reloadCore(rerunOpen) {
    return Promise.all([
      api("GET", "/api/health"), api("GET", "/api/packs"), api("GET", "/api/state"),
      api("GET", "/api/agents"), api("GET", "/api/proposals")
    ]).then(function (rs) {
      if (rs[0].ok && isObj(rs[0].json)) { S.health = rs[0].json; if (typeof S.health.locked === "boolean") setLocked(S.health.locked); }
      if (rs[1].ok) S.packs = listOf(rs[1].json, "packs");
      if (rs[2].ok && isObj(rs[2].json)) S.st = rs[2].json;
      if (rs[3].ok) S.agents = listOf(rs[3].json, "agents");
      if (rs[4].ok) setProposals(listOf(rs[4].json, "proposals"), true);
      S.loaded = rs[2].ok;
      if (!rs[2].ok && !rs[2].nosignal && !rs[2].aborted) S.stError = errText(rs[2]); else S.stError = "";
      renderAllChrome();
      if (rerunOpen) Object.keys(wins).forEach(function (k) { loadWindow(k, true); });
      repaintAll();
    });
  }

  var polling = false;
  function poll() {
    if (polling || document.hidden) return;
    polling = true;
    Promise.all([api("GET", "/api/health"), api("GET", "/api/proposals")]).then(function (rs) {
      if (rs[0].ok && isObj(rs[0].json)) {
        var before = S.health ? S.health.receipts : null;
        S.health = rs[0].json;
        if (typeof S.health.locked === "boolean" && S.health.locked !== S.locked) setLocked(S.health.locked);
        if (wins.receipts && S.health.receipts !== before && views.receipts.status !== "loading") loadReceipts();
      }
      if (rs[1].ok) setProposals(listOf(rs[1].json, "proposals"));
      renderDock();
      renderApexStatus();
      if (apexOpen()) { renderEng(); renderSov(); }
      renderDeskCard();
    }).finally(function () { polling = false; });
  }

  function setProposals(list, force) {
    var sig = proposalSig(list);
    S.proposals = list;
    if (sig === S.propSig && !force) return;
    S.propSig = sig;
    renderDock();
    renderIcons();
    if (wins.approvals) paint("approvals");
    if (apexOpen()) renderApex();
  }

  function agentById(id) {
    for (var i = 0; i < S.agents.length; i++) if (S.agents[i] && S.agents[i].id === id) return S.agents[i];
    return null;
  }
  function agentName(id) { var a = agentById(id); return a && a.name ? str(a.name) : str(id); }

  // Run one agent. Tier 0/1 come back "done" with output + receipt; tier 2 comes back as a proposal (202).
  function isLockedAnswer(r) { return r && r.status === 423; }

  function runAgent(id, params) {
    return api("POST", "/api/agents/" + encodeURIComponent(id) + "/run", { params: params || {} }).then(function (r) {
      var res;
      var j = r.json;
      if (r.nosignal) res = { status: "nosignal" };
      else if (r.aborted) res = { status: "aborted" };
      else if (isLockedAnswer(r)) { res = { status: "locked" }; setLocked(true); }
      else if (r.status === 202 && isObj(j) && j.status === "needs_approval" && isObj(j.proposal)) {
        addProposal(j.proposal);
        res = { status: "proposal", proposal: j.proposal };
      } else if (r.ok && isObj(j) && j.status === "done") {
        res = { status: "done", output: j.output, receipt: j.receipt, tier: j.tier, run_id: j.run_id, at: new Date() };
      } else res = { status: "error", error: errText(r) };
      if (res.status !== "aborted" && res.status !== "nosignal") S.lastRuns[id] = { status: res.status, output: res.output, at: (res.at || new Date()).toISOString() };
      return res;
    });
  }

  function addProposal(p) {
    if (!p || !p.hash) return;
    var list = S.proposals.filter(function (x) { return x.hash !== p.hash; });
    list.push(p);
    setProposals(list);
  }

  function runInto(kind, slot, agentId, params, after) {
    var v = views[kind];
    v[slot] = { status: "loading" };
    paint(kind);
    return runAgent(agentId, params).then(function (res) {
      v[slot] = res;
      paint(kind);
      if (after) after(res);
      poll();
      return res;
    });
  }

  /* ------------------------- windows ------------------------- */

  var WINDOWS = {
    today: { title: "Today", mono: "TD", group: "daily", w: 860, h: 780, desc: "Morning brief and KPIs per location" },
    approvals: { title: "Approvals", mono: "AP", group: "daily", w: 680, h: 660, desc: "Tier 2 work waiting for you to type APPROVE" },
    inbox: { title: "Inbox", mono: "IN", group: "daily", w: 720, h: 500, desc: "Messages from customers, vendors, the franchisor and staff" },
    ask: { title: "Ask", mono: "AS", group: "daily", w: 640, h: 560, desc: "Ask about your numbers" },
    payroll: { title: "Payroll", mono: "PY", group: "money", w: 820, h: 620, desc: "Hours against the schedule before payroll goes out" },
    reconcile: { title: "Reconciliation", mono: "RC", group: "money", w: 900, h: 640, desc: "Bank lines matched to the ledger" },
    receivables: { title: "Receivables", mono: "AR", group: "money", w: 760, h: 580, desc: "Late invoices and reminder notes" },
    rollup: { title: "Franchise rollup", mono: "FR", group: "money", w: 760, h: 540, desc: "Revenue, royalty and ad fund per location" },
    receipts: { title: "Receipts", mono: "RX", group: "system", w: 920, h: 580, desc: "Every run, proposal and approval, hash-chained" },
    settings: { title: "Settings", mono: "ST", group: "system", w: 640, h: 640, desc: "Business pack, appearance, about" },
    capabilities: { title: "What agents may do", short: "Agents", mono: "AG", group: "system", w: 860, h: 640, desc: "Every agent, its tier, what it reads and writes, and what it never touches" }
  };
  var ORDER = ["today", "approvals", "inbox", "ask", "payroll", "reconcile", "receivables", "rollup", "receipts", "capabilities", "settings"];
  var GROUPS = [["daily", "Daily"], ["money", "Money"], ["system", "System"]];
  var MIN_W = 340, MIN_H = 220, BAR_H = 34;

  function deskSize() {
    var r = refs.desk.getBoundingClientRect();
    return { w: Math.max(320, r.width), h: Math.max(240, r.height) };
  }

  function clampGeom(g, d) {
    g.w = Math.max(Math.min(MIN_W, d.w), Math.min(g.w, d.w));
    g.h = Math.max(Math.min(MIN_H, d.h), Math.min(g.h, d.h));
    g.x = Math.min(Math.max(g.x, 96 - g.w), d.w - 96);
    g.y = Math.min(Math.max(g.y, 0), d.h - BAR_H);
    return g;
  }

  function place(w) {
    var n = w.node;
    var d = deskSize();
    if (w.max) {
      n.style.left = "8px"; n.style.top = "8px";
      n.style.width = (d.w - 16) + "px"; n.style.height = (d.h - 16) + "px";
    } else {
      clampGeom(w, d);
      n.style.left = Math.round(w.x) + "px"; n.style.top = Math.round(w.y) + "px";
      n.style.width = Math.round(w.w) + "px"; n.style.height = Math.round(w.h) + "px";
    }
    n.style.zIndex = String(w.z);
    n.hidden = !!w.min;
    if (w.maxBtn) {
      w.maxBtn.textContent = w.max ? "\u2750" : "\u25A1";
      w.maxBtn.setAttribute("aria-label", (w.max ? "Restore " : "Maximize ") + WINDOWS[w.kind].title);
    }
  }

  function openWindow(kind, opts) {
    opts = opts || {};
    if (!WINDOWS[kind]) return null;
    if (S.locked) { announce("The session is locked. Unlock it to open windows."); if (refs.unlockBtn) refs.unlockBtn.focus(); return null; }
    var w = wins[kind];
    if (!w) {
      w = buildWindow(kind);
      wins[kind] = w;
      refs.wins.appendChild(w.node);
      loadWindow(kind, false);
      paint(kind);
    }
    if (opts.focusHash) views.approvals.focus = opts.focusHash;
    w.min = false;
    focusWindow(kind, opts.focusInside !== false);
    if (kind === "approvals" && opts.focusHash) paint("approvals");
    renderDock();
    renderIcons();
    return w;
  }

  function loadWindow(kind, force) {
    var v = views[kind];
    var done = function (run) { return run && (run.status === "done" || run.status === "loading"); };
    if (kind === "today" && (force || !done(v.brief))) runInto("today", "brief", "morning-brief", {});
    else if (kind === "payroll" && (force || !done(v.pre))) runInto("payroll", "pre", "payroll-precheck", {});
    else if (kind === "reconcile" && (force || !done(v.run))) runInto("reconcile", "run", "reconcile", {});
    else if (kind === "rollup" && (force || !done(v.run))) runInto("rollup", "run", "franchisor-rollup", {});
    else if (kind === "receipts") loadReceipts();
    else if (kind === "capabilities") loadCapabilities();
  }

  function buildWindow(kind) {
    var def = WINDOWS[kind];
    var d = deskSize();
    var w = { kind: kind, x: 0, y: 0, w: def.w, h: def.h, z: ++topZ, max: false, min: false };
    var x0 = Math.min(252, Math.max(16, d.w - def.w - 16));
    // Today, the first window, leaves the business card (top right) visible when the desk is wide enough.
    var room = d.w - x0 - 16 - 380;
    if (kind === "today" && room >= 640 && w.w > room) w.w = room;
    if (w.h > d.h - 32) w.h = d.h - 32;
    var step = Object.keys(wins).length % 6; // cascade from the open windows, so a window on an empty desk starts top left
    w.x = x0 + step * 30;
    w.y = 16 + step * 30;
    clampGeom(w, d);

    var s = el("section", "wx-window win");
    s.dataset.kind = kind;
    var titleId = "win-" + kind + "-title";
    s.setAttribute("aria-labelledby", titleId);
    var bar = el("div", "wx-bar");
    bar.tabIndex = 0;
    bar.setAttribute("role", "group");
    bar.setAttribute("aria-label", def.title + " window. Drag or use the arrow keys to move it; Shift and the arrow keys resize it.");
    var title = el("h2", "wx-title-text", def.title);
    title.id = titleId;
    var meta = el("span", "meta hud");
    var bmin = btn("wx-winbtn", "\u2013", function () { minimizeWindow(kind); }, "Minimize " + def.title);
    var bmax = btn("wx-winbtn", "\u25A1", function () { toggleMax(kind); }, "Maximize " + def.title);
    var bclose = btn("wx-winbtn", "\u00D7", function () { closeWindow(kind); }, "Close " + def.title);
    add(bar, title, meta, bmin, bmax, bclose);
    var body = el("div", "wx-body");
    var grip = el("div", "grip");
    grip.setAttribute("aria-hidden", "true");
    add(s, bar, body, grip);
    w.node = s; w.bar = bar; w.body = body; w.meta = meta; w.maxBtn = bmax;

    s.addEventListener("pointerdown", function () { focusWindow(kind, false); }, true);
    s.addEventListener("focusin", function () { if (refs.focused !== kind) focusWindow(kind, false); });
    bar.addEventListener("dblclick", function (e) { if (!e.target.closest("button")) toggleMax(kind); });
    bar.addEventListener("pointerdown", function (e) { startDrag(e, w, "move"); });
    grip.addEventListener("pointerdown", function (e) { startDrag(e, w, "size"); });
    bar.addEventListener("keydown", function (e) { barKey(e, w); });
    place(w);
    return w;
  }

  function startDrag(e, w, mode) {
    if (e.button !== 0 || (e.target.closest && e.target.closest("button"))) return;
    if (w.max) return;
    e.preventDefault();
    var target = e.currentTarget;
    var sx = e.clientX, sy = e.clientY, ox = w.x, oy = w.y, ow = w.w, oh = w.h;
    try { target.setPointerCapture(e.pointerId); } catch (err) { /* capture is a nicety */ }
    function move(ev) {
      if (mode === "move") { w.x = ox + ev.clientX - sx; w.y = oy + ev.clientY - sy; }
      else { w.w = Math.max(MIN_W, ow + ev.clientX - sx); w.h = Math.max(MIN_H, oh + ev.clientY - sy); }
      place(w);
    }
    function up() {
      target.removeEventListener("pointermove", move);
      target.removeEventListener("pointerup", up);
      target.removeEventListener("pointercancel", up);
    }
    target.addEventListener("pointermove", move);
    target.addEventListener("pointerup", up);
    target.addEventListener("pointercancel", up);
  }

  function barKey(e, w) {
    if (e.target !== w.bar) return;
    var step = 16;
    var dx = { ArrowLeft: -step, ArrowRight: step }[e.key] || 0;
    var dy = { ArrowUp: -step, ArrowDown: step }[e.key] || 0;
    if (!dx && !dy) return;
    e.preventDefault();
    if (w.max) return;
    if (e.shiftKey) { w.w = Math.max(MIN_W, w.w + dx); w.h = Math.max(MIN_H, w.h + dy); }
    else { w.x += dx; w.y += dy; }
    place(w);
  }

  function focusWindow(kind, moveFocus) {
    var w = wins[kind];
    if (!w) return;
    if (w.z !== topZ) w.z = ++topZ;
    refs.focused = kind;
    Object.keys(wins).forEach(function (k) { wins[k].node.classList.toggle("is-focused", k === kind); });
    place(w);
    if (moveFocus && !w.node.contains(document.activeElement)) w.bar.focus();
    renderDockTasks();
  }

  function topWindow(except) {
    var best = null;
    Object.keys(wins).forEach(function (k) {
      if (k === except || wins[k].min) return;
      if (!best || wins[k].z > best.z) best = wins[k];
    });
    return best;
  }

  function minimizeWindow(kind) {
    var w = wins[kind];
    if (!w) return;
    w.min = true;
    place(w);
    var next = topWindow(kind);
    if (next) focusWindow(next.kind, true);
    else { refs.focused = null; focusIcon(kind); }
    renderDockTasks();
  }

  function toggleMax(kind) {
    var w = wins[kind];
    if (!w) return;
    w.max = !w.max;
    place(w);
    w.bar.focus();
  }

  function closeWindow(kind, quiet) {
    var w = wins[kind];
    if (!w) return;
    w.node.remove();
    delete wins[kind];
    if (refs.focused === kind) refs.focused = null;
    if (!quiet) {
      var next = topWindow(kind);
      if (next) focusWindow(next.kind, true); else focusIcon(kind);
    }
    renderDock();
    renderIcons();
  }

  function focusIcon(kind) {
    var b = refs.icons && refs.icons.querySelector('.icon[data-kind="' + kind + '"]');
    if (b) b.focus();
  }

  // Rebuild one window's body, keeping typed text, focus and scroll.
  function paint(kind) {
    var w = wins[kind];
    if (!w) return;
    var body = w.body;
    var keep = {};
    Array.prototype.forEach.call(body.querySelectorAll("[data-keep]"), function (n) {
      if (n.type === "radio" || n.type === "checkbox") keep[n.dataset.keep] = n.checked;
      else keep[n.dataset.keep] = n.value;
    });
    var ae = document.activeElement;
    var fk = null, sel = null;
    if (ae && body.contains(ae)) {
      fk = ae.dataset ? (ae.dataset.keep || ae.dataset.focusKey || null) : null;
      if (fk && typeof ae.selectionStart === "number") { try { sel = [ae.selectionStart, ae.selectionEnd]; } catch (e) { sel = null; } }
    }
    var top = body.scrollTop;
    clear(body);
    try { RENDER[kind](body, w); }
    catch (e) {
      // A shape this page does not understand: say so, never draw a guess.
      clear(body);
      var m = el("p", "err wx-row");
      m.setAttribute("role", "alert");
      add(m, tag("miss", "MISS"), el("span", null, "This window could not draw what aios sent (" + str(e && e.message) + ")."));
      body.appendChild(m);
      if (window.console) window.console.warn("render " + kind, e);
    }
    Array.prototype.forEach.call(body.querySelectorAll("[data-keep]"), function (n) {
      var k = n.dataset.keep;
      if (!(k in keep)) return;
      if (n.type === "radio" || n.type === "checkbox") n.checked = keep[k];
      else n.value = keep[k];
    });
    body.scrollTop = top;
    if (fk) {
      var again = body.querySelector('[data-keep="' + cssEscape(fk) + '"]') || body.querySelector('[data-focus-key="' + cssEscape(fk) + '"]');
      if (again) {
        again.focus();
        if (sel && typeof again.setSelectionRange === "function") { try { again.setSelectionRange(sel[0], sel[1]); } catch (e) { /* not a text field */ } }
      }
    }
  }

  function repaintAll() {
    Object.keys(wins).forEach(paint);
    renderDeskCard();
    if (refs.apex && !refs.apexHost.hidden) renderApex();
  }

  function cssEscape(s) {
    return (window.CSS && window.CSS.escape) ? window.CSS.escape(String(s)) : String(s).replace(/["\\]/g, "\\$&");
  }

  // Rebuild a container without dropping keyboard focus (elements carry data-focus-key).
  function rebuild(container, build) {
    var ae = document.activeElement;
    var fk = ae && container.contains(ae) && ae.dataset ? ae.dataset.focusKey : null;
    clear(container);
    build(container);
    if (fk) { var again = container.querySelector('[data-focus-key="' + cssEscape(fk) + '"]'); if (again) again.focus(); }
  }

  /* ------------------------- shared window parts ------------------------- */

  function noSignalBlock(retry) {
    var d = el("div", "nosig wx-card is-pending");
    d.setAttribute("role", "status");
    add(d, tag("nosignal is-strong", "NO_SIGNAL"),
      el("p", "wx-lead", "NO SIGNAL from aios"),
      el("p", "wx-body", "The local runtime is not answering, so nothing is shown here: no numbers until it answers again." +
        (S.link.why ? " (" + S.link.why + ")" : "")),
      el("p", "wx-caption", "Start aios again (double-click it), then try again. Your data is still on this computer."));
    if (retry) add(d, add(el("div", "wx-row"), btn("wx-btn", "Try again", retry, null, "retry")));
    return d;
  }

  // True when the window must show NO SIGNAL (or is still waiting for the first read) instead of data.
  function blocked(body) {
    if (S.link.up === false) { add(body, noSignalBlock(function () { reloadCore(true); })); return true; }
    if (!S.st) {
      if (S.stError) add(body, el("p", "err", "MISS \u00B7 aios answered without the business state: " + S.stError));
      else add(body, empty("Reading from aios\u2026"));
      return true;
    }
    return false;
  }

  function receiptLine(r) {
    if (!isObj(r)) return el("p", "wx-caption", "NO_SIGNAL \u00B7 no receipt returned");
    var p = el("p", "receipt-line");
    var bits = [str(r.kind || "run"), r.tier != null ? "tier " + str(r.tier) : "", fmtWhen(r.ts), "hash " + hashPrefix(r.hash, 12)].filter(Boolean);
    add(p, tag("verified is-strong", "RECEIPT #" + str(r.seq)), hud("span", "wx-caption", "receipt", bits.join(" \u00B7 ")));
    if (r.hash) p.title = "hash " + str(r.hash) + (r.prev ? "\nprev " + str(r.prev) : "");
    return p;
  }

  function runStatusEl(run, idleText) {
    if (!run) return empty(idleText || "Not run yet.");
    if (run.status === "loading") {
      var p = el("p", "running wx-row");
      p.setAttribute("role", "status");
      add(p, tag("reported", "RUNNING"), el("span", "wx-caption", "asking aios\u2026"));
      return p;
    }
    if (run.status === "nosignal") return noSignalBlock(null);
    if (run.status === "locked") {
      var l = el("p", "wx-row");
      add(l, tag("refused is-strong", "REFUSED \u00B7 locked"), el("span", "wx-caption", "aios refused: the session is locked. Unlock it to run agents."));
      return l;
    }
    if (run.status === "aborted") {
      var a = el("p", "wx-row");
      add(a, tag("dead", "STOPPED"), el("span", "wx-caption", "Stopped by Stop everything. Nothing is running."));
      return a;
    }
    if (run.status === "error") {
      var e = el("p", "err wx-row");
      e.setAttribute("role", "alert");
      add(e, tag("miss", "MISS"), el("span", null, run.error));
      return e;
    }
    return null;
  }

  // Draw an agent run: status, then the output (via renderOk) and its receipt.
  function runBlock(run, renderOk, idleText, onUndo) {
    var wrap = el("div", "run");
    var st = runStatusEl(run, idleText);
    if (st) return add(wrap, st);
    if (run.status === "proposal") return add(wrap, proposalNote(run.proposal));
    add(wrap, renderOk(run.output), receiptLine(run.receipt));
    if (onUndo && Number(run.tier) === 1 && run.run_id) {
      var u = run.undo;
      if (u && u.status === "done") add(wrap, add(el("p", "wx-row"), tag("dead is-strong", "UNDONE"), el("span", "wx-caption", u.already ? "this change was already undone" : "reversed on this computer")), u.already ? null : receiptLine(u.receipt));
      else {
        var row = add(el("div", "wx-row"), btn("wx-btn is-ghost", u && u.status === "loading" ? "Undoing\u2026" : "Undo", onUndo, "Undo this tier 1 change", "undo"),
          el("span", "wx-caption", "Tier 1 changes can be undone; undoing writes its own receipt."));
        if (u && u.error) add(row, tag(u.status === "locked" ? "refused" : "miss", u.status === "locked" ? "REFUSED \u00B7 locked" : "MISS"), el("span", "wx-caption", u.error));
        wrap.appendChild(row);
      }
    }
    return wrap;
  }

  // Reverse one tier-1 run (POST /api/runs/{run_id}/undo), then re-read the state it changed.
  // POST /api/runs/{run_id}/undo \u2192 an undo state for the run: done (with its receipt), locked, or an error in aios's words.
  function postUndo(runId) {
    return api("POST", "/api/runs/" + encodeURIComponent(runId) + "/undo", {}).then(function (r) {
      var code = isObj(r.json) ? str(r.json.code) : "";
      if (r.ok) return { status: "done", receipt: isObj(r.json) ? r.json.receipt : null };
      if (r.nosignal) return { status: "error", error: "NO SIGNAL from aios. Nothing was undone." };
      if (isLockedAnswer(r)) { setLocked(true); return { status: "locked", error: "the session is locked" }; }
      if (r.status === 409 && code === "already_undone") return { status: "done", receipt: null, already: true };
      return { status: "error", error: errText(r) };
    });
  }

  function undoRun(kind, slot) {
    var run = views[kind][slot];
    if (!run || !run.run_id) return;
    run.undo = { status: "loading" };
    paint(kind);
    postUndo(run.run_id).then(function (u) {
      run.undo = u;
      if (u.status === "done") announce("Undone." + (isObj(u.receipt) ? " Receipt " + str(u.receipt.seq) + " written." : ""));
      return api("GET", "/api/state").then(function (sr) { if (sr.ok && isObj(sr.json)) S.st = sr.json; paint(kind); poll(); });
    });
  }

  function proposalNote(p) {
    var n = el("div", "propnote wx-card is-active");
    add(n, add(el("div", "wx-row"), tierEl(2), tag("hold", "WAITING FOR YOU")),
      el("p", "wx-label", str(p.summary || "A tier 2 proposal")),
      el("p", "wx-caption", "Nothing has run. Hash " + hashPrefix(p.hash, 16) + " \u00B7 type APPROVE in Approvals to run it once."),
      add(el("div", "wx-row"), btn("wx-btn is-ink", "Open Approvals", function () { openWindow("approvals", { focusHash: p.hash }); }, null, "open-approvals")));
    return n;
  }

  /* generic rendering for outputs (any JSON, never invented) */

  function cellText(key, v) {
    if (v == null) return DASH;
    if (typeof v === "number" || typeof v === "boolean") return fmtField(key, v);
    if (typeof v === "string") return /^\d{4}-\d{2}-\d{2}T/.test(v) ? fmtWhen(v) : v;
    if (Array.isArray(v)) {
      if (!v.length) return DASH;
      if (v.every(function (x) { return x == null || typeof x !== "object"; })) return v.map(str).join("; ");
      return v.map(function (x) { return isObj(x) ? Object.keys(x).map(function (k) { return humanize(k) + " " + cellText(k, x[k]); }).join(", ") : str(x); }).join("; ");
    }
    if (isObj(v)) return Object.keys(v).map(function (k) { return humanize(k) + " " + cellText(k, v[k]); }).join(" \u00B7 ");
    return str(v);
  }

  function table(rows, opts) {
    opts = opts || {};
    var cols = opts.cols || [];
    if (!opts.cols) rows.forEach(function (r) { Object.keys(r).forEach(function (k) { if (cols.indexOf(k) === -1) cols.push(k); }); });
    var wrap = el("div", "tablewrap");
    var t = el("table", "wx-ledger" + (opts.compact ? " compact" : ""));
    if (opts.caption) add(t, el("caption", "sr-only", opts.caption));
    var thead = el("thead"), hr = el("tr");
    cols.forEach(function (c) {
      var th = el("th", null, (opts.labels && opts.labels[c]) || humanize(c));
      th.scope = "col";
      if (rows.some(function (r) { return typeof r[c] === "number"; })) th.className = "n";
      hr.appendChild(th);
    });
    add(t, add(thead, hr));
    var tb = el("tbody");
    rows.forEach(function (r) {
      var tr = el("tr");
      if (opts.rowClass) { var rc = opts.rowClass(r); if (rc) tr.className = rc; }
      cols.forEach(function (c) {
        var custom = opts.cell && opts.cell(c, r[c], r);
        var td = el("td", typeof r[c] === "number" ? "n" : (/hash|ref|id$|reference/.test(c) ? "m" : null));
        if (custom) td.appendChild(custom); else td.textContent = cellText(c, r[c]);
        tr.appendChild(td);
      });
      tb.appendChild(tr);
    });
    add(t, tb);
    return add(wrap, t);
  }

  function renderAny(v, key) {
    if (v == null) return el("p", "wx-caption", DASH);
    if (Array.isArray(v)) {
      if (!v.length) return empty("none");
      if (v.every(isObj)) return table(v, { caption: humanize(key) });
      if (v.every(function (x) { return x == null || typeof x !== "object"; })) {
        var ul = el("ul", "plain");
        v.forEach(function (x) { ul.appendChild(el("li", null, str(x))); });
        return ul;
      }
      var st = el("div", "wx-stack");
      v.forEach(function (x) { st.appendChild(renderAny(x, key)); });
      return st;
    }
    if (isObj(v)) {
      var dl = el("dl", "kv");
      Object.keys(v).forEach(function (k) {
        var val = v[k];
        add(dl, el("dt", null, humanize(k)));
        var dd = el("dd");
        if (val !== null && typeof val === "object") dd.appendChild(renderAny(val, k));
        else dd.textContent = cellText(k, val);
        dl.appendChild(dd);
      });
      return dl;
    }
    return el("p", "wx-body", cellText(key, v));
  }

  // Lead text first (headline / summary), then every other field in a stated order, then the rest.
  function renderOutput(o, opts) {
    opts = opts || {};
    var box = el("div", "out wx-stack");
    if (!isObj(o)) return add(box, renderAny(o, "output"));
    var used = {};
    (opts.lead || ["headline", "summary", "message"]).forEach(function (k) {
      if (typeof o[k] === "string" && o[k]) { add(box, el("p", "wx-lead out-lead", o[k])); used[k] = true; }
    });
    var keys = (opts.order || []).filter(function (k) { return k in o; });
    Object.keys(o).forEach(function (k) { if (keys.indexOf(k) === -1) keys.push(k); });
    keys.forEach(function (k) {
      if (used[k] || (opts.skip && opts.skip.indexOf(k) !== -1)) return;
      var val = o[k];
      var custom = opts.custom && opts.custom[k];
      var s = el("section", "sec");
      var count = Array.isArray(val) ? " \u00B7 " + val.length : "";
      add(s, eyebrow(((opts.labels && opts.labels[k]) || humanize(k)) + count));
      if (custom) add(s, custom(val, o));
      else if (val !== null && typeof val === "object") add(s, renderAny(val, k));
      else add(s, el("p", "wx-body", cellText(k, val)));
      box.appendChild(s);
    });
    return box;
  }

  // A labelled select for one enum param of an agent (from GET /api/agents); the value lives in views[kind].params.
  function paramSelect(kind, agentId, name, label) {
    var a = agentById(agentId);
    var spec = a && Array.isArray(a.params) ? a.params.filter(function (p) { return isObj(p) && p.name === name; })[0] : null;
    if (!spec || !Array.isArray(spec.enum) || !spec.enum.length) return null;
    var v = views[kind];
    v.params = v.params || {};
    if (v.params[name] == null) v.params[name] = spec.default != null ? str(spec.default) : str(spec.enum[0]);
    var id = nextId("param");
    var wrap = el("span", "param");
    var lab = el("label", "wx-caption", label || humanize(name));
    lab.htmlFor = id;
    var sel = el("select", "wx-input sel");
    sel.id = id;
    spec.enum.forEach(function (o) {
      var opt = el("option", null, o === "all" ? "All locations" : /^L\d+$/.test(o) ? locName(o) + " " + o : o);
      opt.value = o;
      sel.appendChild(opt);
    });
    sel.value = v.params[name];
    sel.dataset.focusKey = "param:" + name;
    sel.addEventListener("change", function () { v.params[name] = sel.value; });
    if (spec.description) sel.title = str(spec.description);
    return add(wrap, lab, sel);
  }
  function paramsOf(kind) { var p = views[kind].params; return p ? Object.assign({}, p) : {}; }

  function headRow() {
    var r = el("div", "wx-row head-row");
    for (var i = 0; i < arguments.length; i++) if (arguments[i]) r.appendChild(arguments[i]);
    return r;
  }
  function spacer() { return el("span", "grow"); }

  /* ------------------------- Today ------------------------- */

  function renderToday(body, w) {
    w.meta.textContent = "";
    if (blocked(body)) return;
    if (S.st.as_of) w.meta.textContent = "as of " + str(S.st.as_of);
    var st = S.st;
    var v = views.today;
    add(body, headRow(tierEl(0), el("span", "wx-caption", str(st.brand) + " \u00B7 " + str(st.label)), spacer(),
      btn("wx-btn is-ghost", "Run the brief again", function () { runInto("today", "brief", "morning-brief", {}); }, null, "rerun")));

    var brief = section("Morning brief", "brief");
    add(brief, runBlock(v.brief, renderBrief, "The brief runs when Today opens."));
    body.appendChild(brief);

    var locs = Array.isArray(st.locations) ? st.locations : [];
    var kpis = Array.isArray(st.kpis) ? st.kpis : [];
    if (!locs.length) add(body, empty("NO_SIGNAL \u00B7 aios sent no locations"));
    locs.forEach(function (loc) {
      var s = el("section", "sec loc");
      var h = el("h3", "loc-head");
      add(h, el("span", "wx-label", str(loc.name)), el("span", "wx-caption", " " + str(loc.id) + (loc.manager ? " \u00B7 manager " + str(loc.manager) : "")));
      s.appendChild(h);
      var mine = kpis.filter(function (k) { return k && k.location === loc.id; });
      if (!mine.length) s.appendChild(empty("No KPIs reported for this location."));
      else {
        var grid = el("div", "kpis");
        mine.forEach(function (k) { grid.appendChild(kpiTile(k)); });
        s.appendChild(grid);
      }
      var shifts = shiftsToday(st, loc.id);
      var sh = el("p", "shifts wx-caption");
      sh.textContent = shifts.length ? "On shift today: " + shifts.map(function (x) {
        return str(x.who) + " (" + str(x.role) + ", " + str(x.start).slice(11, 16) + "\u2013" + str(x.end).slice(11, 16) + ")";
      }).join("; ") : "No shifts scheduled today.";
      s.appendChild(sh);
      body.appendChild(s);
    });

    var alerts = Array.isArray(st.alerts) ? st.alerts : [];
    var as = section("Alerts" + (alerts.length ? " \u00B7 " + alerts.length : ""));
    if (!alerts.length) as.appendChild(empty("No alerts."));
    else {
      var ul = el("ul", "alerts");
      alerts.forEach(function (a) {
        var t = sevTag(a.severity);
        var li = el("li");
        add(li, tag(t.cls, t.text), el("span", "wx-caption", locName(a.location)), el("span", null, str(a.text)));
        ul.appendChild(li);
      });
      as.appendChild(ul);
    }
    body.appendChild(as);
  }

  // The morning brief: headline, one line per location, off-target measures worst first, alert and AR counts.
  function renderBrief(o) {
    if (!isObj(o)) return renderAny(o, "output");
    var box = el("div", "out wx-stack");
    if (o.headline) add(box, el("p", "wx-lead out-lead", str(o.headline)));
    if (Array.isArray(o.locations) && o.locations.length) {
      var ul = el("ul", "lines");
      o.locations.forEach(function (l) {
        var off = toNum(l.off_target), on = toNum(l.on_target);
        var li = el("li");
        if (isFinite(off) && isFinite(on)) {
          add(li, tag(off > 0 ? "miss" : "verified", off > 0 ? off + " OFF TARGET" : "ALL ON TARGET"),
            el("span", "wx-label", str(l.name)), el("span", "wx-caption", (l.manager ? str(l.manager) + " \u00B7 " : "") + off + " of " + (off + on) + " measures off target"));
          li.title = str(l.line);
        } else add(li, el("span", null, str(l.line || l.name)));
        ul.appendChild(li);
      });
      box.appendChild(ul);
    }
    if (Array.isArray(o.variances)) {
      var offs = o.variances.filter(function (x) { return x && x.on_target === false; });
      var s = el("section", "sec");
      add(s, eyebrow("Off target, worst first \u00B7 " + offs.length + " of " + o.variances.length));
      if (!offs.length) s.appendChild(empty("Every measure is on target."));
      else {
        var vl = el("ul", "lines");
        offs.forEach(function (x) {
          var li = el("li");
          add(li, tag("miss", "WORSE"), el("span", "wx-label", str(x.location_name || x.location)), el("span", null, str(x.text || x.label)));
          vl.appendChild(li);
        });
        s.appendChild(vl);
      }
      box.appendChild(s);
    }
    var tail = el("div", "wx-row brief-tail");
    if (isObj(o.alerts)) {
      [["crit", "miss"], ["warn", "hold"], ["info", "reported"]].forEach(function (p) {
        if (o.alerts[p[0]] != null) add(tail, tag(p[1], str(o.alerts[p[0]]) + " " + p[0]));
      });
    }
    if (isObj(o.ar)) {
      add(tail, el("span", "wx-caption", str(o.ar.open) + " open invoices \u00B7 " + fmtMoney(toNum(o.ar.open_total)) + " \u00B7 " +
        str(o.ar.over_14_days) + " more than 14 days late \u00B7 " + fmtMoney(toNum(o.ar.over_14_days_total))),
        btn("wx-btn is-ghost", "Open Receivables", function () { openWindow("receivables"); }, null, "open-ar"));
    }
    if (tail.childNodes.length) box.appendChild(tail);
    return box;
  }

  function locName(id) {
    var locs = S.st && Array.isArray(S.st.locations) ? S.st.locations : [];
    for (var i = 0; i < locs.length; i++) if (locs[i].id === id) return str(locs[i].name);
    return str(id);
  }

  function kpiTile(k) {
    var cue = kpiCue(k);
    var t = el("div", "kpi wx-card kpi-" + cue.dir);
    var key = "kpi:" + str(k.location) + ":" + str(k.key);
    add(t, el("div", "kpi-label wx-label", str(k.label || humanize(k.key))),
      hud("div", "kpi-value", key, fmtValue(k.value, k.unit)),
      el("div", "wx-caption", "target " + fmtValue(k.target, k.unit)),
      k.period ? el("div", "wx-caption", str(k.period)) : null,
      add(el("div", "kpi-cue"), tag(cue.cls + " is-strong", cue.word), el("span", "wx-caption", cue.text)));
    return t;
  }

  /* ------------------------- Approvals ------------------------- */

  function renderApprovals(body, w) {
    var n = S.proposals.length;
    w.meta.textContent = S.link.up === false ? "NO_SIGNAL" : n + " waiting";
    w.bar.classList.toggle("signal", n > 0 && S.link.up !== false);
    if (S.link.up === false) { add(body, noSignalBlock(function () { reloadCore(true); })); return; }
    var v = views.approvals;
    add(body, el("p", "wx-body intro", "Tier 2 work (money, anything outward, anything irreversible) waits here. Nothing runs until you type APPROVE. Each approval runs once; a changed instruction is a new proposal with a new hash."));
    if (!n) add(body, empty("Nothing is waiting for approval."));
    S.proposals.forEach(function (p) { body.appendChild(proposalCard(p)); });
    if (v.decided.length) {
      add(body, eyebrow("Decided this session"));
      v.decided.forEach(function (d) { body.appendChild(decidedCard(d)); });
    }
    if (v.focus) {
      var target = body.querySelector('.prop[data-hash="' + cssEscape(v.focus) + '"] input');
      v.focus = null;
      if (target) window.setTimeout(function () { target.scrollIntoView({ block: "nearest" }); target.focus(); }, 0);
    }
  }

  function gateEl(steps) {
    var g = el("div", "wx-gate");
    g.setAttribute("role", "list");
    g.setAttribute("aria-label", "Gate: declared, approved, actuated, observed");
    steps.forEach(function (s) {
      var step = el("div", "wx-gate-step" + (s.cls ? " " + s.cls : ""));
      step.setAttribute("role", "listitem");
      step.setAttribute("data-hud", "gate-" + s.name.toLowerCase());
      add(step, add(el("span", "wx-gate-name"), lamp(s.lamp), el("span", null, s.name + (s.lit ? "" : s.waiting ? " \u00B7 waiting" : ""))),
        el("span", "wx-gate-receipt", s.receipt));
      g.appendChild(step);
    });
    return g;
  }

  function instructionEl(ins) {
    var pre = el("pre", "wx-slab instruction");
    pre.textContent = ins === undefined ? "NO_SIGNAL \u00B7 no instruction returned" : JSON.stringify(ins, null, 2);
    pre.tabIndex = 0;
    pre.setAttribute("aria-label", "The instruction that will run");
    return pre;
  }

  function proposalCard(p) {
    var v = views.approvals;
    var hash = str(p.hash);
    var card = el("article", "wx-approval prop");
    card.dataset.hash = hash;
    var askId = nextId("ask");
    card.setAttribute("aria-labelledby", askId);
    var ask = el("h3", "wx-ask", str(p.summary || "A tier 2 proposal"));
    ask.id = askId;
    add(card, el("p", "wx-from", "From the " + agentName(p.agent) + " agent \u00B7 " + fmtWhen(p.created)), ask,
      add(el("div", "wx-row"), tierEl(2), tag("hold", "WAITING FOR YOU")),
      gateEl(proposalSteps(p, null)),
      eyebrow("What will run", "h4"), instructionEl(p.instruction),
      hud("p", "wx-caption hashline", "proposal-hash", "sha256 " + hashPrefix(hash, 16)));
    card.lastChild.title = "sha256 " + hash;

    var fid = nextId("approve");
    var eid = fid + "-err";
    var field = el("div", "wx-field");
    var lab = el("label", null, "Type APPROVE to run this once");
    lab.htmlFor = fid;
    var input = el("input", "wx-input");
    input.id = fid;
    input.type = "text";
    input.autocomplete = "off";
    input.spellcheck = false;
    input.setAttribute("autocapitalize", "characters");
    input.dataset.keep = "approve:" + hash;
    input.setAttribute("aria-describedby", eid);
    input.addEventListener("keydown", function (e) { if (e.key === "Enter") { e.preventDefault(); approve(p, input.value); } });
    var err = el("p", "err", v.errors[hash] || "");
    err.id = eid;
    if (v.errors[hash]) { err.setAttribute("role", "alert"); input.setAttribute("aria-invalid", "true"); }
    add(field, lab, input, err);
    var actions = el("div", "wx-row");
    add(actions,
      btn("wx-btn is-signal", "Approve and run once", function () { approve(p, input.value); }, null, "approve-btn:" + hash),
      btn("wx-btn is-ghost", "Decline", function () { decline(p); }, null, "decline-btn:" + hash));
    add(card, field, actions, el("p", "wx-default", "If you do nothing, nothing runs."));
    return card;
  }

  function decidedCard(d) {
    var p = d.proposal || {};
    var card = el("article", "wx-card decided");
    var hid = nextId("dec");
    card.setAttribute("aria-labelledby", hid);
    var word = d.status === "done" ? { cls: "verified", text: "APPROVED \u00B7 RAN ONCE" }
      : d.status === "used" ? { cls: "refused", text: "REFUSED \u00B7 ALREADY USED" }
      : { cls: "dead", text: "DECLINED" };
    var h = el("h3", "wx-label", str(p.summary || "Proposal"));
    h.id = hid;
    h.tabIndex = -1;
    h.dataset.focusKey = "decided:" + str(p.hash);
    add(card, add(el("div", "wx-row"), tierEl(2), tag(word.cls + " is-strong", word.text)), h, gateEl(proposalSteps(p, d)));
    if (d.status === "used") add(card, el("p", "wx-body", "Already used: this exact instruction has run once and will not run again. A changed instruction is a new proposal."));
    if (d.status === "declined") add(card, el("p", "wx-body", "Declined. Nothing ran."));
    if (d.status === "done") {
      add(card, receiptLine(d.receipt));
      if (d.output != null) add(card, eyebrow("What it did", "h4"), renderExec(d.output));
    }
    add(card, el("p", "wx-caption", "hash " + hashPrefix(p.hash, 16)));
    return card;
  }

  function approve(p, phrase) {
    var v = views.approvals;
    var hash = str(p.hash);
    delete v.errors[hash];
    return api("POST", "/api/proposals/" + encodeURIComponent(hash) + "/approve", { phrase: str(phrase) }).then(function (r) {
      if (r.aborted) return;
      if (r.nosignal) { v.errors[hash] = "NO SIGNAL from aios. Nothing was approved."; paint("approvals"); return; }
      if (r.ok) {
        var j = isObj(r.json) ? r.json : {};
        decide(p, { status: "done", output: j.output, receipt: j.receipt });
        announce("Approved. It ran once" + (isObj(j.receipt) ? "; receipt " + str(j.receipt.seq) + " written." : "."));
      } else if (isLockedAnswer(r)) {
        v.errors[hash] = "REFUSED \u00B7 locked. Unlock the session, then approve.";
        setLocked(true);
        paint("approvals");
      } else if (r.status === 409 && ["already_used", ""].indexOf(isObj(r.json) ? str(r.json.code) : "") !== -1) {
        decide(p, { status: "used" });
        announce("Already used. That approval has run once already.");
      } else if (r.status === 409) {
        // chain_broken, hash_mismatch: not "already used"; say what aios said and run nothing
        v.errors[hash] = "REFUSED \u00B7 " + errText(r);
        paint("approvals");
      } else if (r.status === 422) {
        v.errors[hash] = "REFUSED \u00B7 wrong phrase. Type APPROVE in capital letters, exactly.";
        paint("approvals");
      } else if (r.status === 404) {
        v.errors[hash] = "MISS \u00B7 aios does not know this proposal any more.";
        paint("approvals");
        poll();
      } else {
        v.errors[hash] = "MISS \u00B7 " + errText(r);
        paint("approvals");
      }
    });
  }

  function decline(p) {
    var v = views.approvals;
    var hash = str(p.hash);
    return api("POST", "/api/proposals/" + encodeURIComponent(hash) + "/decline", {}).then(function (r) {
      if (r.aborted) return;
      if (r.nosignal) { v.errors[hash] = "NO SIGNAL from aios. Nothing was declined."; paint("approvals"); return; }
      if (r.ok) { decide(p, { status: "declined" }); announce("Declined. Nothing ran."); }
      else if (r.status === 409 && ["already_used", ""].indexOf(isObj(r.json) ? str(r.json.code) : "") !== -1) decide(p, { status: "used" });
      else if (r.status === 409) { v.errors[hash] = "REFUSED \u00B7 " + errText(r); paint("approvals"); }
      else { v.errors[hash] = "MISS \u00B7 " + errText(r); paint("approvals"); }
    });
  }

  function decide(p, outcome) {
    var v = views.approvals;
    outcome.proposal = p;
    v.decided.unshift(outcome);
    delete v.errors[str(p.hash)];
    var hadFocus = wins.approvals && wins.approvals.node.contains(document.activeElement);
    setProposals(S.proposals.filter(function (x) { return x.hash !== p.hash; }));
    paint("approvals");
    if (hadFocus && wins.approvals) {
      var h = wins.approvals.body.querySelector('[data-focus-key="decided:' + cssEscape(str(p.hash)) + '"]');
      if (h) h.focus();
    }
    // A finished tier-2 run is visible in the windows that proposed it.
    ["payroll", "receivables"].forEach(function (k) { if (wins[k]) paint(k); });
    poll();
  }

  /* ------------------------- Payroll ------------------------- */

  function renderPayroll(body) {
    if (blocked(body)) return;
    var v = views.payroll;
    add(body, headRow(tierEl(0), el("span", "wx-caption", "Hours against the schedule, per person, before payroll goes out."), spacer(),
      paramSelect("payroll", "payroll-precheck", "location", "Location"),
      btn("wx-btn is-ghost", "Check again", function () { runInto("payroll", "pre", "payroll-precheck", paramsOf("payroll")); }, null, "rerun")));
    var pre = section("Precheck");
    add(pre, runBlock(v.pre, function (o) { return OUTPUT["payroll-precheck"](o); }, "The precheck runs when Payroll opens."));
    body.appendChild(pre);

    var sub = section("Submit payroll");
    add(sub, el("p", "wx-body", "Submitting writes the payroll export. That is tier 2: I write a proposal, and nothing runs until you type APPROVE in Approvals."));
    add(sub, add(el("div", "wx-row"),
      btn("wx-btn is-signal", "Submit payroll", function () {
        runInto("payroll", "submit", "payroll-submit", paramsOf("payroll"), function (res) {
          if (res.status === "proposal") openWindow("approvals", { focusHash: res.proposal.hash });
        });
      }, null, "submit"),
      tierEl(2)));
    if (v.submit) add(sub, runBlock(v.submit, function (o) { return renderAny(o, "output"); }));
    body.appendChild(sub);
  }

  /* ------------------------- Reconciliation ------------------------- */

  var RECON_ORDER = ["matched", "unmatched_bank", "unmatched_ledger", "exceptions", "suggested", "suggested_entries"];
  var RECON_LABELS = { matched: "Matched", unmatched_bank: "Unmatched bank lines", unmatched_ledger: "Unmatched ledger entries",
    exceptions: "Exceptions", suggested: "Suggested entries", suggested_entries: "Suggested entries" };

  function reconCounts(o) {
    var row = el("div", "wx-row counts");
    RECON_ORDER.forEach(function (k) {
      if (!isObj(o) || !Array.isArray(o[k])) return;
      var n = o[k].length;
      var cls = k === "matched" ? "verified" : n ? "hold" : "pass";
      add(row, tag(cls, n + " " + RECON_LABELS[k].toLowerCase()));
    });
    return row;
  }

  function renderReconcile(body) {
    if (blocked(body)) return;
    var v = views.reconcile;
    add(body, headRow(tierEl(0), el("span", "wx-caption", "Bank lines matched to ledger entries: exact amount, date within 3 days, reference contains."), spacer(),
      btn("wx-btn is-ghost", "Match again", function () { runInto("reconcile", "run", "reconcile", {}); }, null, "rerun")));
    var res = section("Matches");
    add(res, runBlock(v.run, function (o) { return OUTPUT.reconcile(o); }, "Matching runs when Reconciliation opens."));
    body.appendChild(res);
    var save = section("Save matches");
    add(save, el("p", "wx-body", "Saving records these matches in your books on this computer. That is tier 1: it runs now, writes a receipt, and can be undone."));
    add(save, add(el("div", "wx-row"),
      btn("wx-btn is-ink", "Save matches", function () { runInto("reconcile", "save", "reconcile", { apply: true }); }, null, "save"),
      tierEl(1)));
    if (v.save) add(save, runBlock(v.save, function (o) {
      if (!isObj(o)) return renderAny(o, "output");
      var b = el("div", "wx-stack");
      if (o.applied === true) add(b, add(el("p", "wx-row"), tag("verified is-strong", "SAVED"), el("span", null, str(o.saved_matches != null ? o.saved_matches : "") + " matches saved to your books on this computer.")));
      return add(b, reconCounts(o));
    }, null, function () { undoRun("reconcile", "save"); }));
    body.appendChild(save);
  }

  /* ------------------------- Receivables ------------------------- */

  function renderReceivables(body) {
    if (blocked(body)) return;
    var v = views.receivables;
    var ar = Array.isArray(S.st.ar) ? S.st.ar : [];
    var late = ar.filter(function (a) { return toNum(a.days_late) > 14; }).length;
    add(body, el("p", "wx-body", "Open invoices from aios. Reminder notes go to invoices more than 14 days late."));
    var list = section("Open invoices \u00B7 " + ar.length);
    if (!ar.length) list.appendChild(empty("No open invoices."));
    else list.appendChild(table(ar, {
      cols: ["id", "customer", "amount", "due", "days_late"],
      labels: { id: "Invoice", days_late: "Days late" },
      caption: "Open invoices",
      cell: function (c, val) { return c === "amount" ? document.createTextNode(fmtMoney(toNum(val))) : null; },
      rowClass: function (r) { return toNum(r.days_late) > 14 ? "is-late" : ""; }
    }));
    if (ar.length) list.appendChild(el("p", "wx-caption", late + " more than 14 days late"));
    body.appendChild(list);

    var draft = section("Draft reminders");
    add(draft, el("p", "wx-body", "Drafting saves a reminder note per late invoice. Tier 1: it runs now, writes a receipt, and can be undone."));
    add(draft, add(el("div", "wx-row"),
      btn("wx-btn is-ink", "Draft reminders", function () { runInto("receivables", "draft", "ar-reminders", {}); }, null, "draft"), tierEl(1)));
    if (v.draft) add(draft, runBlock(v.draft, function (o) { return OUTPUT["ar-reminders"](o); }, null, function () { undoRun("receivables", "draft"); }));
    else if (Array.isArray(S.st.drafts) && S.st.drafts.length) add(draft, el("p", "wx-caption", S.st.drafts.length + " reminder drafts are saved and waiting to be sent."));
    if (Array.isArray(S.st.sent) && S.st.sent.length) add(draft, el("p", "wx-caption", S.st.sent.length + " reminders already sent to the outbox: " + S.st.sent.map(function (x) { return str(x.invoice); }).join(", ")));
    body.appendChild(draft);

    var send = section("Send reminders");
    add(send, el("p", "wx-body", "Sending is outward, so it is tier 2: I write a proposal, and nothing goes anywhere until you type APPROVE. In this demo, sending writes to the outbox folder on this computer and contacts no one."));
    add(send, add(el("div", "wx-row"),
      btn("wx-btn is-signal", "Send reminders", function () {
        runInto("receivables", "send", "send-reminders", {}, function (res) {
          if (res.status === "proposal") openWindow("approvals", { focusHash: res.proposal.hash });
        });
      }, null, "send"), tierEl(2)));
    if (v.send) add(send, runBlock(v.send, function (o) { return renderAny(o, "output"); }));
    body.appendChild(send);
  }

  /* ------------------------- Franchise rollup ------------------------- */

  function renderRollup(body) {
    if (blocked(body)) return;
    var v = views.rollup;
    var st = S.st;
    var rates = [];
    if (isFinite(toNum(st.royalty_rate))) rates.push("royalty " + fmtNumber(toNum(st.royalty_rate) * 100, 2) + "%");
    if (isFinite(toNum(st.ad_fund_rate))) rates.push("ad fund " + fmtNumber(toNum(st.ad_fund_rate) * 100, 2) + "%");
    add(body, headRow(tierEl(0), el("span", "wx-caption", rates.length ? "Rates from the franchise agreement: " + rates.join(" \u00B7 ") : "Rates: NO_SIGNAL"), spacer(),
      paramSelect("rollup", "franchisor-rollup", "period", "Month"),
      btn("wx-btn is-ghost", "Roll up", function () { runInto("rollup", "run", "franchisor-rollup", paramsOf("rollup")); }, null, "rerun")));
    var s = section("Per location and franchise total");
    add(s, runBlock(v.run, function (o) { return OUTPUT["franchisor-rollup"](o); }, "The rollup runs when this window opens."));
    body.appendChild(s);
  }

  /* ------------------------- Inbox ------------------------- */

  var INBOX_TAGS = ["all", "customer", "vendor", "franchisor", "staff"];

  function renderInbox(body, w) {
    w.meta.textContent = "";
    if (blocked(body)) return;
    var v = views.inbox;
    var all = Array.isArray(S.st.inbox) ? S.st.inbox : [];
    w.meta.textContent = all.length + (all.length === 1 ? " message" : " messages");
    var filters = el("div", "wx-row filters");
    filters.setAttribute("role", "group");
    filters.setAttribute("aria-label", "Show messages from");
    INBOX_TAGS.forEach(function (t) {
      var n = t === "all" ? all.length : all.filter(function (m) { return m.tag === t; }).length;
      var b = btn("wx-btn is-ghost", (t === "all" ? "All" : humanize(t)) + " \u00B7 " + n, function () { v.filter = t; paint("inbox"); }, null, "filter:" + t);
      b.setAttribute("aria-pressed", v.filter === t ? "true" : "false");
      filters.appendChild(b);
    });
    body.appendChild(filters);
    var shown = v.filter === "all" ? all : all.filter(function (m) { return m.tag === v.filter; });
    if (!shown.length) { body.appendChild(empty("No messages here.")); return; }
    var sorted = shown.slice().sort(function (a, b) { return str(b.received).localeCompare(str(a.received)); });
    body.appendChild(table(sorted, {
      cols: ["tag", "from", "subject", "location", "received"],
      labels: { tag: "Kind", subject: "Message" },
      caption: "Inbox",
      cell: function (c, val, row) {
        if (c === "tag") return tag("hold", str(val));
        if (c === "location") return document.createTextNode(val ? locName(val) : DASH);
        if (c === "subject") {
          var d = el("div", "msg");
          add(d, el("div", "wx-label", str(val)), row.preview ? el("div", "wx-caption msg-preview", str(row.preview)) : null);
          return d;
        }
        return null;
      }
    }));
  }

  /* ------------------------- Receipts ------------------------- */

  function loadReceipts() {
    var v = views.receipts;
    v.status = "loading";
    paint("receipts");
    return api("GET", "/api/receipts?limit=50").then(function (r) {
      if (r.aborted) { v.status = "aborted"; }
      else if (r.nosignal) { v.status = "nosignal"; }
      else if (r.ok && isObj(r.json)) { v.status = "done"; v.data = r.json; }
      else { v.status = "error"; v.error = errText(r); }
      paint("receipts");
    });
  }

  function renderReceipts(body, w) {
    var v = views.receipts;
    if (S.link.up === false || v.status === "nosignal") { w.meta.textContent = "NO_SIGNAL"; add(body, noSignalBlock(function () { loadReceipts(); })); return; }
    var d = v.data;
    var head = headRow();
    if (d) {
      var ok = d.chain_ok === true;
      add(head, tag((ok ? "verified" : "miss") + " is-strong", ok ? "CHAIN OK" : "CHAIN BROKEN"),
        el("span", "wx-caption", ok ? str(d.count) + " records, each linked to the hash of the one before it." : "A record does not match the hash chain. Something changed receipts.jsonl outside aios."));
      w.meta.textContent = (ok ? "chain ok" : "chain BROKEN") + " \u00B7 " + str(d.count);
    } else w.meta.textContent = "";
    add(head, spacer(), btn("wx-btn is-ghost", "Refresh", function () { loadReceipts(); }, null, "refresh"));
    body.appendChild(head);
    if (v.status === "loading" && !d) { body.appendChild(empty("Reading receipts\u2026")); return; }
    if (v.status === "error") { add(body, add(el("p", "err wx-row"), tag("miss", "MISS"), el("span", null, v.error))); return; }
    if (v.status === "aborted" && !d) { body.appendChild(empty("Stopped.")); return; }
    if (!d) { body.appendChild(empty()); return; }
    var recs = listOf(d.records, "records");
    if (!recs.length) { body.appendChild(empty("No receipts yet.")); return; }
    body.appendChild(el("p", "wx-caption", "Newest first, last " + recs.length + ". Stored in receipts.jsonl in your data folder."));
    body.appendChild(table(recs, {
      cols: ["seq", "ts", "kind", "agent", "tier", "summary", "hash"],
      labels: { seq: "#", ts: "Time" }, compact: true,
      caption: "Receipts, newest first",
      cell: function (c, val) {
        if (c === "tier") return tierEl(val);
        if (c === "hash") { var s = el("span", "wx-mono", hashPrefix(val, 12)); s.title = str(val); return s; }
        if (c === "agent") return document.createTextNode(val ? agentName(val) : DASH);
        if (c === "seq") return document.createTextNode(str(val));
        return null;
      }
    }));
  }

  /* ------------------------- What agents may do (GET /api/capabilities) ------------------------- */

  function loadCapabilities() {
    var v = views.capabilities;
    v.status = "loading";
    paint("capabilities");
    return api("GET", "/api/capabilities").then(function (r) {
      if (r.aborted) v.status = "aborted";
      else if (r.nosignal) v.status = "nosignal";
      else if (r.ok) { v.status = "done"; v.list = listOf(r.json, "capabilities"); }
      else { v.status = "error"; v.error = errText(r); }
      paint("capabilities");
    });
  }

  function capList(items, cls, emptyText) {
    var arr = Array.isArray(items) ? items : [];
    if (!arr.length) return empty(emptyText);
    var ul = el("ul", "caps " + cls);
    arr.forEach(function (x) { ul.appendChild(el("li", null, str(x))); });
    return ul;
  }

  function renderCapabilities(body, w) {
    var v = views.capabilities;
    w.meta.textContent = "";
    if (S.link.up === false || v.status === "nosignal") { add(body, noSignalBlock(function () { loadCapabilities(); })); return; }
    add(body, el("p", "wx-body intro", "What each agent may read, what it may write, and what it never touches, as aios reports it. Tier 2 agents still need you to type APPROVE for every act."));
    if (v.status === "loading" && !v.list) { body.appendChild(empty("Reading from aios\u2026")); return; }
    if (v.status === "error") { add(body, add(el("p", "err wx-row"), tag("miss", "MISS"), el("span", null, "aios did not report capabilities: " + v.error))); return; }
    var list = v.list || [];
    if (!list.length) { body.appendChild(empty("aios reported no agents.")); return; }
    w.meta.textContent = list.length + " entries";
    list.forEach(function (c) {
      var card = el("article", "wx-card cap");
      var hid = nextId("cap");
      card.setAttribute("aria-labelledby", hid);
      var h = el("h3", "wx-label", str(c.name || c.agent));
      h.id = hid;
      add(card, add(el("div", "wx-row"), tierEl(c.tier), h, el("span", "wx-caption", str(c.agent))));
      var grid = el("div", "cap-grid");
      add(grid,
        add(el("div", "cap-col"), eyebrow("Reads", "h4"), capList(c.reads, "reads", "nothing")),
        add(el("div", "cap-col"), eyebrow("Writes", "h4"), capList(c.writes, "writes", "nothing")),
        add(el("div", "cap-col"), eyebrow("Never", "h4"), capList(c.never, "never", "none listed")));
      card.appendChild(grid);
      if (c.network != null) add(card, add(el("p", "wx-row"), el("span", "wx-eyebrow", "Network"), el("span", "wx-caption", str(c.network))));
      body.appendChild(card);
    });
  }

  /* ------------------------- Ask ------------------------- */

  var SUGGESTIONS = ["Which location is furthest behind target?", "What is waiting for my approval?", "Which invoices are late?"];

  function ask(q) {
    var v = views.ask;
    var text = str(q).trim();
    if (!text || v.pending) return;
    v.pending = text;
    v.error = "";
    paint("ask");
    api("POST", "/api/ask", { q: text }).then(function (r) {
      v.pending = null;
      if (r.aborted) v.error = "Stopped.";
      else if (r.nosignal) v.error = "NO SIGNAL from aios. Nothing was answered.";
      else if (isLockedAnswer(r)) { v.error = "REFUSED \u00B7 locked. Unlock the session to ask."; setLocked(true); }
      else if (r.ok && isObj(r.json)) {
        v.history.unshift({ q: text, mode: r.json.mode, answer: r.json.answer, sources: r.json.sources });
        var input = wins.ask && wins.ask.body.querySelector('[data-keep="ask:q"]');
        if (input) input.value = "";
      } else v.error = "MISS \u00B7 " + errText(r);
      paint("ask");
      if (!v.error && v.history.length) announce(str(v.history[0].answer).slice(0, 240));
      poll();
    });
  }

  function llmText() {
    var h = S.health;
    if (!h) return { lamp: "unknown", short: "AI NO_SIGNAL", long: "AI model state: NO_SIGNAL" };
    var l = str(h.llm).toLowerCase();
    if (!l || l === "off") return { lamp: "off", short: "AI model off", long: "AI model off \u00B7 answers are computed from your data on this computer" };
    return { lamp: "on", short: "AI model on", long: "AI model on (" + str(h.model || h.llm) + ") \u00B7 questions you ask are sent to the model" };
  }

  function renderAsk(body) {
    if (S.link.up === false) { add(body, noSignalBlock(function () { reloadCore(true); })); return; }
    var v = views.ask;
    var inputId = nextId("askq");
    var lab = el("label", "sr-only", "Ask about your business");
    lab.htmlFor = inputId;
    var input = el("input", "wx-input");
    input.id = inputId;
    input.type = "text";
    input.placeholder = "Ask about your locations, payroll, invoices\u2026";
    input.autocomplete = "off";
    input.dataset.keep = "ask:q";
    input.addEventListener("keydown", function (e) { if (e.key === "Enter") { e.preventDefault(); ask(input.value); } });
    var go = btn("wx-btn", v.pending ? "Asking\u2026" : "Ask", function () { ask(input.value); }, null, "ask-go");
    if (v.pending) go.disabled = true;
    add(body, lab, add(el("div", "wx-composer"), input, go));
    var sugg = el("div", "wx-row");
    SUGGESTIONS.forEach(function (s, i) { sugg.appendChild(btn("wx-btn is-ghost", s, function () { input.value = s; ask(s); }, null, "sugg:" + i)); });
    var lt = llmText();
    add(body, sugg, add(el("p", "wx-row"), lamp(lt.lamp), el("span", "wx-caption", lt.long)));
    if (v.pending) add(body, add(el("p", "wx-row"), tag("reported", "RUNNING"), el("span", "wx-caption", "asking aios: " + v.pending)));
    if (v.error) { var e = el("p", "err", v.error); e.setAttribute("role", "alert"); add(body, e); }
    v.history.forEach(function (h) {
      var c = el("article", "wx-card answer");
      var mode = str(h.mode).toLowerCase();
      var mtag = mode === "llm" ? tag("inferred", "LLM \u00B7 model answer") : mode === "deterministic" ? tag("reported", "DETERMINISTIC \u00B7 computed from your data") : tag("nosignal", "mode " + (mode || "NO_SIGNAL"));
      add(c, el("p", "wx-label", h.q), add(el("div", "wx-row"), mtag), el("p", "answer-text", str(h.answer)));
      var src = Array.isArray(h.sources) ? h.sources : [];
      if (src.length) {
        var sr = el("div", "wx-row sources");
        add(sr, el("span", "wx-caption", "sources"));
        src.forEach(function (s) { sr.appendChild(tag("unreported", str(s))); });
        c.appendChild(sr);
      }
      body.appendChild(c);
    });
  }

  /* ------------------------- Settings ------------------------- */

  function switchPack(id) {
    var v = views.settings;
    v.packMsg = { cls: "reported", text: "Switching\u2026" };
    paint("settings");
    return api("POST", "/api/pack", { id: id }).then(function (r) {
      if (r.aborted) { v.packMsg = { cls: "dead", text: "Stopped." }; paint("settings"); return; }
      if (r.nosignal) { v.packMsg = { cls: "nosignal", text: "NO SIGNAL from aios. The pack did not change." }; paint("settings"); return; }
      if (!r.ok || !isObj(r.json)) { v.packMsg = { cls: "miss", text: "MISS \u00B7 " + errText(r) }; paint("settings"); return; }
      v.packMsg = { cls: "verified", text: "Switched to " + str(r.json.label || id) + "." };
      v.packRun = { run_id: r.json.run_id, receipt: r.json.receipt, label: str(r.json.label || id), undo: null };
      announce(v.packMsg.text);
      return afterPackChange(r.json);
    });
  }

  // A different pack is active (a switch or its undo): drop per-pack results, re-read everything, re-run open windows.
  function afterPackChange(st) {
    if (isObj(st)) S.st = st;
    ["today", "payroll", "reconcile", "receivables", "rollup"].forEach(function (k) { views[k] = {}; });
    views.inbox.filter = "all";
    views.apex.runs = {};
    return Promise.all([api("GET", "/api/agents"), api("GET", "/api/health"), api("GET", "/api/proposals"), isObj(st) ? null : api("GET", "/api/state")]).then(function (rs) {
      if (rs[0].ok) S.agents = listOf(rs[0].json, "agents");
      if (rs[1].ok && isObj(rs[1].json)) S.health = rs[1].json;
      if (rs[2].ok) setProposals(listOf(rs[2].json, "proposals"), true);
      if (rs[3] && rs[3].ok && isObj(rs[3].json)) S.st = rs[3].json;
      renderAllChrome();
      Object.keys(wins).forEach(function (k) { loadWindow(k, true); });
      repaintAll();
    });
  }

  function undoPack() {
    var v = views.settings;
    var pr = v.packRun;
    if (!pr || !pr.run_id) return;
    pr.undo = { status: "loading" };
    paint("settings");
    postUndo(pr.run_id).then(function (u) {
      pr.undo = u;
      if (u.status === "done") {
        v.packMsg = { cls: "dead", text: u.already ? "That switch was already undone." : "Switch undone. The previous pack is active again." };
        announce(v.packMsg.text);
        return afterPackChange(null);
      }
      paint("settings");
    });
  }

  function radioGroup(name, legend, options, current, keepPrefix) {
    var fs = el("fieldset", "radios");
    add(fs, el("legend", "wx-label", legend));
    options.forEach(function (o) {
      var id = nextId(name);
      var row = el("div", "radio");
      var input = el("input");
      input.type = "radio";
      input.name = name;
      input.id = id;
      input.value = o.value;
      input.checked = o.value === current;
      if (keepPrefix) input.dataset.keep = keepPrefix + o.value;
      if (o.onChange) input.addEventListener("change", function () { if (input.checked) o.onChange(o.value); });
      var lab = el("label", null, o.label);
      lab.htmlFor = id;
      add(row, input, lab);
      if (o.note) add(row, el("span", "wx-caption", o.note));
      fs.appendChild(row);
    });
    return fs;
  }

  function renderSettings(body) {
    var v = views.settings;
    var up = S.link.up !== false;

    var pk = section("Business pack");
    if (!up) pk.appendChild(noSignalBlock(function () { reloadCore(true); }));
    else if (!S.packs.length) pk.appendChild(empty());
    else {
      var cur = S.st ? str(S.st.pack) : S.health ? str(S.health.pack) : "";
      var gname = nextId("pack");
      pk.appendChild(radioGroup(gname, "Demo business", S.packs.map(function (p) {
        return { value: str(p.id), label: str(p.label || p.id), note: str(p.id) === cur ? "current" : "" };
      }), cur, "pack:"));
      add(pk, add(el("div", "wx-row"),
        btn("wx-btn is-ink", "Switch pack", function () {
          var chosen = body.querySelector('input[name="' + gname + '"]:checked');
          if (chosen && chosen.value !== cur) switchPack(chosen.value);
          else { v.packMsg = { cls: "pass", text: "That pack is already active." }; paint("settings"); }
        }, null, "switch-pack"), tierEl(1)));
      add(pk, el("p", "wx-caption", "Switching is tier 1: reversible, and it writes a receipt."));
      if (v.packMsg) { var m = el("p", "wx-row"); m.setAttribute("role", "status"); add(m, tag(v.packMsg.cls, v.packMsg.text)); pk.appendChild(m); }
      var pr = v.packRun;
      if (pr && isObj(pr.receipt)) pk.appendChild(receiptLine(pr.receipt));
      if (pr && pr.run_id) {
        var u = pr.undo;
        if (u && u.status === "done") { if (isObj(u.receipt)) pk.appendChild(receiptLine(u.receipt)); }
        else {
          var urow = add(el("div", "wx-row"), btn("wx-btn is-ghost", u && u.status === "loading" ? "Undoing\u2026" : "Undo switch", undoPack, "Undo this pack switch", "undo-pack"),
            el("span", "wx-caption", "Switching back is a tier 1 undo with its own receipt."));
          if (u && u.error) add(urow, tag(u.status === "locked" ? "refused" : "miss", u.status === "locked" ? "REFUSED \u00B7 locked" : "MISS"), el("span", "wx-caption", u.error));
          pk.appendChild(urow);
        }
      }
    }
    body.appendChild(pk);

    var ap = section("Appearance");
    ap.appendChild(radioGroup(nextId("theme"), "Desk", [
      { value: "system", label: "Follow the system", onChange: setTheme },
      { value: "light", label: "Paper desk (light)", onChange: setTheme },
      { value: "dark", label: "Night desk (dark)", onChange: setTheme }
    ], readTheme(), null));
    body.appendChild(ap);

    var ai = section("AI model");
    var lt = llmText();
    add(ai, add(el("p", "wx-row"), lamp(lt.lamp), el("span", null, lt.long)));
    add(ai, el("p", "wx-caption", "The model is off unless aios is started with ANTHROPIC_API_KEY set. With it off, nothing leaves this computer."));
    body.appendChild(ai);

    var ab = section("About");
    var dl = el("dl", "kv");
    var h = S.health;
    add(dl, el("dt", null, "Product"), el("dd", null, "SMB OS Desktop \u00B7 Business Intelligence for small multi-location franchises: today's numbers per location, payroll and bank checks, the franchise rollup, and an approvals gate for anything that moves money."),
      el("dt", null, "Runtime"), hud("dd", "wx-mono", "version", h ? str(h.name || "aios") + " " + str(h.version) : "NO_SIGNAL"),
      el("dt", null, "Data"), add(el("dd"), tag("hold is-strong", "Fictional demo data"), el("span", "wx-body", " Every business, person and number here is made up for the demo.")),
      el("dt", null, "Where your data lives"), el("dd", null,
        "On this computer only, in the folder aios was started with (by default your user config folder, under smb-os-desktop). " +
        "receipts.jsonl holds the receipt chain; outbox/ holds anything a tier 2 approval wrote."),
      el("dt", null, "Data folder"), el("dd", "wx-mono", h && h.data_dir ? str(h.data_dir) : "NO_SIGNAL \u00B7 aios did not say"));
    ab.appendChild(dl);
    body.appendChild(ab);

    var kb = section("Keyboard");
    var ul = el("ul", "plain keys");
    [["Ctrl+K or Ctrl+Space", "open or close the Apex"], ["Enter or double-click", "open a desktop icon"], ["Arrow keys", "move between icons; on a focused title bar, move the window"],
      ["Shift + arrow keys", "on a focused title bar, resize the window"], ["Escape", "close the Apex"], ["Tab", "inside the Apex, moves through its four bands"]].forEach(function (k) {
      add(ul, add(el("li"), el("kbd", "wx-mono", k[0]), el("span", null, " " + k[1])));
    });
    kb.appendChild(ul);
    body.appendChild(kb);
  }

  function statusTag(st) {
    var s = str(st).toLowerCase();
    if (s === "ok") return tag("pass", "OK");
    if (s === "review") return tag("hold", "REVIEW");
    if (s === "blocked" || s === "block") return tag("miss", "BLOCKED");
    return tag("nosignal", s ? s.toUpperCase() : "NO_SIGNAL");
  }

  function figures(pairs) {
    var g = el("div", "figs");
    pairs.forEach(function (p) {
      if (p[1] == null || p[1] === "") return;
      var f = el("div", "fig");
      add(f, el("span", "wx-caption", p[0]), hud("span", "fig-v", "fig:" + p[0], p[1]));
      g.appendChild(f);
    });
    return g;
  }

  function hrs(v) { var n = toNum(v); return isFinite(n) ? fmtNumber(n, 2) + " h" : null; }
  function pct(v) { var n = toNum(v); return isFinite(n) ? fmtNumber(n, 1) + "%" : null; }

  // Payroll precheck: ready or not, totals, one row per person, then each issue.
  function renderPrecheck(o) {
    if (!isObj(o) || !Array.isArray(o.people)) return renderOutput(o, {});
    var box = el("div", "out wx-stack");
    var head = el("div", "wx-row");
    if (typeof o.ready === "boolean") add(head, tag((o.ready ? "verified" : "miss") + " is-strong", o.ready ? "READY" : "NOT READY"));
    if (o.summary) add(head, el("span", "wx-lead", str(o.summary)));
    box.appendChild(head);
    var per = isObj(o.period) ? "Pay period " + str(o.period.start) + " to " + str(o.period.end) + (o.period.pay_date ? " \u00B7 pay date " + str(o.period.pay_date) : "") : "";
    var basis = o.basis === "evv_visits" ? "visits checked against EVV" : o.basis === "flagged_vs_clocked" ? "flagged hours checked against clocked hours" : str(o.basis);
    add(box, el("p", "wx-caption", [per, basis, o.location && o.location !== "all" ? "location " + locName(o.location) : "all locations"].filter(Boolean).join(" \u00B7 ")));
    var t = isObj(o.totals) ? o.totals : {};
    box.appendChild(figures([["People", t.people != null ? fmtNumber(toNum(t.people)) : null], ["With issues", t.people_with_issues != null ? fmtNumber(toNum(t.people_with_issues)) : null],
      ["Blocking", t.blocking != null ? fmtNumber(toNum(t.blocking)) : null], ["Scheduled", hrs(t.scheduled_hours)], ["Worked", hrs(t.worked_hours)],
      ["Overtime", hrs(t.overtime_hours)], ["Flagged", hrs(t.flagged_hours)], ["EVV verified", pct(t.evv_verified_pct)]]));
    var evv = o.people.some(function (x) { return isObj(x.visits); });
    var cols = ["name", "location", "scheduled_hours", "worked_hours", "variance_hours", "overtime_hours"].concat(evv ? ["visits"] : ["flagged_hours", "efficiency_pct"]).concat(["issues", "status"]);
    box.appendChild(table(o.people, {
      cols: cols, caption: "People", compact: true,
      labels: { scheduled_hours: "Sched h", worked_hours: "Worked h", variance_hours: "Var h", overtime_hours: "OT h", flagged_hours: "Flagged h", efficiency_pct: "Eff %" },
      cell: function (c, v, row) {
        if (c === "status") return statusTag(v);
        if (c === "location") return document.createTextNode(locName(v));
        if (c === "name") return add(el("div", "msg"), el("div", "wx-label", str(v)), row.role ? el("div", "wx-caption", str(row.role)) : null);
        if (c === "visits" && isObj(v)) return document.createTextNode(str(v.verified) + "/" + str(v.scheduled) + " verified" + (v.missed ? " \u00B7 " + v.missed + " missed" : ""));
        return null;
      },
      rowClass: function (r) { return r.status === "blocked" ? "is-late" : ""; }
    }));
    if (Array.isArray(o.issues) && o.issues.length) {
      var s = el("section", "sec");
      add(s, eyebrow("Issues \u00B7 " + o.issues.length));
      var ul = el("ul", "lines");
      o.issues.forEach(function (i) {
        var sev = str(i.severity).toLowerCase();
        add(ul, add(el("li"), sev === "block" || sev === "blocking" ? tag("miss", "BLOCKS") : tag("hold", "REVIEW"),
          el("span", "wx-label", str(i.name)), el("span", "wx-caption", [locName(i.location), i.date].filter(Boolean).join(" \u00B7 ")), el("span", null, str(i.text))));
      });
      s.appendChild(ul);
      box.appendChild(s);
    }
    if (o.note) box.appendChild(el("p", "wx-caption", str(o.note)));
    return box;
  }

  // Bank reconciliation: counts, the four lists, exceptions, totals.
  function renderRecon(o) {
    if (!isObj(o)) return renderAny(o, "output");
    var box = el("div", "out wx-stack");
    if (o.summary) box.appendChild(el("p", "wx-lead out-lead", str(o.summary)));
    var cap = [o.account ? "account " + str(o.account) : "", o.from && o.to ? str(o.from) + " to " + str(o.to) : ""].filter(Boolean).join(" \u00B7 ");
    if (cap) box.appendChild(el("p", "wx-caption", cap));
    box.appendChild(reconCounts(o));
    if (o.applied === true) box.appendChild(add(el("p", "wx-row"), tag("verified is-strong", "SAVED"), el("span", null, str(o.saved_matches != null ? o.saved_matches : "") + " matches saved to your books on this computer.")));
    var lists = [
      ["matched", "Matched", ["bank_id", "ledger_id", "amount", "bank_date", "ledger_date", "days_apart", "ref"]],
      ["unmatched_bank", "Unmatched bank lines", ["id", "date", "amount", "description", "hint"]],
      ["unmatched_ledger", "Unmatched ledger entries", ["id", "date", "amount", "memo", "ref", "account", "hint"]],
      ["exceptions", "Exceptions", ["type", "bank_id", "ledger_id", "text"]],
      ["suggested_entries", "Suggested entries", ["bank_id", "date", "amount", "debit", "credit", "memo"]],
      ["suggested", "Suggested entries", null]];
    lists.forEach(function (l) {
      if (!Array.isArray(o[l[0]])) return;
      var s = el("section", "sec");
      add(s, eyebrow(l[1] + " \u00B7 " + o[l[0]].length));
      if (!o[l[0]].length) s.appendChild(empty("none"));
      else {
        var cols = l[2] ? l[2].filter(function (c) { return o[l[0]].some(function (r) { return r[c] != null && r[c] !== ""; }); }) : null;
        s.appendChild(table(o[l[0]], { cols: cols || undefined, caption: l[1], labels: { bank_id: "Bank", ledger_id: "Ledger", days_apart: "Days apart", ref: "Reference" },
          cell: function (c, v) { return c === "amount" ? document.createTextNode(fmtMoney(toNum(v))) : null; } }));
      }
      box.appendChild(s);
    });
    if (isObj(o.totals)) { var ts = el("section", "sec"); add(ts, eyebrow("Totals"), renderAny(o.totals, "totals")); box.appendChild(ts); }
    if (Array.isArray(o.rules) && o.rules.length) { var rs = el("section", "sec"); add(rs, eyebrow("Matching rules")); var ul = el("ul", "plain"); o.rules.forEach(function (r) { ul.appendChild(el("li", "wx-caption", str(r))); }); rs.appendChild(ul); box.appendChild(rs); }
    return box;
  }

  // Franchise rollup: one row per location plus the franchise total.
  function renderRollupOut(o) {
    if (!isObj(o) || !Array.isArray(o.locations)) return renderOutput(o, {});
    var box = el("div", "out wx-stack");
    if (o.summary) box.appendChild(el("p", "wx-lead out-lead", str(o.summary)));
    box.appendChild(el("p", "wx-caption", [o.period_label || o.period, o.prior_period ? "compared with " + str(o.prior_period) : "",
      isFinite(toNum(o.royalty_rate)) ? "royalty " + fmtField("royalty_rate", toNum(o.royalty_rate)) : "", isFinite(toNum(o.ad_fund_rate)) ? "ad fund " + fmtField("ad_fund_rate", toNum(o.ad_fund_rate)) : "",
      o.basis ? "on " + str(o.basis).replace(/_/g, " ") : ""].filter(Boolean).join(" \u00B7 ")));
    var rows = o.locations.map(function (l) { return Object.assign({ _name: str(l.name) + " " + str(l.id) }, l); });
    if (isObj(o.total)) rows.push(Object.assign({ _name: "Franchise total", _total: true }, o.total));
    var cols = ["_name", "gross_sales", "allowances", "net_sales", "royalty", "ad_fund", "total_due"];
    if (rows.some(function (r) { return r.change_pct != null; })) cols.push("change_pct");
    box.appendChild(table(rows, {
      cols: cols, caption: "Rollup", compact: true, labels: { _name: "Location", total_due: "Due to franchisor", change_pct: "Net vs prior" },
      cell: function (c, v) {
        if (c === "change_pct") {
          var n = toNum(v);
          if (!isFinite(n)) return document.createTextNode(DASH);
          return document.createTextNode((n > 0 ? "\u25B2 up " : n < 0 ? "\u25BC down " : "flat ") + fmtNumber(Math.abs(n), 1) + "%");
        }
        if (c !== "_name") { var m = toNum(v); return document.createTextNode(isFinite(m) ? fmtMoney(m) : DASH); }
        return null;
      },
      rowClass: function (r) { return r._total ? "is-total" : ""; }
    }));
    return box;
  }

  // Receivables reminder drafts: one note per late invoice, nothing sent.
  function renderDrafts(o) {
    if (!isObj(o) || !Array.isArray(o.drafts)) return renderOutput(o, {});
    var box = el("div", "out wx-stack");
    if (o.summary) box.appendChild(el("p", "wx-lead out-lead", str(o.summary)));
    if (!o.drafts.length) box.appendChild(empty("No invoice is late enough for a reminder."));
    o.drafts.forEach(function (d) {
      var c = el("details", "draft wx-card");
      var sm = el("summary");
      add(sm, tag("hold", str(d.stage || "draft")), el("span", "wx-label", str(d.subject || d.invoice)),
        el("span", "wx-caption", [d.customer, d.invoice, fmtMoney(toNum(d.amount)), d.days_late != null ? d.days_late + " days late" : ""].filter(Boolean).join(" \u00B7 ")));
      add(c, sm, el("p", "draft-body", str(d.body)));
      box.appendChild(c);
    });
    var tail = [];
    if (o.total != null) tail.push("total " + fmtMoney(toNum(o.total)));
    if (Array.isArray(o.already_sent) && o.already_sent.length) tail.push("already reminded: " + o.already_sent.join(", "));
    if (Array.isArray(o.skipped) && o.skipped.length) tail.push("not late enough: " + o.skipped.join(", "));
    if (tail.length) box.appendChild(el("p", "wx-caption", tail.join(" \u00B7 ")));
    return box;
  }

  // What an approved tier-2 act returned: summary, the files it wrote, the demo note.
  function renderExec(o) {
    if (!isObj(o)) return renderAny(o, "output");
    var box = el("div", "out wx-stack");
    if (o.summary) box.appendChild(el("p", "wx-body", str(o.summary)));
    if (Array.isArray(o.files) && o.files.length) {
      var ul = el("ul", "plain");
      o.files.forEach(function (f) { ul.appendChild(el("li", "wx-mono", str(f))); });
      box.appendChild(add(el("div", "sec"), eyebrow("Written to your data folder", "h4"), ul));
    }
    if (o.note) box.appendChild(el("p", "wx-caption", str(o.note)));
    if (!o.summary && !o.files) box.appendChild(renderAny(o, "output"));
    return box;
  }

  // How each agent's output is drawn wherever it appears (its window, the Apex).
  var OUTPUT = {
    "morning-brief": renderBrief,
    "payroll-precheck": renderPrecheck,
    "reconcile": renderRecon,
    "franchisor-rollup": renderRollupOut,
    "ar-reminders": renderDrafts
  };

  var RENDER = {
    today: renderToday, approvals: renderApprovals, inbox: renderInbox, ask: renderAsk, payroll: renderPayroll,
    reconcile: renderReconcile, receivables: renderReceivables, rollup: renderRollup, receipts: renderReceipts, settings: renderSettings,
    capabilities: renderCapabilities
  };

  /* ------------------------- desktop ------------------------- */

  var deskSel = null;

  function mountDesk() {
    var desk = document.getElementById("desk");
    refs.desk = desk;
    var nav = el("nav", "icons");
    nav.setAttribute("aria-label", "Desktop icons");
    nav.addEventListener("keydown", onDeskKey);
    refs.icons = nav;
    var card = el("section", "deskcard wx-card");
    card.setAttribute("aria-label", "This business");
    refs.deskCard = card;
    var winLayer = el("div", "wins");
    refs.wins = winLayer;
    var hint = el("p", "hint wx-caption", "Enter or double-click opens \u00B7 Ctrl+K opens the Apex");
    var lockLayer = el("div", "locklayer");
    lockLayer.hidden = true;
    refs.lockLayer = lockLayer;
    add(desk, nav, card, winLayer, hint, lockLayer);
    renderIcons();
    renderDeskCard();
  }

  function renderIcons() {
    var nav = refs.icons;
    if (!nav) return;
    var ae = document.activeElement;
    var focusedKind = ae && ae.classList && ae.classList.contains("icon") ? ae.dataset.kind : null;
    clear(nav);
    GROUPS.forEach(function (g) {
      var sec = el("section", "grp");
      add(sec, el("h2", "wx-eyebrow", g[1]));
      var grid = el("div", "grid");
      ORDER.forEach(function (k) { if (WINDOWS[k].group === g[0]) grid.appendChild(iconFor(k)); });
      add(sec, grid);
      nav.appendChild(sec);
    });
    if (focusedKind) { var again = nav.querySelector('.icon[data-kind="' + focusedKind + '"]'); if (again) again.focus(); }
  }

  function iconFor(kind) {
    var def = WINDOWS[kind];
    var b = el("button", "icon");
    b.type = "button";
    b.dataset.kind = kind;
    b.dataset.group = def.group;
    b.setAttribute("aria-pressed", deskSel === kind ? "true" : "false");
    b.title = def.desc;
    var n = kind === "approvals" && S.link.up !== false ? S.proposals.length : 0;
    var label = def.title + (wins[kind] ? ", open" : "") + (n ? ", " + n + " waiting" : "");
    b.setAttribute("aria-label", label);
    var mono = el("span", "wx-icon", def.mono);
    mono.setAttribute("aria-hidden", "true");
    add(b, mono, el("span", "lbl", def.short || def.title));
    if (n) { var badge = el("span", "badge", String(n)); badge.setAttribute("aria-hidden", "true"); b.appendChild(badge); }
    b.addEventListener("click", function () { selectIcon(kind); });
    b.addEventListener("dblclick", function (e) { e.preventDefault(); openWindow(kind); });
    b.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") { e.preventDefault(); selectIcon(kind); openWindow(kind); }
    });
    return b;
  }

  function selectIcon(kind) {
    deskSel = kind;
    Array.prototype.forEach.call(refs.icons.querySelectorAll(".icon"), function (n) {
      n.setAttribute("aria-pressed", n.dataset.kind === kind ? "true" : "false");
    });
  }

  function onDeskKey(e) {
    var dirs = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -2, ArrowDown: 2 };
    var d = dirs[e.key];
    if (!d) return;
    e.preventDefault();
    var all = Array.prototype.slice.call(refs.icons.querySelectorAll(".icon"));
    var i = all.indexOf(document.activeElement);
    var next = all[Math.max(0, Math.min(all.length - 1, (i === -1 ? 0 : i + d)))];
    if (next) { selectIcon(next.dataset.kind); next.focus(); }
  }

  function renderDeskCard() {
    var c = refs.deskCard;
    if (!c) return;
    clear(c);
    var st = S.st;
    add(c, el("p", "wx-eyebrow", "SMB OS Desktop \u00B7 Business Intelligence"), add(el("p", "wx-row"), tag("hold is-strong", "Fictional demo data")));
    if (S.link.up === false) {
      add(c, el("p", "wx-title", "NO SIGNAL from aios"), el("p", "wx-body", "The local runtime is not answering. Start it again; nothing is shown until it answers."));
      return;
    }
    if (!st) { add(c, el("p", "wx-title", "Starting\u2026"), empty("Reading from aios\u2026")); return; }
    var locs = Array.isArray(st.locations) ? st.locations : [];
    add(c, el("h2", "wx-title", str(st.brand)),
      el("p", "wx-body", str(st.label) + " \u00B7 " + locs.length + (locs.length === 1 ? " location" : " locations")),
      el("p", "wx-caption", "as of " + str(st.as_of) + (st.operator ? " \u00B7 operator " + str(st.operator) : "")),
      el("p", "wx-caption", locs.map(function (l) { return str(l.name) + " " + str(l.id); }).join(" \u00B7 ")),
      el("p", "statement", "I run on this computer and keep your numbers here. Anything that moves money or leaves the building waits for you to type APPROVE."));
  }

  /* ------------------------- dock (the slab taskbar) ------------------------- */

  function mountDock() {
    var host = document.getElementById("dock-host");
    var dock = el("div", "dock wx-taskbar");
    dock.setAttribute("role", "region");
    dock.setAttribute("aria-label", "Dock");
    var brand = btn("wx-brand", "SMB OS", function () { toggleApex(); }, "SMB OS: open the Apex command surface (Ctrl+K)");
    var tasks = el("nav", "tasks");
    tasks.setAttribute("aria-label", "Open windows");
    var right = el("div", "right");
    var appr = btn("asks", "", function () { openWindow("approvals"); });
    appr.setAttribute("data-hud", "approvals");
    var ai = el("span", "read ai");
    var link = el("span", "read link");
    link.setAttribute("role", "status");
    var fict = el("span", "fict", "Fictional demo data");
    var lockRead = btn("lockread", "Locked", function () { if (refs.unlockBtn) refs.unlockBtn.focus(); }, "The session is locked: go to Unlock");
    lockRead.hidden = true;
    refs.lockRead = lockRead;
    var theme = btn("wx-task theme", "Night desk", function () { setTheme(effectiveTheme() === "dark" ? "light" : "dark"); });
    var clock = el("span", "read clock");
    var time = el("time", "hud", "--:--");
    time.setAttribute("data-hud", "clock");
    add(clock, time);
    add(right, lockRead, appr, ai, link, fict, theme, clock);
    add(dock, brand, tasks, right);
    host.appendChild(dock);
    refs.tasks = tasks; refs.appr = appr; refs.ai = ai; refs.link = link; refs.theme = theme; refs.clock = time;
    renderDock();
  }

  function renderDock() {
    if (!refs.tasks) return;
    renderDockTasks();
    var n = S.proposals.length;
    var down = S.link.up === false;
    var a = refs.appr;
    a.className = "asks" + (n && !down ? "" : " is-zero");
    a.textContent = down ? "Approvals NO_SIGNAL" : n ? n + " waiting for approval" : "No approvals waiting";
    a.setAttribute("aria-label", a.textContent + ": open Approvals");
    var lt = llmText();
    clear(refs.ai);
    add(refs.ai, lamp(down ? "unknown" : lt.lamp), el("span", null, down ? "AI NO_SIGNAL" : lt.short));
    refs.ai.title = lt.long;
    renderLink();
    renderDockTheme();
    if (refs.lockRead) refs.lockRead.hidden = !S.locked;
  }

  var dockSig = null;
  function renderDockTasks() {
    var nav = refs.tasks;
    if (!nav) return;
    var open = ORDER.filter(function (k) { return wins[k]; });
    var sig = open.map(function (k) { return k + (wins[k].min ? ":m" : "") + (refs.focused === k ? ":f" : ""); }).join(",");
    if (sig === dockSig) return;
    dockSig = sig;
    rebuild(nav, function () {
      if (!open.length) { add(nav, el("span", "ph", "no windows open")); return; }
      open.forEach(function (k) {
        var w = wins[k];
        var on = refs.focused === k && !w.min;
        var b = btn("wx-task" + (w.min ? " is-min" : ""), WINDOWS[k].title, function () {
          if (on) minimizeWindow(k); else openWindow(k);
        }, WINDOWS[k].title + (w.min ? " (minimized): restore" : on ? ": minimize" : ": bring to front"), "task:" + k);
        b.setAttribute("aria-current", on ? "true" : "false");
        nav.appendChild(b);
      });
    });
  }

  function renderLink() {
    var up = S.link.up;
    var h = S.health;
    var text = up === false ? "NO SIGNAL from aios" : up === null ? "aios NO_SIGNAL \u00B7 not yet read" : "aios " + str(h ? h.version : "") + (h && h.chain_ok === false ? " \u00B7 chain BROKEN" : "");
    var lampKind = up === false || up === null ? "unknown" : h && h.chain_ok === false ? "degraded" : "on";
    if (refs.link) {
      clear(refs.link);
      add(refs.link, lamp(lampKind), el("span", null, text));
      refs.link.title = up === false ? "aios is not answering" + (S.link.why ? ": " + S.link.why : "") : "the local runtime on 127.0.0.1";
    }
    var nl = document.getElementById("narrow-link");
    var nlamp = document.getElementById("narrow-lamp");
    if (nl) nl.textContent = up === false ? "NO SIGNAL from aios" : up ? "aios is running on this computer" : "aios: NO_SIGNAL \u00B7 not yet read";
    if (nlamp) nlamp.className = "wx-lamp " + lampKind;
    document.body.classList.toggle("is-nosignal", up === false);
  }

  function renderDockTheme() {
    if (!refs.theme) return;
    var dark = effectiveTheme() === "dark";
    refs.theme.setAttribute("aria-pressed", dark ? "true" : "false");
    refs.theme.textContent = "Night desk";
    refs.theme.title = dark ? "Night desk is on. Click for the paper desk." : "Paper desk is on. Click for the night desk.";
  }

  function startClock() {
    function tick() {
      var d = new Date();
      if (!refs.clock) return;
      refs.clock.textContent = d.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" });
      refs.clock.setAttribute("datetime", d.toISOString());
      refs.clock.title = d.toLocaleDateString("en-US", { weekday: "long", month: "long", day: "numeric", year: "numeric" });
    }
    tick();
    window.setInterval(tick, 15000);
  }

  /* ------------------------- Apex: four bands ------------------------- */
  // Tier 0 \u00B7 Search (the anchor bar), then three bands: Tier 1 \u00B7 Operations (the business right now),
  // Tier 2 \u00B7 Engine & workers (the agents and the runtime), Tier 3 \u00B7 Sovereignty (the owner's authority, on the slab).

  var qoItems = [];
  var qoSel = 0;
  var prevFocus = null;
  var toastTimer = null;
  var AGENT_WINDOW = { "morning-brief": "today", "payroll-precheck": "payroll", "payroll-submit": "payroll", "reconcile": "reconcile",
    "ar-reminders": "receivables", "send-reminders": "receivables", "franchisor-rollup": "rollup" };

  function mountApex() {
    var host = document.getElementById("apex-host");
    refs.apexHost = host;
    var apex = el("div", "apex");
    apex.setAttribute("role", "dialog");
    apex.setAttribute("aria-modal", "true");
    apex.setAttribute("aria-label", "Apex command surface");
    refs.apex = apex;

    var head = el("div", "head");
    var qo = el("input", "qo wx-input");
    qo.id = "apex-search";
    qo.type = "text";
    qo.placeholder = "Search locations, numbers, agents, approvals, receipts\u2026";
    qo.setAttribute("autocomplete", "off");
    qo.setAttribute("spellcheck", "false");
    qo.setAttribute("role", "combobox");
    qo.setAttribute("aria-autocomplete", "list");
    qo.setAttribute("aria-expanded", "false");
    qo.setAttribute("aria-controls", "qo-results");
    var lab = el("label", "band-label");
    lab.htmlFor = qo.id;
    add(lab, el("span", "tier-eyebrow", "Tier 0"), el("span", "band-name", "Search"));
    var results = el("ul", "results");
    results.id = "qo-results";
    results.setAttribute("role", "listbox");
    results.setAttribute("aria-label", "Matches");
    results.hidden = true;
    var qwrap = add(el("div", "qwrap"), qo, results);
    var hint = el("span", "keyhint", "Ctrl+K \u00B7 Ctrl+Space");
    hint.setAttribute("aria-hidden", "true");
    var stop = btn("wx-stop", "Stop everything", stopEverything, "Stop everything: cancel every request and close every window", "stop");
    var close = btn("wx-winbtn apex-close", "\u00D7", closeApex, "Close the Apex (Escape)");
    add(head, el("span", "brand wx-pill is-ink", "APEX"), lab, qwrap, hint, stop, close);
    qo.addEventListener("input", function () { qoSel = 0; refreshResults(); });
    qo.addEventListener("keydown", onQoKey);
    refs.qo = qo; refs.results = results; refs.qwrap = qwrap;

    var bands = el("div", "bands");
    add(bands, band("ops", "Tier 1", "Operations", false), band("eng", "Tier 2", "Engine & workers", false), band("sov", "Tier 3", "Sovereignty", true));
    var status = el("div", "status");
    var st = el("span", "st");
    var toastEl = el("span", "toast");
    toastEl.setAttribute("role", "status");
    add(status, st, toastEl);
    refs.apexStatus = st; refs.toast = toastEl;
    add(apex, head, bands, status);
    host.appendChild(apex);
    host.addEventListener("pointerdown", function (e) { if (e.target === host) closeApex(); });
    document.addEventListener("pointerdown", function (e) { if (!qwrap.contains(e.target)) hideResults(); });
  }

  function band(key, tier, name, slab) {
    var s = el("section", "band wx-window band-" + key + (slab ? " is-slab" : ""));
    var hid = "band-" + key + "-title";
    s.setAttribute("aria-labelledby", hid);
    var bar = el("div", "wx-bar" + (slab ? "" : " well"));
    var h = el("h2", "wx-title-text band-title");
    h.id = hid;
    add(h, el("span", "tier-eyebrow", tier), el("span", "band-name", name));
    var meta = el("span", "meta hud");
    add(bar, h, meta);
    var body = el("div", "wx-body");
    add(s, bar, body);
    refs["band_" + key] = body;
    refs["bmeta_" + key] = meta;
    return s;
  }

  function apexOpen() { return refs.apexHost && !refs.apexHost.hidden; }

  function openApex() {
    if (apexOpen() || isNarrow()) return;
    prevFocus = document.activeElement;
    refs.apexHost.hidden = false;
    document.getElementById("app").inert = true;
    renderApex();
    loadApexReceipts();
    refs.qo.value = "";
    hideResults();
    refs.qo.focus();
  }

  function closeApex() {
    if (!apexOpen()) return;
    refs.apexHost.hidden = true;
    document.getElementById("app").inert = false;
    hideResults();
    var back = prevFocus && document.contains(prevFocus) && !(prevFocus.closest && prevFocus.closest("[inert]")) ? prevFocus : null;
    prevFocus = null;
    if (S.locked && refs.unlockBtn) { refs.unlockBtn.focus(); return; }
    if (back && typeof back.focus === "function") back.focus();
    else { var t = topWindow(null); if (t) focusWindow(t.kind, true); else focusIcon("today"); }
  }

  function toggleApex() { if (apexOpen()) closeApex(); else openApex(); }

  function toast(text) {
    if (!refs.toast) return;
    refs.toast.textContent = str(text);
    window.clearTimeout(toastTimer);
    toastTimer = window.setTimeout(function () { refs.toast.textContent = ""; }, 8000);
  }

  // Open a window from the Apex; a locked session opens nothing.
  function openFromApex(kind, opts) {
    if (S.locked) { toast("The session is locked. Unlock it first."); return; }
    closeApex();
    openWindow(kind, opts);
  }

  function loadApexReceipts() {
    return api("GET", "/api/receipts?limit=50").then(function (r) {
      if (r.ok && isObj(r.json)) views.apex.receipts = listOf(r.json.records, "records");
      if (apexOpen()) { renderEng(); }
    });
  }

  function renderApex() { renderOps(); renderEng(); renderSov(); renderApexStatus(); }

  /* Tier 1 \u00B7 Operations: the business right now, location by location */
  function renderOps() { if (refs.band_ops) rebuild(refs.band_ops, buildOps); }
  function buildOps(body) {
    var meta = refs.bmeta_ops;
    if (S.link.up === false) { meta.textContent = "NO_SIGNAL"; body.appendChild(noSignalBlock(function () { reloadCore(true); })); return; }
    var st = S.st;
    if (!st) { meta.textContent = ""; body.appendChild(empty()); return; }
    meta.textContent = "as of " + str(st.as_of);
    var kpis = Array.isArray(st.kpis) ? st.kpis : [];
    var alerts = Array.isArray(st.alerts) ? st.alerts : [];
    (Array.isArray(st.locations) ? st.locations : []).forEach(function (loc) {
      var b = btn("opcard", null, function () { openFromApex("today"); }, null, "op:" + loc.id);
      add(b, add(el("span", "op-head"), el("span", "wx-label", str(loc.name)), el("span", "wx-caption", str(loc.id) + (loc.manager ? " \u00B7 " + str(loc.manager) : ""))));
      var w = worstVariance(kpis.filter(function (k) { return k && k.location === loc.id; }));
      var wl = el("span", "op-line");
      if (!w) add(wl, tag("nosignal", "NO_SIGNAL"), el("span", "wx-caption", "no measures reported"));
      else if (w.gap >= 0) add(wl, tag("verified", "ALL ON TARGET"), el("span", null, "best margin: " + str(w.k.label)));
      else add(wl, tag("miss", "WORST"), el("span", null, str(w.k.label) + " " + fmtValue(w.k.value, w.k.unit) + " vs " + fmtValue(w.k.target, w.k.unit) + " (" + fmtNumber(Math.abs(w.gap), 1) + "% worse)"));
      b.appendChild(wl);
      var mine = alerts.filter(function (a) { return a && a.location === loc.id; });
      var al = el("span", "op-line");
      if (!mine.length) add(al, el("span", "wx-caption", "no open alerts"));
      else {
        ["crit", "warn", "info"].forEach(function (sv) {
          var n = mine.filter(function (a) { return str(a.severity).toLowerCase() === sv; }).length;
          if (n) { var t = sevTag(sv); add(al, tag(t.cls, n + " " + sv)); }
        });
        add(al, el("span", "wx-caption", str(mine[0].text).slice(0, 80) + (str(mine[0].text).length > 80 ? "\u2026" : "")));
      }
      b.appendChild(al);
      var shifts = shiftsToday(st, loc.id);
      add(b, add(el("span", "op-line"), el("span", "wx-caption", shifts.length + " on shift today" +
        (shifts.length ? ": " + shifts.slice(0, 3).map(function (x) { return str(x.who); }).join(", ") + (shifts.length > 3 ? " +" + (shifts.length - 3) : "") : ""))));
      body.appendChild(b);
    });
    var inbox = Array.isArray(st.inbox) ? st.inbox : [];
    var ib = btn("opcard op-inbox", null, function () { openFromApex("inbox"); }, null, "op:inbox");
    var byTag = INBOX_TAGS.slice(1).map(function (t) { var n = inbox.filter(function (m) { return m.tag === t; }).length; return n ? n + " " + t : ""; }).filter(Boolean);
    add(ib, add(el("span", "op-head"), el("span", "wx-label", "Inbox"), el("span", "wx-caption", inbox.length + (inbox.length === 1 ? " message" : " messages"))),
      add(el("span", "op-line"), el("span", "wx-caption", byTag.join(" \u00B7 ") || "empty")));
    body.appendChild(ib);
  }

  /* Tier 2 \u00B7 Engine & workers: every agent as a worker, plus the runtime's own facts */
  function lastReceiptFor(id) {
    var recs = views.apex.receipts || [];
    for (var i = 0; i < recs.length; i++) {
      var r = recs[i];
      if (r && r.agent === id && (r.kind === "run" || r.kind === "approve")) return r;
    }
    return null;
  }

  function workerState(a) {
    var id = a.id;
    var pend = S.proposals.filter(function (p) { return p.agent === id; }).length;
    var last = S.lastRuns[id];
    var rec = lastReceiptFor(id);
    if (pend) return { lamp: "degraded", glyph: "\u25B2", word: "needs attention", why: pend + " waiting for approval" };
    if (last && (last.status === "error" || last.status === "locked")) return { lamp: "degraded", glyph: "\u25B2", word: "needs attention", why: last.status === "locked" ? "refused: the session is locked" : "the last run failed" };
    if (last && last.status === "done" && isObj(last.output) && last.output.ready === false) return { lamp: "degraded", glyph: "\u25B2", word: "needs attention", why: "ran " + fmtWhen(last.at) + " \u00B7 not ready" };
    if (rec) return { lamp: "on", glyph: "\u25CF", word: "ran", why: fmtWhen(rec.ts) + " \u00B7 receipt #" + str(rec.seq) };
    if (last && last.status === "done") return { lamp: "on", glyph: "\u25CF", word: "ran", why: fmtWhen(last.at) };
    return { lamp: "off", glyph: "\u25CB", word: "idle", why: views.apex.receipts ? "no run in the last " + views.apex.receipts.length + " receipts" : "not run yet" };
  }

  function renderEng() { if (refs.band_eng) rebuild(refs.band_eng, buildEng); }
  function buildEng(body) {
    var meta = refs.bmeta_eng;
    if (S.link.up === false) { meta.textContent = "NO_SIGNAL"; body.appendChild(noSignalBlock(function () { reloadCore(true); })); return; }
    var h = S.health;
    var lt = llmText();
    body.appendChild(figures([["Runtime", h ? str(h.name || "aios") + " " + str(h.version) : "NO_SIGNAL"], ["Receipts", h && h.receipts != null ? fmtNumber(toNum(h.receipts)) : "NO_SIGNAL"],
      ["AI model", lt.short.replace(/^AI model /, "")], ["Pack", S.st ? str(S.st.label) : "NO_SIGNAL"]]));
    meta.textContent = S.agents.length + " workers";
    if (!S.agents.length) { body.appendChild(empty()); return; }
    var ul = el("ul", "workers");
    S.agents.forEach(function (a) {
      var ws = workerState(a);
      var li = el("li", "worker");
      var top = el("div", "w-top");
      var win = AGENT_WINDOW[a.id];
      var name = win ? btn("w-name", str(a.name || a.id), function () { openFromApex(win); }, "Open " + WINDOWS[win].title + " for " + str(a.name || a.id), "wname:" + a.id) : el("span", "w-name", str(a.name || a.id));
      var gcls = ws.lamp === "on" ? "ran" : ws.lamp === "degraded" ? "attn" : "idle";
      var g = el("span", "g", ws.glyph);
      g.setAttribute("aria-hidden", "true");
      add(top, add(el("span", "w-glyph " + gcls), g, el("span", null, ws.word)), name, tierEl(a.tier));
      var run = btn("wx-btn is-mono w-run", "Run", function () { apexRun(a.id); }, "Run " + str(a.name || a.id), "run:" + a.id);
      if (S.locked) { run.disabled = true; run.title = "The session is locked"; }
      var row = add(el("div", "w-row"), add(el("div", "w-main"), top, el("p", "wx-caption", ws.why)), run);
      li.appendChild(row);
      var res = views.apex.runs[a.id];
      if (res) li.appendChild(workerResult(a, res));
      ul.appendChild(li);
    });
    body.appendChild(ul);
  }

  function workerResult(a, res) {
    var p = el("div", "w-result");
    p.setAttribute("role", "status");
    if (res.status === "loading") return add(p, tag("reported", "RUNNING"), el("span", "wx-caption", "asking aios\u2026"));
    if (res.status === "done") {
      var o = res.output;
      var line = isObj(o) ? str(o.summary || o.headline || "") : "";
      return add(p, tag("verified is-strong", "RECEIPT #" + (isObj(res.receipt) ? str(res.receipt.seq) : "?")), el("span", "w-out", line || "done"));
    }
    if (res.status === "proposal") {
      return add(p, tag("hold", "WAITING FOR APPROVE"), btn("wx-btn is-ghost", "Open Approvals", function () { openFromApex("approvals", { focusHash: res.proposal.hash }); }, null, "wopen:" + a.id));
    }
    if (res.status === "locked") return add(p, tag("refused is-strong", "REFUSED \u00B7 locked"), el("span", "wx-caption", "aios refused: the session is locked."));
    if (res.status === "nosignal") return add(p, tag("nosignal", "NO_SIGNAL"), el("span", "wx-caption", "aios did not answer."));
    if (res.status === "aborted") return add(p, tag("dead", "STOPPED"));
    return add(p, tag("miss", "MISS"), el("span", "wx-caption", str(res.error)));
  }

  function apexRun(id) {
    if (S.locked) { toast("The session is locked. Unlock it to run agents."); return; }
    views.apex.runs[id] = { status: "loading" };
    renderEng();
    runAgent(id, {}).then(function (res) {
      views.apex.runs[id] = res;
      if (res.status === "done") toast(agentName(id) + ": done, receipt " + (isObj(res.receipt) ? "#" + str(res.receipt.seq) : "missing"));
      else if (res.status === "proposal") toast(agentName(id) + ": proposal written. Nothing runs until you type APPROVE.");
      else if (res.status === "error") toast(agentName(id) + ": " + res.error);
      return loadApexReceipts();
    }).then(function () { if (apexOpen()) renderApex(); poll(); });
  }

  /* Tier 3 \u00B7 Sovereignty: the owner's authority, on the slab */
  function renderSov() { if (refs.band_sov) rebuild(refs.band_sov, buildSov); }
  function buildSov(body) {
    var meta = refs.bmeta_sov;
    if (S.link.up === false) { meta.textContent = "NO_SIGNAL"; body.appendChild(noSignalBlock(function () { reloadCore(true); })); return; }
    var h = S.health || {};
    var n = S.proposals.length;
    meta.textContent = S.locked ? "LOCKED" : n ? n + " waiting" : "";
    var ap = el("div", "sov-row");
    add(ap, add(el("div", "sov-k"), el("span", "wx-caption", "Approvals waiting"), hud("span", "sov-big", "sov-approvals", String(n))),
      btn("wx-btn" + (n ? " is-signal" : ""), "Open approvals", function () { openFromApex("approvals"); }, null, "sov:approvals"));
    body.appendChild(ap);
    var dl = el("dl", "kv sov-kv");
    var chain = h.chain_ok === true ? tag("verified is-strong", "YES \u00B7 INTACT") : h.chain_ok === false ? tag("miss is-strong", "NO \u00B7 BROKEN") : tag("nosignal", "NO_SIGNAL");
    add(dl, el("dt", null, "Receipt chain intact"), add(el("dd"), chain, el("span", "wx-caption", h.receipts != null ? " " + str(h.receipts) + " receipts" : "")),
      el("dt", null, "Where your data lives"), el("dd", "wx-mono", h.data_dir ? str(h.data_dir) : "NO_SIGNAL \u00B7 aios did not say"),
      el("dt", null, "AI model"), add(el("dd"), lamp(llmText().lamp), el("span", null, " " + llmText().long)),
      el("dt", null, "Session"), add(el("dd"), S.locked ? tag("refused is-strong", "LOCKED") : tag("pass", "OPEN"),
        el("span", "wx-caption", S.locked ? " runs and approvals are refused" : " agents may run inside their tiers")));
    body.appendChild(dl);
    var lockBtn = btn("wx-btn" + (S.locked ? " is-signal" : ""), S.locked ? "Unlock session" : "Lock session", function () { if (S.locked) unlockSession(); else lockSession(); },
      null, "sov:lock");
    lockBtn.setAttribute("aria-pressed", S.locked ? "true" : "false");
    var review = btn("wx-btn", "Review what agents may do", function () { openFromApex("capabilities"); }, null, "sov:caps");
    var rec = btn("wx-btn", "Open receipts", function () { openFromApex("receipts"); }, null, "sov:receipts");
    if (S.locked) { review.disabled = true; rec.disabled = true; }
    add(body, add(el("div", "sov-controls"), lockBtn, review, rec),
      el("p", "wx-caption", S.locked ? "Locked. Unlock to open windows and let agents run again."
        : "Lock session stops everything (cancels running requests, closes every window) and then locks aios: it refuses agent runs and approvals until you unlock."));
  }

  function renderApexStatus() {
    if (!refs.apexStatus) return;
    var h = S.health;
    var bits;
    if (S.link.up === false) bits = ["NO SIGNAL from aios"];
    else if (!h) bits = ["aios " + NOT_READ];
    else bits = ["aios " + str(h.version), "pack " + str(S.st ? S.st.label : h.pack), str(h.receipts) + " receipts",
      h.chain_ok === true ? "chain ok" : h.chain_ok === false ? "chain BROKEN" : "chain NO_SIGNAL", llmText().short, S.locked ? "LOCKED" : "session open"];
    refs.apexStatus.textContent = bits.join(" \u00B7 ");
  }

  /* Tier 0 \u00B7 Search: windows, locations, numbers, agents, approvals, receipts, messages, commands; Ask as the last resort */
  function qoCandidates(q) {
    var items = [];
    var st = S.st || {};
    ORDER.forEach(function (k) {
      items.push({ kind: "window", label: WINDOWS[k].title, alias: k + " " + (WINDOWS[k].short || ""), hint: wins[k] ? "open" : WINDOWS[k].desc, act: function () { openFromApex(k); } });
    });
    (Array.isArray(st.locations) ? st.locations : []).forEach(function (l) {
      items.push({ kind: "location", label: str(l.name) + " " + str(l.id), alias: "location " + str(l.manager), hint: "manager " + str(l.manager), act: function () { openFromApex("today"); } });
    });
    (Array.isArray(st.kpis) ? st.kpis : []).forEach(function (k) {
      var cue = kpiCue(k);
      items.push({ kind: "number", label: locName(k.location) + " \u00B7 " + str(k.label) + " " + fmtValue(k.value, k.unit),
        alias: "kpi " + str(k.key) + " " + str(k.value), hint: "target " + fmtValue(k.target, k.unit) + " \u00B7 " + cue.word.toLowerCase(), act: function () { openFromApex("today"); } });
    });
    S.agents.forEach(function (a) {
      items.push({ kind: "agent", label: "Run " + str(a.name || a.id), alias: str(a.id) + " agent worker", hint: tierTag(a.tier).text, act: function () { apexRun(a.id); } });
    });
    S.proposals.forEach(function (p) {
      items.push({ kind: "approval", label: str(p.summary), alias: "approval approve proposal " + str(p.agent) + " " + str(p.hash).slice(0, 16), hint: "waiting \u00B7 " + hashPrefix(p.hash, 10),
        act: function () { openFromApex("approvals", { focusHash: p.hash }); } });
    });
    (views.apex.receipts || []).slice(0, 30).forEach(function (r) {
      items.push({ kind: "receipt", label: "#" + str(r.seq) + " " + str(r.summary), alias: "receipt " + str(r.kind) + " " + str(r.agent) + " " + str(r.hash).slice(0, 16),
        hint: str(r.kind) + " \u00B7 " + fmtWhen(r.ts), act: function () { openFromApex("receipts"); } });
    });
    (Array.isArray(st.alerts) ? st.alerts : []).forEach(function (al) {
      items.push({ kind: "alert", label: str(al.text), alias: "alert " + str(al.severity) + " " + locName(al.location), hint: str(al.severity) + " \u00B7 " + locName(al.location), act: function () { openFromApex("today"); } });
    });
    (Array.isArray(st.inbox) ? st.inbox : []).forEach(function (m) {
      items.push({ kind: "message", label: str(m.subject), alias: "inbox message " + str(m.from) + " " + str(m.tag), hint: str(m.from), act: function () { openFromApex("inbox"); } });
    });
    items.push({ kind: "cmd", label: "Stop everything", alias: "halt cancel close stop", hint: "cancel every request, close every window", act: stopEverything });
    items.push(S.locked
      ? { kind: "cmd", label: "Unlock session", alias: "unlock resume sovereignty", hint: "let agents run again", act: unlockSession }
      : { kind: "cmd", label: "Lock session", alias: "lock sovereignty", hint: "stop everything, then lock aios", act: lockSession });
    items.push({ kind: "cmd", label: "Review what agents may do", alias: "capabilities permissions sovereignty", hint: "reads, writes, never", act: function () { openFromApex("capabilities"); } });
    items.push({ kind: "cmd", label: "Night desk", alias: "theme dark", hint: "appearance", act: function () { setTheme("dark"); toast("Night desk"); } });
    items.push({ kind: "cmd", label: "Paper desk", alias: "theme light", hint: "appearance", act: function () { setTheme("light"); toast("Paper desk"); } });
    items.push({ kind: "cmd", label: "Follow the system theme", alias: "theme system", hint: "appearance", act: function () { setTheme("system"); toast("Theme follows the system"); } });
    S.packs.forEach(function (p) {
      items.push({ kind: "cmd", label: "Switch pack: " + str(p.label || p.id), alias: "pack " + str(p.id), hint: "tier 1 \u00B7 writes a receipt", act: function () { openFromApex("settings"); switchPack(p.id); } });
    });
    var t = str(q).trim();
    var out = (t ? filterItems(t, items) : items).slice(0, 14);
    if (t) out.push({ kind: "ask", label: "Ask: " + t, alias: "", hint: "opens Ask with this question", act: function () { if (S.locked) { toast("The session is locked. Unlock it first."); return; } closeApex(); openWindow("ask"); ask(t); } });
    return out;
  }

  function refreshResults(force) {
    var q = refs.qo.value;
    if (!q.trim() && force !== true) { qoItems = []; hideResults(); return; }
    qoItems = qoCandidates(q);
    if (qoSel >= qoItems.length) qoSel = 0;
    var ul = clear(refs.results);
    if (!qoItems.length) {
      var none = el("li", "empty", "no match");
      none.setAttribute("role", "option");
      none.setAttribute("aria-disabled", "true");
      ul.appendChild(none);
      showResults(true);
      return;
    }
    qoItems.forEach(function (it, i) {
      var li = el("li");
      li.setAttribute("role", "option");
      li.id = "qo-opt-" + i;
      add(li, el("span", "k wx-tag", it.kind), el("span", "lb", it.label), it.hint ? el("span", "hn wx-caption", it.hint) : null);
      li.addEventListener("mousemove", function () { if (qoSel !== i) { qoSel = i; paintSel(); } });
      li.addEventListener("click", function () { act(it); });
      ul.appendChild(li);
    });
    showResults(true);
    paintSel();
  }

  function paintSel() {
    var lis = refs.results.children;
    for (var i = 0; i < lis.length; i++) lis[i].setAttribute("aria-selected", i === qoSel ? "true" : "false");
    var cur = lis[qoSel];
    if (cur && cur.id) {
      refs.qo.setAttribute("aria-activedescendant", cur.id);
      if (typeof cur.scrollIntoView === "function") cur.scrollIntoView({ block: "nearest" });
    }
  }

  function showResults(on) {
    refs.results.hidden = !on;
    refs.qo.setAttribute("aria-expanded", on ? "true" : "false");
    if (!on) refs.qo.removeAttribute("aria-activedescendant");
  }
  function hideResults() { if (refs.results) showResults(false); }

  function act(it) {
    if (!it) return;
    refs.qo.value = "";
    qoSel = 0;
    hideResults();
    it.act();
  }

  function onQoKey(e) {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (refs.results.hidden) { refreshResults(true); return; }
      if (!qoItems.length) return;
      qoSel = (qoSel + (e.key === "ArrowDown" ? 1 : qoItems.length - 1)) % qoItems.length;
      paintSel();
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (!refs.qo.value.trim()) return;
      if (qoItems.length) act(qoItems[qoSel] || qoItems[0]);
    }
  }

  /* Lock session = Stop everything: cancel every request, close every window, then lock aios */
  function stopAll() {
    var cancelled = 0;
    inflight.forEach(function (c) { c.abort(); cancelled += 1; });
    inflight.clear();
    var kinds = Object.keys(wins);
    kinds.forEach(function (k) { closeWindow(k, true); });
    Object.keys(views).forEach(function (k) {
      var v = views[k];
      Object.keys(v).forEach(function (slot) { if (isObj(v[slot]) && v[slot].status === "loading") v[slot] = { status: "aborted" }; });
    });
    Object.keys(views.apex.runs).forEach(function (id) { if (views.apex.runs[id].status === "loading") views.apex.runs[id] = { status: "aborted" }; });
    if (views.ask.pending) views.ask.pending = null;
    renderDock();
    renderIcons();
    return { windows: kinds.length, cancelled: cancelled };
  }

  // Stop everything: cancel every request and close every window. Nothing is locked; work can start again at once.
  function stopEverything() {
    var done = stopAll();
    var msg = "Stopped. Closed " + done.windows + (done.windows === 1 ? " window" : " windows") + "; cancelled " + done.cancelled + (done.cancelled === 1 ? " request" : " requests") + ". Nothing is running.";
    toast(msg);
    announce(msg);
    if (apexOpen()) renderApex();
  }

  function lockSession() {
    var done = stopAll();
    var stopped = "Closed " + done.windows + (done.windows === 1 ? " window" : " windows") + "; cancelled " + done.cancelled + (done.cancelled === 1 ? " request" : " requests") + ".";
    return api("POST", "/api/lock", {}).then(function (r) {
      var msg;
      if (r.ok) {
        S.lockReceipt = isObj(r.json) && isObj(r.json.receipt) ? r.json.receipt : null;
        S.lockedAt = new Date();
        setLocked(true);
        msg = "Locked. " + stopped + " aios refuses agent runs and approvals until you unlock.";
      } else if (r.nosignal) msg = "Stopped. " + stopped + " NO SIGNAL from aios: the lock was not confirmed.";
      else if (r.aborted) msg = "Stopped. " + stopped;
      else msg = "Stopped. " + stopped + " MISS \u00B7 aios did not lock: " + errText(r);
      toast(msg);
      announce(msg);
      poll();
    });
  }

  function unlockSession() {
    return api("POST", "/api/unlock", {}).then(function (r) {
      var msg;
      if (r.ok) { setLocked(false); msg = "Unlocked. Agents may run inside their tiers again."; }
      else if (r.nosignal) msg = "NO SIGNAL from aios: still locked.";
      else msg = "MISS \u00B7 aios did not unlock: " + errText(r);
      toast(msg);
      announce(msg);
      poll();
    });
  }

  // The lock is aios's state (health.locked, 423 answers); the desktop mirrors it and never assumes it.
  function setLocked(on) {
    on = !!on;
    if (S.locked === on && S.lockDrawn) return;
    S.locked = on;
    S.lockDrawn = true;
    if (on) stopAll();
    renderLock();
    renderDock();
    if (apexOpen()) renderApex();
    Object.keys(wins).forEach(paint);
  }

  function renderLock() {
    var layer = refs.lockLayer;
    if (!layer) return;
    var on = !!S.locked;
    layer.hidden = !on;
    [refs.icons, refs.wins, refs.deskCard].forEach(function (n) { if (n) n.inert = on; });
    clear(layer);
    if (!on) return;
    var card = el("section", "lockcard wx-window is-slab");
    card.setAttribute("aria-labelledby", "lock-title");
    var bar = el("div", "wx-bar");
    add(bar, add(el("h2", "wx-title-text band-title"), el("span", "tier-eyebrow", "Tier 3"), el("span", "band-name", "Sovereignty")));
    var body = el("div", "wx-body");
    var title = el("p", "wx-title", "Session locked");
    title.id = "lock-title";
    var unlock = btn("wx-btn is-signal is-tap", "Unlock session", function () { unlockSession(); }, null, "unlock");
    refs.unlockBtn = unlock;
    add(body, add(el("div", "wx-row"), tag("refused is-strong", "LOCKED"), el("span", "wx-caption", S.lockedAt ? "since " + fmtWhen(S.lockedAt.toISOString()) : "reported by aios")),
      title,
      el("p", "lock-text", "Every window is closed and nothing runs: aios refuses agent runs and approvals until you unlock. Your data stays on this computer."),
      S.lockReceipt ? receiptLine(S.lockReceipt) : null,
      add(el("div", "wx-row"), unlock));
    add(card, bar, body);
    layer.appendChild(card);
    if (!apexOpen()) unlock.focus();
  }

  /* ------------------------- keys, resize, boot ------------------------- */

  function onGlobalKey(e) {
    if (isNarrow()) return;
    var k = e.key;
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && (k === "k" || k === "K" || k === " " || e.code === "Space")) {
      e.preventDefault();
      toggleApex();
      return;
    }
    if (k === "Escape" && apexOpen()) {
      e.preventDefault();
      if (!refs.results.hidden) hideResults(); else closeApex();
      return;
    }
    if (k === "Tab" && apexOpen()) {
      // The Apex is modal: Tab wraps inside it instead of leaving for the browser chrome.
      var f = Array.prototype.filter.call(refs.apex.querySelectorAll("button, input, select, a[href], [tabindex]:not([tabindex='-1'])"),
        function (n) { return !n.disabled && n.offsetParent !== null; });
      if (!f.length) return;
      var first = f[0], last = f[f.length - 1], cur = document.activeElement;
      if (e.shiftKey && (cur === first || !refs.apex.contains(cur))) { e.preventDefault(); last.focus(); }
      else if (!e.shiftKey && (cur === last || !refs.apex.contains(cur))) { e.preventDefault(); first.focus(); }
    }
  }

  function onResize() {
    Object.keys(wins).forEach(function (k) { place(wins[k]); });
  }

  function renderAllChrome() {
    renderDock();
    renderIcons();
    renderDeskCard();
    renderApexStatus();
    if (apexOpen()) renderApex();
  }

  function boot() {
    mountDesk();
    mountDock();
    mountApex();
    startClock();
    document.addEventListener("keydown", onGlobalKey, true);
    window.addEventListener("resize", onResize);
    if (darkMq && darkMq.addEventListener) darkMq.addEventListener("change", function () { renderDockTheme(); });
    document.addEventListener("visibilitychange", function () { if (!document.hidden) poll(); });
    reloadCore(false).then(function () {
      if (!isNarrow() && S.st && !Object.keys(wins).length) openWindow("today", { focusInside: false });
    });
    window.setInterval(poll, POLL_MS);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", boot);
  else boot();
})();
