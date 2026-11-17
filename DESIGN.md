---
name: WingView Flight Pages
description: Server-rendered flight, aircraft, airport and airline pages in the flight-tracker convention, in WingView navy, sky and Inter.
colors:
  navy: "#002f64"
  navy-deep: "#00244d"
  navy-hover: "#0a3d7a"
  sky: "#229cf8"
  sky-hover: "#58b5fc"
  sky-edge: "#0065ad"
  link: "#0065ad"
  link-hover: "#004d87"
  paper: "#f1f4f8"
  surface: "#ffffff"
  ink: "#17305a"
  muted: "#53627a"
  head: "#e7eef6"
  zebra: "#f6f8fb"
  hover: "#eaf3fd"
  line: "#d6dde7"
  line-soft: "#e6ebf2"
  line-strong: "#b7c5d6"
  band-text: "#b9cce4"
  band-meta: "#9db4d1"
  grid-head-text: "#dce8f6"
  ok: "#13794a"
  late: "#a24f00"
  bad: "#c0342a"
  idle: "#53627a"
  ocean: "#c0dbe5"
  land: "#faf4e9"
  route-ahead-dash: "#86c4f8"
typography:
  display:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "28px"
    fontWeight: 700
    lineHeight: 1.15
    letterSpacing: "-0.015em"
  headline:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "32px"
    fontWeight: 700
    lineHeight: 1
    letterSpacing: "-0.02em"
    fontFeature: "\"tnum\""
  title:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "17px"
    fontWeight: 700
    lineHeight: 1.3
    letterSpacing: "-0.005em"
  title-panel:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "15px"
    fontWeight: 700
    lineHeight: 1.5
  reading:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "19px"
    fontWeight: 700
    lineHeight: 1.3
    fontFeature: "\"tnum\""
  body:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
    fontFeature: "\"cv11\", \"ss01\""
  body-table:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "13.5px"
    fontWeight: 400
    lineHeight: 1.5
    fontFeature: "\"tnum\""
  label:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.5
rounded:
  tag: "3px"
  sm: "4px"
  full: "50%"
spacing:
  hair: "2px"
  row: "8px"
  cell: "14px"
  dock: "16px"
  gutter: "20px"
  section: "24px"
components:
  button-app:
    backgroundColor: "{colors.sky}"
    textColor: "{colors.navy}"
    rounded: "{rounded.sm}"
    padding: "0 16px"
    height: "38px"
  button-app-hover:
    backgroundColor: "{colors.sky-hover}"
    textColor: "{colors.navy}"
  button-primary:
    backgroundColor: "{colors.navy}"
    textColor: "{colors.surface}"
    rounded: "{rounded.sm}"
    padding: "0 16px"
    height: "38px"
  button-primary-hover:
    backgroundColor: "{colors.navy-hover}"
    textColor: "{colors.surface}"
  input-search:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "0 42px 0 12px"
    height: "40px"
  masthead:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.navy}"
    height: "64px"
  title-band:
    backgroundColor: "{colors.navy}"
    textColor: "{colors.surface}"
    typography: "{typography.display}"
    padding: "18px 0 16px"
  dock:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.sm}"
    padding: "{spacing.dock}"
    width: "360px"
  status-ok:
    backgroundColor: "{colors.ok}"
    textColor: "{colors.surface}"
    typography: "{typography.title}"
    padding: "11px 16px"
  status-late:
    backgroundColor: "{colors.late}"
    textColor: "{colors.surface}"
  status-bad:
    backgroundColor: "{colors.bad}"
    textColor: "{colors.surface}"
  status-live:
    backgroundColor: "{colors.link}"
    textColor: "{colors.surface}"
  status-idle:
    backgroundColor: "{colors.idle}"
    textColor: "{colors.surface}"
  panel:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.sm}"
  panel-header:
    backgroundColor: "{colors.head}"
    textColor: "{colors.navy}"
    typography: "{typography.title-panel}"
    padding: "10px 14px"
  grid-header:
    backgroundColor: "{colors.navy}"
    textColor: "{colors.grid-head-text}"
    padding: "7px 14px"
  grid-cell:
    textColor: "{colors.ink}"
    typography: "{typography.body-table}"
    padding: "8px 14px"
  route-code:
    textColor: "{colors.navy}"
    typography: "{typography.headline}"
