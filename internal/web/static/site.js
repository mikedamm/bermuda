// WingView flight pages: draws each page's map from the data the
// server embedded next to it, with MapLibre GL JS over WingView's Protomaps
// basemap (a PMTiles archive read by HTTP range requests), and counts the
// title band's "Updated" time up. Everything else on the page works
// without this script.

// Operator logos come from WingView's CDN for any three-letter designator,
// unchecked; one it does not have would show the browser's broken image.
// Hide those, including any that failed before this script ran.
(() => {
	"use strict";
	const logo = "img.op, img.band-logo, img.band-bar-logo";
	const hide = (img) => (img.style.visibility = "hidden");
	document.querySelectorAll(logo).forEach((img) => img.complete && !img.naturalWidth && hide(img));
	document.addEventListener("error", (e) => e.target.matches?.(logo) && hide(e.target), true);
})();

// The airport band's split-flap clock: the airport's local time, each
// changed figure turning over as on an old departures board. The server
// draws the time it rendered; this keeps it turning, on the minute.
(() => {
	"use strict";
	const el = document.querySelector("[data-clock]");
	if (!el) return;
	let fmt;
	try {
		fmt = new Intl.DateTimeFormat("en-US", { timeZone: el.dataset.clock, hour: "2-digit", minute: "2-digit", hour12: true });
	} catch {
		return; // a zone the browser does not know: the rendered time stays
	}
	const flaps = [...el.querySelectorAll(".flap")];
	const said = el.querySelector(".vh");
	const zone = said?.textContent.trim().split(" ").pop() || "";
	const still = matchMedia("(prefers-reduced-motion: reduce)");

	// four figures, HH:MM, and AM or PM; the spoken time drops the leading zero
	const read = () => {
		const p = Object.fromEntries(fmt.formatToParts(new Date()).map((x) => [x.type, x.value]));
		const h = p.hour.padStart(2, "0"), period = (p.dayPeriod || "").toUpperCase();
		return { figures: [h[0], h[1], p.minute[0], p.minute[1], period], text: `${+h}:${p.minute} ${period}` };
	};
	const half = (cls, text) => {
		const s = document.createElement("span");
		s.className = `flap-half ${cls} flap-leaf`;
		s.append(Object.assign(document.createElement("b"), { textContent: text }));
		return s;
	};
	const turn = (flap, to) => {
		const from = flap.dataset.v ?? "";
		if (from === to) return;
		flap.dataset.v = to;
		const top = flap.querySelector(":scope > .flap-top b"), bot = flap.querySelector(":scope > .flap-bot b");
		top.textContent = to; // under the falling leaf
		if (still.matches) {
			bot.textContent = to;
			return;
		}
		flap.querySelectorAll(".flap-leaf").forEach((n) => n.remove()); // a turn still running
		const fall = half("flap-top", from), land = half("flap-bot", to);
		flap.append(fall, land);
		land.addEventListener("animationend", () => {
			bot.textContent = to;
			fall.remove();
			land.remove();
		}, { once: true });
	};
	const tick = () => {
		const now = read();
		flaps.forEach((f, i) => turn(f, now.figures[i] ?? ""));
		if (said) said.textContent = `Local time ${now.text} ${zone}`;
		setTimeout(tick, 60000 - (Date.now() % 60000) + 40); // just past the next minute
	};
	tick();
})();

// The aircraft page's band: it sticks to the top, and when the dock scrolls
// up and touches it the dock folds into it: this marks the band is-docked
// and CSS quickly fades the dock out and condenses the band into a bar (the
// WingView mark, the dock's one-line summary, the codes by the tail), and
// reverses it on the way back down. The band keeps its height in the flow,
// so nothing below it moves.
(() => {
	"use strict";
	const band = document.querySelector("[data-condense]");
	if (!band) return;
	const stage = band.nextElementSibling;
	const dock = stage?.querySelector(".dock");
	const BAR = 56; // the condensed height
	const SLACK = 6; // px past the line before it undocks, so it cannot flicker there
	let height = 0;

	// how much of the band the condensed bar clips away (offsetHeight
	// ignores the clip, so this reads the band as laid out)
	const measure = () => {
		height = band.offsetHeight;
		band.style.setProperty("--cut", Math.max(0, height - BAR) + "px");
	};
	// docked once the dock's top meets the full band's foot; undocked again
	// only once it is back past the line by SLACK
	const update = () => {
		if (!dock) return;
		const foot = band.getBoundingClientRect().top + height;
		// where the dock sits in the flow: its parent is never transformed,
		// and offsetTop ignores the dock's own lift as it folds away
		const top = dock.parentElement.getBoundingClientRect().top + dock.offsetTop;
		const docked = band.classList.contains("is-docked") ? top < foot + SLACK : top <= foot;
		band.classList.toggle("is-docked", docked);
	};
	let frame = 0;
	addEventListener("scroll", () => {
		frame ||= requestAnimationFrame(() => {
			frame = 0;
			update();
		});
	}, { passive: true });
	const again = () => {
		measure();
		update();
	};
	addEventListener("resize", again);
	document.fonts?.ready.then(again); // the band's height in Inter, once it has loaded
	again();
	requestAnimationFrame(() => requestAnimationFrame(() => band.classList.add("cond-ready"))); // animate from here on
})();

