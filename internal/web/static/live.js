// WingView flight pages: keeps a flight that is under way current while the
// page is open. Every 30 seconds it asks the server for the parts of the
// page that change (status, dock, times) and the map's moving pieces, and
// swaps them in. It pauses while the tab is hidden, backs off when the
// server struggles, and stops once the flight is over or its signed URL
// lapses (a reload starts over).
(() => {
	"use strict";
	let url = document.body.dataset.live;
	if (!url || /bot|crawl|spider|slurp|preview/i.test(navigator.userAgent)) return;

	const EVERY = 30e3, MAX_WAIT = 5 * 60e3;
	const parts = {}, shown = {};
	for (const el of document.querySelectorAll("[data-part]")) parts[el.dataset.part] = el;
	const map = document.querySelector("[data-map]");
	let timer = 0, wait = EVERY, busy = false, stopped = false;
	let last = Date.now();

	const schedule = (ms) => {
		clearTimeout(timer);
		timer = stopped || document.hidden ? 0 : setTimeout(poll, Math.max(1000, ms));
	};

	async function poll() {
		timer = 0;
		busy = true;
		try {
			const r = await fetch(url, { headers: { accept: "application/json" }, credentials: "omit" });
			if (r.status === 403) {
				stopped = true;
				return;
			}
			if (!r.ok) throw new Error("live refresh: HTTP " + r.status);
			const d = await r.json();
			for (const [k, html] of Object.entries(d.parts || {})) {
				const el = parts[k];
				// leave alone a part that has not changed or that holds keyboard focus
				if (!el || shown[k] === html || el.contains(document.activeElement)) continue;
				el.innerHTML = shown[k] = html;
			}
			if (d.map && map) map.dispatchEvent(new CustomEvent("wv:live", { detail: d.map }));
			if (d.next) url = d.next;
			stopped = !!d.done;
			last = Date.now();
			wait = EVERY;
		} catch (err) {
			wait = Math.min(wait * 2, MAX_WAIT);
		} finally {
			busy = false;
		}
		schedule(wait);
	}

	document.addEventListener("visibilitychange", () => {
		if (document.hidden) {
			clearTimeout(timer);
			timer = 0;
		} else if (!timer && !busy) {
			schedule(EVERY - (Date.now() - last));
		}
	});
	schedule(EVERY - (Date.now() - last));
})();