---

# Design System: WingView Flight Pages

## Overview

**Creative North Star: "The Tracker Desk"**

The pages play the flight-tracker category straight, in the convention FlightAware and Flightradar24 set: a live map leads, a white panel docked over it answers status at a glance, and dense ruled tables carry the depth underneath. Nothing is staged for effect. The page is an instrument a traveler reads in five seconds on a phone and an enthusiast reads for five minutes on a desktop, and both read the same surfaces.

The identity is WingView's own, bound by the brand: the window-and-wing logo, navy (#002f64) as the structural colour (title band, table headers, codes and times), sky (#229cf8) as the live and forward colour (route ahead, the subject airport, the app button), and Inter at UI sizes with tabular numerals. The ground is a cool paper grey; panels are white with hairline rules and small 4px corners. State is carried by solid, saturated status bands, never by decoration.

Density is high and deliberate. Type stays between 12px and 32px; the largest thing on a page is a three-letter airport code. The map is the only image, and it is tinted into the palette rather than left in the basemap's defaults.

**Key Characteristics:**
- White masthead, navy title band, full-bleed map stage, paper ground below.
- One docked flight panel with a status band welded to its top edge.
- Panels with pale header bars; data grids with navy column headers and zebra rows.
- Every number tabular; every time carries its local zone in small muted type.
- Flat surfaces; the single real shadow is the dock floating over the map.

## Colors

A two-blue brand on cool neutrals, with three solid state colours that do all the signalling.

### Primary
- **WingView Navy** (`navy`): the structural colour. Title band, data-grid column headers, airport codes, headline times, masthead nav text, the plane glyph, pin rings. Deepened to `navy-deep` for the footer and lifted to `navy-hover` on the primary button.

### Secondary
- **Sky** (`sky`): live and forward. The route still ahead on the map, the subject airport's fill, the main airport pins, focus rings, and the app button (always with navy text and a `sky-edge` border, as in the app itself). `sky-hover` is its hover.
- **Deep Sky / Link Blue** (`link`, same value as `sky-edge`): text links, the route already flown, the progress fill on the route track, bar-chart fills, the airport-traffic network lines, range rings, and the live status band. `link-hover` darkens it for hovered links.

### Tertiary (state)
- **Landed Green** (`ok`): on time, arrived, landed without delay.
- **Delay Amber** (`late`): late departure or late arrival.
- **Alert Red** (`bad`): cancelled, diverted.
- **Idle Slate** (`idle`): scheduled with no delay, no current data. Shares its value with `muted`.

### Neutral
- **Paper** (`paper`): page ground behind panels; also the map fog and the static map's space colour.
- **Surface** (`surface`): masthead, dock, panels, breadcrumb bar.
- **Ink** (`ink`): body text and table values. A navy-tinted near-black, never pure black.
- **Muted** (`muted`): labels, city names, time-zone suffixes, counts, notes, dashes for missing values.
- **Head** (`head`): panel header bars. **Zebra** (`zebra`): even grid rows. **Hover** (`hover`): hovered grid rows on pointer devices.
- **Rules** (`line`, `line-soft`, `line-strong`): `line` for panel borders and header-bar bottoms, `line-soft` for row dividers inside panels, `line-strong` for input borders.
- **On-navy text** (`band-text`, `band-meta`, `grid-head-text`): the softened whites used for secondary text on the title band, footer and grid headers.
- **Map** (`ocean`, `land`): the basemap's water and land, set in the generated MapLibre style (`static/map-style.json`) and on the server-drawn fallback map.

### Named Rules
**The Two Blues Rule.** Navy is structure, sky is motion. Navy never marks the live route or the subject airport; sky never fills a header or carries body text. Text-weight blue is always `link`, because sky on white is too light to read.

**The Solid State Rule.** Status is a filled band or a coloured word, in exactly the five state colours. A green band never sits beside a late delay: a late departure outranks an on-time arrival estimate.