// "Updated 12 seconds ago": the band's time counts up from when the page's
// data was read. The server writes the clock time, which stays for readers
// without this script and becomes the tooltip.
(() => {
	"use strict";
	const plural = (n, unit) => n + " " + unit + (n === 1 ? "" : "s") + " ago";
	const ago = (s) =>
		s < 5 ? "just now"
		: s < 60 ? s + " seconds ago"
		: s < 3600 ? plural(Math.floor(s / 60), "minute")
		: s < 86400 ? plural(Math.floor(s / 3600), "hour")
		: plural(Math.floor(s / 86400), "day");
	const tick = () => {
		for (const t of document.querySelectorAll("time[data-ago]")) {
			const at = Date.parse(t.dateTime);
			if (Number.isNaN(at)) continue;
			t.title ||= t.textContent;
			const text = ago(Math.max(0, Math.round((Date.now() - at) / 1000)));
			if (t.textContent !== text) t.textContent = text;
		}
	};
	tick();
	setInterval(tick, 1000);
})();

(() => {
	"use strict";
	const cfg = document.body.dataset;
	const maps = [...document.querySelectorAll("[data-map]")];
	if (!cfg.mapTiles || !maps.length) return;
	// crawlers read the page; the map is for people
	if (/bot|crawl|spider|slurp|facebookexternalhit|embedly|whatsapp|preview/i.test(navigator.userAgent)) return;

	const C = { flown: "#0065ad", ahead: "#229cf8", net: "#0065ad", ring: "#0065ad", navy: "#002f64", past: "#4a5363", pastOld: "#b2b9c3" };
	const FONT = ["Inter SemiBold"]; // Inter, as on the page: glyphs built with its cv11 and ss01 forms
	const PLANE = '<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M12 1.6c.9 0 1.5 1 1.5 2.3v5.3l8 4.6v2.1l-8-2.4v4.6l2.4 1.8v1.7L12 20.6l-3.9 1v-1.7l2.4-1.8v-4.6l-8 2.4v-2.1l8-4.6V3.9c0-1.3.6-2.3 1.5-2.3Z"/></svg>';
	// the same airliner as an icon for the map's own symbol layer: navy with a white edge
	const PLANE_ICON = '<svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 24 24"><path fill="#002f64" stroke="#ffffff" stroke-width="1.6" stroke-linejoin="round" paint-order="stroke" d="M12 1.6c.9 0 1.5 1 1.5 2.3v5.3l8 4.6v2.1l-8-2.4v4.6l2.4 1.8v1.7L12 20.6l-3.9 1v-1.7l2.4-1.8v-4.6l-8 2.4v-2.1l8-4.6V3.9c0-1.3.6-2.3 1.5-2.3Z"/></svg>';

	const css = document.createElement("link");
	css.rel = "stylesheet";
	css.href = cfg.mapCss;
	document.head.append(css);
	const script = (src) => new Promise((ok, fail) => {
		const s = document.createElement("script");
		s.src = src;
		s.onload = ok;
		s.onerror = fail;
		document.head.append(s);
	});

	Promise.all([import(cfg.mapLib), script(cfg.mapPmtiles), fetch(cfg.mapStyle).then((r) => r.json())])
		.then(([ml, , style]) => {
			ml.addProtocol("pmtiles", new pmtiles.Protocol().tile);
			style.sources.protomaps.url = "pmtiles://" + cfg.mapTiles;
			// elevation shading: Terrarium tiles, or a flat map without them
			if (cfg.mapTerrain && style.sources.terrain) style.sources.terrain.tiles = [cfg.mapTerrain];
			else {
				delete style.sources.terrain;
				style.layers = style.layers.filter((l) => l.source !== "terrain");
			}
			style.glyphs = location.origin + cfg.mapGlyphs;
			maps.forEach((el) => draw(ml, style, el));
		})
		.catch((err) => console.warn("map unavailable:", err));

	function draw(ml, style, el) {
		const data = JSON.parse(el.querySelector('script[type="application/json"]').textContent);
		const map = new ml.Map({
			container: el.querySelector(".map-canvas"),
			style,
			// fixed: the server frames each map, so it does not pan or zoom
			// (and never catches the page's scroll on a phone); pins and
			// aircraft still take clicks, they link to their pages
			dragPan: false, scrollZoom: false, boxZoom: false, doubleClickZoom: false,
			touchZoomRotate: false, dragRotate: false, touchPitch: false, keyboard: false,
			fadeDuration: 0,
			maxZoom: 14,
			attributionControl: { compact: true },
			center: data.focus || [-40, 30],
			zoom: data.focus ? data.zoom || 7 : 1.5,
		});
		foldCredit(map, el);

		// the camera frames the route between the docked panel and the right
		// edge of the content column, not the window: on a wide screen the
		// map runs edge to edge but the route stays where the page's content is.
		// The map is the page's fixed background, filling the window, so the
		// page around it is measured as it lies scrolled to the top: the route
		// sits in the window the page opens with, under the title band and
		// above whatever covers the map below (the panels; on phones the dock).
		const padding = () => {
			const stage = el.closest(".stage");
			const dock = stage?.querySelector(".dock");
			const box = el.getBoundingClientRect();
			const y = window.scrollY;
			// the route goes under the title band: in the stage, or above it
			// (aircraft pages, where it sticks), where the stage's top is its foot
			const band = stage?.querySelector(":scope > .band");
			const foot = band ? band.getBoundingClientRect().bottom + y : stage ? stage.getBoundingClientRect().top + y : box.top;
			const under = Math.max(0, foot - box.top);
			const over = dock && matchMedia("(min-width: 900px)").matches;
			const wrap = stage?.querySelector(":scope > .stage-wrap");
			const cover = !over && wrap?.offsetHeight ? wrap : stage?.nextElementSibling;
			const covered = cover ? Math.max(0, box.bottom - (cover.getBoundingClientRect().top + y)) : 0;
			el.style.setProperty("--map-cover", covered + "px");
			if (!over) return { top: under + 52, bottom: covered + 52, left: 52, right: 52 }; // dock sits below: even margins
			const left = Math.min(dock.getBoundingClientRect().right - box.left + 48, el.clientWidth * 0.62);
			const col = dock.parentElement; // .stage-wrap, the content column
			const end = col.getBoundingClientRect().right - parseFloat(getComputedStyle(col).paddingRight);
			// 64 px inside the column leaves room for a pin's label, which sits to its right
			const right = Math.max(64, Math.min(box.right - end + 64, el.clientWidth - left - 240));
			// the panels rise over the map's faded foot: keep the route above it
			return { top: under + 56, bottom: covered + 80, left, right };
		};
		const bounds = (pts) => pts.reduce((b, c) => b.extend(c), new ml.LngLatBounds(pts[0], pts[0]));
		const coords = [];
		(data.lines || []).forEach((l) => coords.push(...l.c));
		(data.pins || []).forEach((p) => coords.push(p.p));
		(data.planes || []).forEach((p) => coords.push(p.p));
		if (data.plane) coords.push(data.plane.p);
		const frame = () => {
			if (data.fit?.length) {
				map.fitBounds(bounds(data.fit), { padding: padding(), duration: 0, maxZoom: 6 });
			} else if (data.focus) {
				map.jumpTo({ center: data.focus, zoom: data.zoom || 7, padding: padding() });
			} else if (coords.length) {
				map.fitBounds(bounds(coords), { padding: padding(), duration: 0, maxZoom: 8.5 });
			}
		};
		frame();
		let t;
		window.addEventListener("resize", () => {
			clearTimeout(t);
			t = setTimeout(frame, 150);
		});

		// the page's own aircraft: an HTML marker the live refresh moves
		let marker = null;
		const placePlane = (p) => {
			if (!p) {
				marker?.remove();
				marker = null;
				return;
			}
			if (!marker) {
				const plane = document.createElement("span");
				plane.className = "mk-plane";
				plane.innerHTML = PLANE;
				marker = new ml.Marker({ element: plane, rotationAlignment: "map", pitchAlignment: "map" }).setLngLat(p.p).addTo(map);
			}
			marker.setLngLat(p.p).setRotation(p.h);
		};
		placePlane(data.plane);

		// live.js sends the flight's new position and route split; the
		// camera stays where the reader left it
		el.addEventListener("wv:live", (e) => {
			data.lines = e.detail.lines;
			data.plane = e.detail.plane;
			placePlane(data.plane);
			if (drawn) map.getSource("wv")?.setData(routes(data));
		});

		// Overlays need only the style, not every basemap tile: draw them as
		// soon as it is ready, so a cold tile cache never hides the flight.
		let drawn = false;
		const overlays = () => {
			if (drawn) return;
			drawn = true;
			map.addSource("wv", { type: "geojson", data: routes(data) });
			const before = map.getStyle().layers.find((l) => l.type === "symbol")?.id;
			const line = (id, k, paint) => map.addLayer({
				id, type: "line", source: "wv", filter: ["==", ["get", "k"], k],
				layout: { "line-cap": "round", "line-join": "round" }, paint,
			}, before);
			line("wv-ring", "ring", { "line-color": C.ring, "line-width": 1.2, "line-opacity": 0.6, "line-dasharray": [3, 3] });
			line("wv-net", "net", {
				"line-color": C.net,
				"line-width": ["interpolate", ["linear"], ["get", "w"], 0, 1, 1, 3],
				"line-opacity": ["interpolate", ["linear"], ["get", "w"], 0, 0.3, 1, 0.9],
			});
			// earlier flights under the current one: gray, darkest the latest
			line("wv-past", "past", {
				"line-color": ["interpolate", ["linear"], ["get", "w"], 1, C.past, 3, C.pastOld], // w: age, 1 the latest
				"line-width": 2.4,
			});
			line("wv-flown", "flown", { "line-color": C.flown, "line-width": 3.2 });
			line("wv-ahead", "ahead", { "line-color": C.ahead, "line-width": 3, "line-dasharray": [0.6, 1.8] });

			// airports: drawn in the map so labels give way to each other,
			// busiest first; each one links to its page
			const main = ["==", ["get", "k"], "main"];
			const size = ["min", 6, ["*", 0.025, ["get", "n"]]];
			map.addSource("wv-pins", {
				type: "geojson",
				data: {
					type: "FeatureCollection",
					features: (data.pins || []).map((p) => ({
						type: "Feature", properties: { l: p.l, k: p.k || "minor", u: p.u || "", n: p.n || 0 }, geometry: { type: "Point", coordinates: p.p },
					})),
				},
			});
			// the page's own airport never gives way; it is drawn last, above traffic
			const subject = ["==", ["get", "k"], "subject"];
			const others = ["!=", ["get", "k"], "subject"];
			map.addLayer({ id: "wv-pin-ring", type: "circle", source: "wv-pins", filter: others, paint: {
				"circle-radius": ["case", main, ["+", 8.5, size], ["+", 5, size]], "circle-color": C.navy,
			} });
			map.addLayer({ id: "wv-pin", type: "circle", source: "wv-pins", filter: others, paint: {
				"circle-radius": ["case", main, ["+", 5, size], ["+", 3.2, size]],
				"circle-color": ["case", main, C.ahead, "#ffffff"],
				"circle-stroke-width": ["case", main, 2, 0], "circle-stroke-color": "#ffffff",
			} });
			map.addLayer({ id: "wv-pin-label", type: "symbol", source: "wv-pins", filter: others, layout: {
				"text-field": ["get", "l"],
				"text-font": FONT,
				"text-size": ["case", main, 13, 11.5],
				"text-variable-anchor": ["left", "right", "top", "bottom"],
				"text-radial-offset": 0.95,
				"text-padding": 4,
				"text-justify": "auto",
				"symbol-sort-key": ["case", main, ["-", -100000, ["get", "n"]], ["*", -1, ["get", "n"]]],
			}, paint: { "text-color": C.navy, "text-halo-color": "#ffffff", "text-halo-width": 1.6 } });
			map.addLayer({ id: "wv-subject-ring", type: "circle", source: "wv-pins", filter: subject, paint: { "circle-radius": 10, "circle-color": C.navy } });
			map.addLayer({ id: "wv-subject", type: "circle", source: "wv-pins", filter: subject, paint: {
				"circle-radius": 6, "circle-color": C.ahead, "circle-stroke-width": 2.5, "circle-stroke-color": "#ffffff",
			} });
			map.addLayer({ id: "wv-subject-label", type: "symbol", source: "wv-pins", filter: subject, layout: {
				"text-field": ["get", "l"], "text-font": FONT, "text-size": 15,
				"text-anchor": "left", "text-offset": [1.1, 0], "text-allow-overlap": true, "text-ignore-placement": true,
			}, paint: { "text-color": C.navy, "text-halo-color": "#ffffff", "text-halo-width": 2 } });

			const link = (id) => {
				map.on("click", id, (e) => { const u = e.features[0]?.properties.u; if (u) location.href = u; });
				map.on("mouseenter", id, () => { map.getCanvas().style.cursor = "pointer"; });
				map.on("mouseleave", id, () => { map.getCanvas().style.cursor = ""; });
			};
			for (const id of ["wv-pin", "wv-pin-label", "wv-subject", "wv-subject-label"]) link(id);

			// other live aircraft (airport traffic, flights in the air now): a
			// symbol layer above the labels, so labels give way to them; each
			// one turns to its heading and links to its flight
			if ((data.planes || []).length) {
				const icon = new Image(48, 48);
				icon.onload = () => {
					if (!map.hasImage("wv-plane")) map.addImage("wv-plane", icon, { pixelRatio: 2 });
					map.addSource("wv-planes", {
						type: "geojson",
						data: {
							type: "FeatureCollection",
							features: data.planes.map((p) => ({ type: "Feature", properties: { h: p.h, l: p.l || "", u: p.u || "" }, geometry: { type: "Point", coordinates: p.p } })),
						},
					});
					map.addLayer({ id: "wv-planes", type: "symbol", source: "wv-planes", layout: {
						"icon-image": "wv-plane", "icon-rotate": ["get", "h"], "icon-rotation-alignment": "map",
						"icon-allow-overlap": true, "icon-ignore-placement": false,
					} }, "wv-subject-ring"); // under the subject airport, above everything else
					link("wv-planes");
				};
				icon.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(PLANE_ICON);
			}
		};
		if (map.isStyleLoaded()) overlays();
		else map.once("style.load", overlays);
	}

	// The credit stays readable until it has been on screen for five seconds
	// (OSM's attribution guideline), then fades over two and folds to the
	// (i) button, the way MapLibre folds it when the map is dragged.
	function foldCredit(map, el) {
		const credit = el.querySelector(".maplibregl-ctrl-attrib");
		if (!credit || !("IntersectionObserver" in window)) return;
		let timer = 0;
		const seen = new IntersectionObserver(([e]) => {
			clearTimeout(timer);
			if (!e.isIntersecting) return; // the clock runs only while it is in view
			timer = setTimeout(() => {
				seen.disconnect();
				if (!credit.classList.contains("maplibregl-compact-show")) return; // already folded
				credit.classList.add("wv-credit-out");
				setTimeout(() => credit.classList.remove("maplibregl-compact-show", "wv-credit-out"), 2000);
			}, 5000);
		}, { threshold: 1 });
		// the credit has text once the basemap source reports its attribution;
		// MapLibre's own listener runs first and opens it
		const start = () => {
			if (!credit.classList.contains("maplibregl-compact-show")) return;
			map.off("sourcedata", start);
			seen.observe(credit);
		};
		map.on("sourcedata", start);
	}

	// the map's lines: routes and range rings
	function routes(data) {
		const features = (data.lines || []).map((l) => ({
			type: "Feature", properties: { k: l.k, w: l.w ?? 1 }, geometry: { type: "LineString", coordinates: l.c },
		}));
		for (const r of data.rings || []) {
			features.push({ type: "Feature", properties: { k: "ring", w: 1 }, geometry: { type: "LineString", coordinates: circle(r.c, r.r) } });
		}
		return { type: "FeatureCollection", features };
	}

	// a ring of `mi` statute miles around [lon, lat]
	function circle([lon, lat], mi, n = 96) {
		const r = Math.PI / 180, d = mi / 3958.8, p1 = lat * r, l1 = lon * r, out = [];
		for (let i = 0; i <= n; i++) {
			const b = (i / n) * 2 * Math.PI;
			const p2 = Math.asin(Math.sin(p1) * Math.cos(d) + Math.cos(p1) * Math.sin(d) * Math.cos(b));
			const l2 = l1 + Math.atan2(Math.sin(b) * Math.sin(d) * Math.cos(p1), Math.cos(d) - Math.sin(p1) * Math.sin(p2));
			out.push([l2 / r, p2 / r]);
		}
		return out;
	}
})();
