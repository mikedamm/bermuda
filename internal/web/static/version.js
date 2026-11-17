// WingView pages: reload when a new version of the site is deployed, so an
// open page never runs old markup and scripts against the new server. While
// the tab is visible it asks which build the server runs every 30 seconds,
// and once more when the tab comes back.
(() => {
	"use strict";
	const mine = document.body.dataset.version;
	if (!mine || /bot|crawl|spider|slurp|preview/i.test(navigator.userAgent)) return;

	const EVERY = 30e3, KEY = "wv-reloaded";
	let timer = 0;

	// a reload must not throw away what someone is typing
	const typing = () => document.activeElement?.matches("input, textarea, select");

	// During a rolling deploy the reload can still reach an old instance and
	// come back with the old page: reload for one version once a minute at
	// most, so a mixed fleet can never loop the page.
	const mayReload = (v) => {
		let last = null;
		try { last = JSON.parse(sessionStorage.getItem(KEY)); } catch {}
		if (last && last.v === v && Date.now() - last.at < 60e3) return false;
		try { sessionStorage.setItem(KEY, JSON.stringify({ v, at: Date.now() })); } catch {}
		return true;
	};

	async function check() {
		clearTimeout(timer);
		timer = 0;
		if (document.hidden) return;
		try {
			const r = await fetch("/tracker/version", { cache: "no-store", credentials: "omit" });
			const v = r.ok ? (await r.text()).trim() : "";
			if (v && v !== mine && !typing() && mayReload(v)) {
				location.reload();
				return;
			}
		} catch {}
		if (!document.hidden) timer = setTimeout(check, EVERY);
	}

	document.addEventListener("visibilitychange", () => {
		if (document.hidden) {
			clearTimeout(timer);
			timer = 0;
		} else if (!timer) {
			check();
		}
	});
	timer = setTimeout(check, EVERY);
})();