## Typography

**Display Font:** Inter (with -apple-system, BlinkMacSystemFont, Segoe UI, sans-serif)
**Body Font:** Inter, same stack
**Label/Mono Font:** none; Inter with tabular numerals does the instrument work

**Character:** One family at UI sizes, set tight and bold for codes and times, regular for reading. Self-hosted variable Inter (400 to 800), with the `cv11` and `ss01` alternates on body text.

### Hierarchy
- **Display** (700, 28px, 1.15, -0.015em): the page name in the title band (flight number, registration, airport). 24px under 900px.
- **Headline** (700, 32px, 1, -0.02em): airport codes in the route block. 28px under 640px.
- **Title** (700, 17px): the status band's state word, and the search dock's label.
- **Title, panel** (700, 15px): panel header bars, in navy.
- **Reading** (700, 19px): the two times that matter in the dock (left the gate, landed or arriving).
- **Body** (400, 15px, 1.5): running text; prose capped at 70ch at 14.5px with 1.6 leading.
- **Body, table** (400, 13.5px): grid cells; links and row headers in 600.
- **Label** (400, 12px, muted): telemetry and route labels, grid column headers (600, on navy), time-zone suffixes at 11.5 to 12px.

### Named Rules
**The Tabular Rule.** Every time, count, distance, altitude and speed is set with tabular numerals and right-aligned in grids, so columns of figures line up.

**The Small Zone Rule.** A clock time is followed by its airport-local zone in smaller, muted type (`8:55 PM AST`). Times are never shown without their zone.

## Layout

A single content column, max 1240px with a 20px gutter (16px under 900px), set between full-bleed bands.

