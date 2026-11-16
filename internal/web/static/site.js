// WingView flight pages: draws each page's interactive map from the data the
// server embedded next to it, with MapLibre GL JS over WingView's Protomaps
// basemap (a PMTiles archive read by HTTP range requests). Everything else
// on the page works without this script.
(() => {
	"use strict";
	const cfg = document.body.dataset;
	const maps = [...document.querySelectorAll("[data-map]")];
	if (!cfg.mapTiles || !maps.length) return;
	// crawlers read the page; the map is for people
	if (/bot|crawl|spider|slurp|facebookexternalhit|embedly|whatsapp|preview/i.test(navigator.userAgent)) return;

	const C = { flown: "#0065ad", ahead: "#229cf8", net: "#0065ad", ring: "#0065ad", navy: "#002f64" };
	const FONT = ["Noto Sans Medium"];
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
			style.glyphs = location.origin + cfg.mapGlyphs;
			maps.forEach((el) => draw(ml, style, el));
		})
		.catch((err) => console.warn("map unavailable:", err));

	function draw(ml, style, el) {
		const data = JSON.parse(el.querySelector('script[type="application/json"]').textContent);
		const map = new ml.Map({
			container: el.querySelector(".map-canvas"),
			style,
			cooperativeGestures: true,
			fadeDuration: 0,
			maxZoom: 14,
			attributionControl: { compact: true },
			center: data.focus || [-40, 30],
			zoom: data.focus ? data.zoom || 7 : 1.5,
		});
		map.addControl(new ml.NavigationControl({ showCompass: false }), "top-right");

		// the camera keeps clear of the docked panel when it sits over the map
		const padding = () => {
			const dock = el.closest(".stage")?.querySelector(".dock");
			const over = dock && matchMedia("(min-width: 900px)").matches;
			if (!over) return { top: 52, bottom: 52, left: 52, right: 52 }; // dock sits below: even margins
			const left = Math.min(dock.getBoundingClientRect().right - el.getBoundingClientRect().left + 48, el.clientWidth * 0.62);
			return { top: 56, bottom: 56, left, right: 64 };
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

		if (data.plane) {
			const plane = document.createElement("span");
			plane.className = "mk-plane";
			plane.innerHTML = PLANE;
			new ml.Marker({ element: plane, rotation: data.plane.h, rotationAlignment: "map", pitchAlignment: "map" })
				.setLngLat(data.plane.p).addTo(map);
		}

		// Overlays need only the style, not every basemap tile: draw them as
		// soon as it is ready, so a cold tile cache never hides the flight.
		let drawn = false;
		const overlays = () => {
			if (drawn) return;
			drawn = true;
			const features = (data.lines || []).map((l) => ({
				type: "Feature", properties: { k: l.k, w: l.w ?? 1 }, geometry: { type: "LineString", coordinates: l.c },
			}));
			for (const r of data.rings || []) {
				features.push({ type: "Feature", properties: { k: "ring", w: 1 }, geometry: { type: "LineString", coordinates: circle(r.c, r.r) } });
			}
			map.addSource("wv", { type: "geojson", data: { type: "FeatureCollection", features } });
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
