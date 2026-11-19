// WingView live pages: keeps a flight that is under way, or an airport's
// arrivals and departures boards, current while the page is open. Every 30
// seconds it asks the server for the parts of the page that change (a
// flight's status, dock and times and the map's moving pieces; an airport's
// two boards) and swaps them in. It pauses while the tab is hidden, backs off when the
// server struggles, and stops once the flight is over or its signed URL
// lapses (a reload starts over). An airport's boards never finish.
//
// An airport's boards can also be paged (Earlier / Later flights) and
// searched. Each board's view (pages each way, search) goes with every
// request, so the server draws the board as the reader has it, from the
// whole day's flights, and the 30-second refresh keeps it that way. A
// search filters the rows already shown as the reader types, then the
// server's answer brings every match of the day.
(() => {
	"use strict";
	let url = document.body.dataset.live;
	if (!url || /bot|crawl|spider|slurp|preview/i.test(navigator.userAgent)) return;

	const EVERY = 30e3, MAX_WAIT = 5 * 60e3, MAX_PAGES = 50, TYPING = 250;
	const parts = {}, shown = {};
	for (const el of document.querySelectorAll("[data-part]")) parts[el.dataset.part] = el;
	const map = document.querySelector("[data-map]");
	let timer = 0, wait = EVERY, stopped = false, ctl = null;
	let last = Date.now();

	// the boards' views, by part, and their keys in the request (live.go,
	// boardViews)
	const boards = { arrivals: "a", departures: "d" };
	const views = {};

	const query = () => {
		const q = new URLSearchParams();
		for (const [part, v] of Object.entries(views)) {
			const k = boards[part];
			if (v.e) q.set(k + "e", v.e);
			if (v.l) q.set(k + "l", v.l);
			if (v.q) q.set(k + "q", v.q);
		}
		const s = q.toString();
		return s ? "?" + s : "";
	};

	const terms = (s) => s.toLowerCase().split(/\s+/).filter(Boolean);

	// narrow the rows a board shows to a search while the server answers
	const filter = (part) => {
		const box = document.querySelector(`[data-search="${part}"]`), el = parts[part];
		if (!box || !el) return;
		const t = terms(box.value);
		for (const tr of el.querySelectorAll("tbody tr")) {
			const text = tr.dataset.q || "";
			tr.hidden = t.length > 0 && !t.every((w) => text.includes(w));
		}
	};

	const schedule = (ms) => {
		clearTimeout(timer);
		timer = stopped || document.hidden ? 0 : setTimeout(poll, Math.max(1000, ms));
	};

	// poll asks for the page's changing parts now. A request still out is
	// dropped for it, so an answer never brings back a view the reader has
	// moved on from; said names a board whose search was just answered.
	async function poll(said) {
		clearTimeout(timer);
		timer = 0;
		if (ctl) ctl.abort();
		const c = (ctl = new AbortController());
		try {
			const r = await fetch(url + query(), { headers: { accept: "application/json" }, credentials: "omit", signal: c.signal });
			if (r.status === 403) {
				stopped = true;
				return;
			}
			if (!r.ok) throw new Error("live refresh: HTTP " + r.status);
			const d = await r.json();
			if (c !== ctl) return;
			for (const [k, html] of Object.entries(d.parts || {})) {
				const el = parts[k];
				if (!el || shown[k] === html) continue;
				// leave alone a part where the reader is on a link; one on
				// Earlier or Later gets the same button back in the new board
				const at = document.activeElement, more = el.contains(at) && at.dataset.more;
				if (el.contains(at) && !more) continue;
				el.innerHTML = shown[k] = html;
				if (k in boards) filter(k);
				if (more) (el.querySelector(`[data-more="${more}"]`) || el.querySelector("[data-more]"))?.focus();
			}
			if (said) {
				const out = document.querySelector(`[data-said="${said}"]`), found = parts[said]?.querySelector(".board-found");
				if (out) out.textContent = found ? found.textContent : "";
			}
			if (d.map && map) map.dispatchEvent(new CustomEvent("wv:live", { detail: d.map }));
			if (d.next) url = d.next;
			stopped = !!d.done;
			last = Date.now();
			wait = EVERY;
		} catch (err) {
			if (c !== ctl) return; // a newer request took over
			for (const b of document.querySelectorAll('[data-more][aria-busy="true"]')) b.removeAttribute("aria-busy");
			wait = Math.min(wait * 2, MAX_WAIT);
		} finally {
			if (c === ctl) {
				ctl = null;
				schedule(wait);
			}
		}
	}

	for (const part of Object.keys(boards)) {
		const el = parts[part];
		if (!el) continue;
		const v = (views[part] = { e: 0, l: 0, q: "" });
		el.addEventListener("click", (e) => {
			const b = e.target.closest("[data-more]");
			if (!b || b.getAttribute("aria-busy")) return;
			if (b.dataset.more === "earlier") v.e = Math.min(v.e + 1, MAX_PAGES);
			else v.l = Math.min(v.l + 1, MAX_PAGES);
			b.setAttribute("aria-busy", "true");
			poll();
		});
		const box = document.querySelector(`[data-search="${part}"]`);
		if (!box) continue;
		let typing = 0;
		const search = () => {
			filter(part);
			clearTimeout(typing);
			typing = setTimeout(() => {
				const q = terms(box.value).join(" ");
				if (q === v.q) return;
				// a new search starts around now
				v.q = q;
				v.e = v.l = 0;
				poll(part);
			}, TYPING);
		};
		box.addEventListener("input", search);
		box.addEventListener("keydown", (e) => {
			if (e.key === "Escape" && box.value) {
				box.value = "";
				search();
			}
		});
		if (box.value) search(); // typed before this ran, or kept by the browser
	}

	document.addEventListener("visibilitychange", () => {
		if (document.hidden) {
			clearTimeout(timer);
			timer = 0;
		} else if (!timer && !ctl) {
			schedule(EVERY - (Date.now() - last));
		}
	});
	schedule(EVERY - (Date.now() - last));
})();