- **Shell, top to bottom:** white masthead (64px: logo at 140px, search up to 460px, nav and the app button pushed right), navy title band (name, airline or owner line, codes; status label and update time aligned to the bottom right), map stage, paper content area, white breadcrumb bar, deep-navy footer.
- **Stage:** the map fills the stage absolutely (min 560px); the dock sits in normal flow inside the content column, 20px from the top, so the stage grows with the dock and nothing in it is ever clipped or scrolled. 360px wide for flights, 400px for search. On flight and aircraft pages the dock carries only what a glance needs (status, route, times, distance flown and to go, altitude, ground speed); the aircraft, heading, squawk and position age sit in a side panel that refreshes with it. The aircraft's 3D model from the app heads the dock, full width and 16:9 on the dock's surface, above the status band: a plain iframe of the app's embed (it draws nothing when the app has no model for the aircraft). On flight pages it sits outside the part live refresh rewrites, so it never reloads. The map is fixed: no pan, zoom or controls, framed by the server between the dock and the right edge of the content column; pins and aircraft link to their pages. At 900px and up the panels below rise 56px into the stage, and the map's last 176px fade into the paper behind them (no hairline under the stage); the map credit sits above the overlap.
- **Below the stage:** a main column plus a 340px side column at 1000px and up; 20px gaps between panels; 24px above (at 900px and up, 56px into the stage's fade instead), 48px below.
- **Under 900px:** the masthead wraps and the search takes a full row; secondary nav links hide, the app button stays (34px). The map becomes a 320px block in flow and the dock drops below it, full width, square-cornered, no shadow, divided by a bottom rule.
- **Under 640px:** panels go edge to edge (no side borders, no radius). Data grids drop their header row and reflow each row into a small labelled grid: four columns, or three for five-field rows; only times, durations, altitude, distance and last-seen get an 11px muted caption, since date, aircraft, route and status read on their own. The scheduled-versus-actual table keeps its table form at 12.5px.
- **Rhythm:** 8px row padding, 14px cell and panel insets, 16px dock inset, 20px gutters and gaps, 24px section breaks.

## Elevation & Depth

Flat by default, ruled with hairlines. Depth appears only where something genuinely floats over the map.

### Shadow Vocabulary
- **Dock float** (`box-shadow: 0 10px 30px rgb(16 31 51 / 0.22), 0 1px 3px rgb(16 31 51 / 0.16)`): the flight panel over the map. Removed when the dock moves below the map.
- **Marker halo** (`drop-shadow(0 0 1.5px #fff) drop-shadow(0 1px 2px rgb(0 47 100 / 0.4))`): plane glyphs over the basemap, for legibility only.

### Named Rules
**The One Float Rule.** Only the dock casts a shadow. Panels below the stage are flat with a 1px `line` border.

## Shapes

Small, square-ish corners and hairline rules. Panels, the dock, buttons and inputs take 4px; the title-band status label and map-pin labels take 3px; pins and the route-track plane disc are round. Under 640px panels lose their corners and side borders; under 900px the dock does too. Lines are 1px everywhere; the only heavier strokes are map routes and the 2px sky underline on the current nav item.

## Components

### Buttons
Compact and solid, 600 weight at 14px, 38px tall, 4px corners.
- **App button:** sky fill, navy text, `sky-edge` border; lighter sky on hover. Used for "Open WingView 3D" and "Follow this flight live" in the masthead, and full-width (42px) at the foot of the airport dock.
- **Primary:** navy fill, white text; `navy-hover` on hover. Used for search submit (44px beside a 44px input).
- **Focus:** 2px sky outline, 2px offset, site-wide.

### Inputs / Fields
- **Style:** white, 1px `line-strong` border, 4px corners, 40px (masthead) or 44px at 16px type (search dock).
- **Focus:** 2px sky outline inset by 1px, border to `sky-edge`.

### Navigation
- **Masthead:** navy 600 links at 14px, 22px apart; the current page carries a 2px sky underline. Under 900px only the app button remains.
- **Breadcrumbs:** 13px muted links with slash separators on a white bar.
- **Footer:** deep navy, `band-text` copy, white links.

### Cards / Containers (panels)
- **Corner Style:** 4px. **Background:** surface. **Border:** 1px `line`. **Shadow:** none.
- **Header bar:** `head` fill, 15px 700 navy title, 10px 14px padding, an optional muted count right-aligned on the same baseline.
- **Body:** 12px 14px; notes 12.5px muted under a `line-soft` rule.

### Status Band (signature)
The one-line answer welded to the top edge of the dock: white text on a solid state fill, 11px 16px padding, the state word at 17px 700 and the detail at 600, spread to opposite ends. States: `ok` (arrived or landed on time), `late` (late departure or arrival, including a late departure on an otherwise on-time estimate), `bad` (diverted, cancelled), `live` (en route or taxiing with no delay known, in link blue), `idle` (scheduled, or no current data). The title band repeats the state as a 13px label chip with 3px corners.

### Route Block
Departure and arrival codes in 32px navy at either end, city names in 13px muted below. Between them a 2px track: dashed `route-ahead-dash` for the whole leg, solid `link` for the flown share, with a navy plane on a white disc at the progress point. Below a `line-soft` rule, the two times that matter at 19px navy with small zones, each with a 13px delay word in `ok` or `late`. A centred muted note gives distance flown and to go.

### Telemetry Rows
Two-column definition grid (altitude, ground speed, heading, squawk): 12px muted label over a 15px 600 tabular value, each cell topped by a `line-soft` rule. A muted right-aligned line gives the position's age. Missing readings show an en dash. Single label/value rows (aircraft, registration) are flex rows: muted label left, 600 value right.

### Data Grids
Full-width collapsed tables at 13.5px: navy header row with 12px 600 `grid-head-text`, 8px 14px cells divided by `line-soft`, zebra even rows, `hover` rows on pointer devices. Links and row headers are 600. Numeric columns right-aligned. Status words are coloured tags (12.5px 600): landed in `ok`, in the air or taxiing in `link`, alerts in `bad`, the rest muted. The scheduled-versus-actual table groups rows under white sub-headers in 13px 700 navy. Bar columns use a 6px `line-soft` track with a `link` fill.

### About Panel
The side column's first panel on flight, airport and aircraft pages. One plain sentence that says what the thing is (a flight's route and how often it flies; an airport's place; an aircraft's year, model and operator), then a key-value list of facts the rest of the page does not show. Live status belongs to the dock, codes and type to the title band, a flying record to its own panel, so none of it is repeated here. A fact's qualifier ("on 14 of 27 flights") sits under its value in 12.5px muted, never squeezed into it. Values are written for people: "PST · UTC−8", not a zone database name; "47.4479° N, 122.3103° W", not signed decimals; "Twin jet", not "2 jet". On airport pages the dock is live traffic only and the reference facts (elevation, coordinates, time zone) live here.

