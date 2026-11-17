# Third-party files served by the map pages

| Path | Source | License |
|---|---|---|
| `maplibre-gl-6.16.0/` | MapLibre GL JS 6.16.0, npm `maplibre-gl` (dist build, unmodified) | BSD-3-Clause, see `LICENSE.txt` |
| `pmtiles-4.5.0/pmtiles.js` | PMTiles 4.5.0, npm `pmtiles` (UMD build, unmodified) | BSD-3-Clause, Protomaps LLC |
| `glyphs/` | Inter 4.1 (rsms/inter) Regular, Medium, SemiBold and Italic as MapLibre SDF glyph ranges (Latin, Greek, Cyrillic), built with stadiamaps `build_pbf_glyphs` after baking the `ss01` and `cv11` features the page uses into each font's character map | SIL Open Font License 1.1 (Inter) |
| `../map-style.json` | Generated from npm `@protomaps/basemaps` 5.7.2 ("light" flavor cut to the layers that draw at zoom 0–9, recoloured as WingView's warm atlas, Inter labels, cities and a hillshade layer added) | BSD-3-Clause, Protomaps LLC |

Map data: Protomaps Basemap v4 from OpenStreetMap (ODbL) and Natural Earth; elevation from Mapterhorn (open terrain data, sources listed at https://mapterhorn.com/attribution). Both are attributed on every map.