### Map
MapLibre GL JS over WingView's Protomaps v4 basemap (`i.wvcdn.com/v4.pmtiles`), styled by `static/map-style.json`. It started from `@protomaps/basemaps` light and is cut down to what draws at the zooms the pages use (0 to about 9): no streets, buildings, street labels, tunnels, bridges, rail or small landuse, 20 layers in all. It is a consumer map, not a chart: quiet, warm and legible under the routes.
- **Palette:** a warm atlas, the complement of the page's navy and sky. Land `land` (a warm stone), water `ocean` (a quiet grey-teal, a different hue from the sky route ahead), low-zoom vegetation in olive-to-sage tints of the same warm family, highways one soft warm line (#e8d6b6), major roads fainter from zoom 7, country borders solid warm taupe (#b9a796), state borders lighter and dashed. Airports alone are cool (#e6eaf2) with slate runways from zoom 7, since they are the site's subject. The brand blues stay the only saturated colour on the map: routes, pins and aircraft.
- **Relief:** Mapterhorn elevation (`TERRAIN`), shaded with MapLibre's `igor` method in a warm brown (#7a6652), under the water so seas and lakes stay flat. Strongest at world zoom (0.75), where it picks out the ranges, and quiet at airport zoom (0.22), under the traffic. Elevation is read only to zoom 6 and stretched beyond it, which keeps a page to a few tiles.
- **Labels:** Inter, as on the page, with the page's `cv11` and `ss01` forms baked into the glyphs; English names only. Countries in Medium capitals, letter-spaced 0.12em, warm grey (#6b5c4f); states lighter (#9c8b7c), abbreviated below zoom 6; cities from zoom 3 in Regular, sparse and ranked by population, quieter than the airport codes; seas and lakes in letter-spaced Italic grey-teal (#5b8595). Pin and aircraft labels are Inter SemiBold.
- **Route flown:** solid `link`, 3.2px, along the trail the aircraft actually flew. **Route ahead:** sky, 3px, short dashes, along the rest of its filed route from the aircraft onward. Great circles stand in when the trail or plan is missing, when the aircraft is far off its route, or when it has diverted. A route across the antimeridian stays in one copy of the world, with its airports and aircraft beside it.
- **Airport pins:** a navy ring around a white dot, sized by traffic; main airports get a sky dot with a white stroke and larger 13px labels. Labels are navy with a white halo and give way to each other, busiest first.
- **Subject airport:** navy ring, sky dot, white stroke, 15px label that never gives way, drawn above traffic.
- **Planes:** navy top-down airliner glyph with a white edge, rotated to heading; the subject flight larger (30px) than other traffic (22px).
- **Range rings and traffic network:** `link` at reduced opacity, rings dashed at 1.2px, network lines weighted by traffic.

## Do's and Don'ts

### Do:
- **Do** keep navy for structure (band, column headers, codes) and sky for what is live or ahead.
- **Do** answer state with a solid status band in one of the five state colours, welded to the top of the flight panel.
- **Do** give every panel a `head` header bar and every data grid a navy column header.
- **Do** set every figure in tabular numerals and follow every clock time with its local zone in smaller muted type.
- **Do** show missing data as a muted en dash and estimates labelled "estimated".
- **Do** dock the panel over the map at 900px and up, and below a 320px map under it.
- **Do** keep app-button text navy on sky, with the `sky-edge` border.

### Don't:
- **Don't** use rounded-card SaaS styling: no radii above 4px on panels, buttons or inputs, and no shadows on panels below the stage.
- **Don't** use poster-scale display type; the largest type on a page is a 32px airport code.
- **Don't** set body or link text in sky; use `link`.
- **Don't** show a green status beside a late departure.
- **Don't** introduce a second typeface or new state colours.
