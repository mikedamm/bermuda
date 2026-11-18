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
  flown: "#cee9fd"
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
- White masthead, then a full-bleed map stage whose top is the title band in 80% navy, the map running on under it; paper ground below.
- One docked flight panel with a status band welded to its top edge.
- Panels with pale header bars; data grids with navy column headers and zebra rows.
- Every number tabular; every time carries its local zone in small muted type.
- Flat surfaces; the single real shadow is the dock floating over the map.

## Colors

A two-blue brand on cool neutrals, with three solid state colours that do all the signalling.

### Primary
- **WingView Navy** (`navy`): the structural colour. Title band, data-grid column headers, airport codes, headline times, the plane glyph, pin rings. Deepened to `navy-deep` for the footer and lifted to `navy-hover` on the primary button.

### Secondary
- **Sky** (`sky`): live and forward. The route still ahead on the map, the subject airport's fill, the main airport pins, focus rings, and the app button (always with navy text and a `sky-edge` border, as in the app itself). `sky-hover` is its hover.
- **Deep Sky / Link Blue** (`link`, same value as `sky-edge`): text links, the route already flown, bar-chart fills, the airport-traffic network lines, range rings, and the live status band. `link-hover` darkens it for hovered links.

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

- **Shell, top to bottom:** white masthead (64px: logo at 168px on the left, search up to 460px at the page's centre, the App Store and Google Play badges on the right), title band (on flight and aircraft pages the flight on the left and the tail number on the right; elsewhere the name with its codes and a quiet "Updated 12 seconds ago" at the bottom right), map stage, paper content area, white breadcrumb bar, deep-navy footer. The masthead is a three-column grid whose side columns are equal and never narrower than the two badges, so the search stays centred and gives up its own width first (256px at 900px).
- **Title band:** on pages with a map it is the top of the stage, 80% navy over the map, and the map is framed below it; elsewhere it is solid navy. Flight and aircraft pages share one band of two heroes. On the left the flight: the airline's mark and the flight as people say it, "United Airlines Flight 7" (the IATA number without the airline's code, else the callsign's). On the right, right-aligned, the tail number in the same 28px 700 white with the airframe ("Boeing 787-9 Dreamliner") under it in 14px `band-text`. The flight page's H1 is the flight, its mark links to the airline and the tail number to the aircraft page; the aircraft page's H1 is the tail number, and its left hero is the flight it is flying, else the next flight it is assigned to, else its operator, the whole hero linking there. No update time, status chip, callsign or date: status lives in the dock, the date in the flight times panel, the Mode S code in the aircraft's specs. Under 640px the tail drops to its own row past a faint white rule, the airframe beside it. On flight, aircraft and airport pages the band sits above the stage and sticks to the top of the page at full size. The moment the dock scrolls up and touches it, the dock folds into it: the dock fades out in 140ms as it lifts 12px into the band, and the band condenses in 240ms into a 56px bar (its foot is clipped, its navy goes from 80% to solid, and its full row fades out and rises) while the condensed row fades in just behind it. The condensed row, left to right: the WingView logo at reduced size (116px, from wingview-header-dark.svg, the masthead logo with "Wing" in white for the navy; the round mark alone on phones); in the middle the dock's one-line summary (the status as a coloured chip, the route's codes either side of a 112px progress track in sky, and the status detail; the chip alone on phones, beside the mark); at the right, by the tail, the airline's mark at 28px with the flight's ICAO ident (ASA14), or on an aircraft page the operator's code when it is not on a flight, then a faint rule and the tail number in white. Both pages draw it from one template (band-bar, fed by each page's Bar()); on flight pages the summary is a live part, so it refreshes with the dock it stands in for. The condensed row only repeats the page, so it is hidden from assistive tech and out of the tab order. Scrolling back past the line (6px of slack, so it cannot flicker) reverses it: condensed row out, bar open, dock back. It is all clip, transform and opacity, so the band's height in the flow never changes and nothing below moves; reduced motion makes it instant. On phones the tail's row is clipped away and the summary keeps only the chip. The airport page has no dock, so its band condenses when the first board reaches it; its condensed row keeps the WingView logo at the left, puts the split-flap clock in the middle at 20 x 28px (still turning on the minute), and at the right the airport's name in `band-text` (left off on phones), a faint rule and its code in white, where the tail sits on aircraft pages. Other bands keep the "Updated" time, which counts up in the browser from when the page's data was read ("just now", "12 seconds ago", "3 minutes ago"); the server's clock time stays for readers without script and becomes its tooltip.
- **Stage:** the map is the page's fixed background: it fills the window and stays put while the band, the dock and a full-width paper sheet holding the panels scroll up over it. It sits at the base layer (z-index 0), not behind the page, so its pins and aircraft stay clickable through the stage's open area; the masthead, the sheet, the breadcrumbs and the footer are positioned above it. The camera frames the route in the window the page opens with: under the band and above whatever covers the map below it (the sheet on wide screens, the dock on phones), and the map credit sits at the foot of that window. The stage keeps its height (min 560px); the dock sits in normal flow inside the content column, 20px from the top, so the stage grows with the dock and nothing in it is ever clipped or scrolled. 360px wide for flights, 400px for search. On flight and aircraft pages the dock carries only what a glance needs (status, route, times, distance flown and to go, altitude, ground speed); the aircraft, heading, squawk and position age sit in a side panel that refreshes with it. The aircraft's 3D model from the app sits in the dock under the status band, above the route, full width and 16:9 on white: a plain iframe of the app's embed (it draws nothing when the app has no model for the aircraft). On flight pages the band and the route are separate live parts with the model between them, so it never reloads. The map is fixed: no pan, zoom or controls, framed by the server between the dock and the right edge of the content column; pins and aircraft link to their pages. At 900px and up the sheet rises 120px into the stage, and the map fades into its paper over the 240px around the sheet's top edge; the fade travels with the sheet as it scrolls (no hairline under the stage).
- **Below the stage:** a main column plus a 340px side column at 1000px and up; 20px gaps between panels; 24px above (at 900px and up, 120px into the stage's fade instead), 48px below. On flight pages the main column holds the Flights panel and the side column starts with Flight times (there for now, tightened to fit: zone names dropped, since the note says times are local, and an estimate under its time), then the aircraft and position, About and same-route flights.
- **Under 900px:** the logo and badges share the masthead's first row, scaled with the screen (logo 37vw, badges 7vw tall, both capped at their desktop size), and the search takes a full row. The stage stacks: the title band over the map's top, then 320px of map, then the dock, full width, square-cornered, no shadow, divided by a bottom rule.
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

Small, square-ish corners and hairline rules. Panels, the dock, buttons and inputs take 4px; the title-band status label and map-pin labels take 3px; pins are round. Under 640px panels lose their corners and side borders; under 900px the dock does too. Lines are 1px everywhere; the only heavier strokes are map routes and the 2px sky underline on the current nav item.

## Components

### Buttons
Compact and solid, 600 weight at 14px, 38px tall, 4px corners.
- **App button:** sky fill, navy text, `sky-edge` border; lighter sky on hover. Used full-width (42px) at the foot of the airport dock.
- **Store badges:** Apple's "Download on the App Store" and Google's "Get it on Google Play" artwork, unaltered, both 135 x 40, 8px apart, linking to www.wingview.com/ios and /android.
- **Primary:** navy fill, white text; `navy-hover` on hover. Used for search submit (44px beside a 44px input).
- **Focus:** 2px sky outline, 2px offset, site-wide.

### Inputs / Fields
- **Style:** white, 1px `line-strong` border, 4px corners, 40px (masthead) or 44px at 16px type (search dock).
- **Focus:** 2px sky outline inset by 1px, border to `sky-edge`.

### Navigation
- **Masthead:** no text links: logo, search, store badges. The tracker and the app are linked from the footer.
- **Breadcrumbs:** 13px muted links with slash separators on a white bar.
- **Footer:** deep navy, `band-text` copy, white links.

### Cards / Containers (panels)
- **Corner Style:** 4px. **Background:** surface. **Border:** 1px `line`. **Shadow:** none.
- **Header bar:** `head` fill, 15px 700 navy title, 10px 14px padding, an optional muted count right-aligned on the same baseline.
- **Body:** 12px 14px; notes 12.5px muted under a `line-soft` rule.

### Status Band (signature)
The one-line answer welded to the top edge of the dock: white text on a solid state fill, 11px 16px padding, the state word at 17px 700 and the detail at 600, spread to opposite ends. States: `ok` (arrived or landed on time), `late` (late departure or arrival, including a late departure on an otherwise on-time estimate), `bad` (diverted, cancelled), `live` (en route or taxiing with no delay known, in link blue), `idle` (scheduled, or no current data). The title band does not repeat it.

### Route Block
The route is the dock's progress bar: a strip across the dock's full width, under the model, filled in `flown` (sky at 22% on white) from the left as far as the flight has come and `paper` for the rest; a landed flight is full, a scheduled one empty. Departure and arrival codes in 32px navy at either end with city names in 13px muted below; between them, centred and balanced, the distance in 13px 600 ink on two lines ("536 mi flown" over "518 mi to go" in flight, "1,054 mi" over "2h 30m in the air" otherwise). No line or marker at the fill's edge, which would cut the text. Below it, the two times that matter at 19px navy with small zones, each with a 13px delay word in `ok` or `late`, and under each where to find the flight at that end in 12.5px 600 ink, parts joined by a middle dot: "Terminal 1 · Gate B12" where it left from, "Gate N10 · Baggage 15" where it lands (the airline's assignments, as the boards use them; nothing when none is known).

### Telemetry Rows
Two-column definition grid (altitude, ground speed, heading, squawk): 12px muted label over a 15px 600 tabular value, each cell topped by a `line-soft` rule. A muted right-aligned line gives the position's age. Missing readings show an en dash. Single label/value rows (aircraft, registration) are flex rows: muted label left, 600 value right.

### Data Grids
Full-width collapsed tables at 13.5px: navy header row with 12px 600 `grid-head-text`, 8px 14px cells divided by `line-soft`, zebra even rows, `hover` rows on pointer devices. Links and row headers are 600. Numeric columns right-aligned. Status words are coloured tags (12.5px 600): landed in `ok`, in the air or taxiing in `link`, alerts in `bad`, the rest muted. The scheduled-versus-actual table groups rows under white sub-headers in 13px 700 navy. Bar columns use a 6px `line-soft` track with a `link` fill.

**Airport page:** the map stage is the same height as on aircraft pages (560px less the band, plus the overlap; 380px on phones, taller than the other pages' 320px, since the map of the field and its traffic is the page's lead) and carries no dock and no range rings; it opens at zoom 6.8, wide enough to name the cities around the field, its traffic coloured by what it is doing there (see Map), and the board headings carry the key: a 10px square with 2px corners before Arrivals (`link` blue), Departures (`ok` green) and Flying over (gray). The band sits above it and condenses as described under the title band. The band's right side is a split-flap clock of the airport's local time, HH:MM with a leading zero and an AM/PM flap, "Local time · PST" under it in 12px `band-text`. Each flap is a 28 x 40px tile in two halves of dark navy (#173158 to #0b1b33) split by a 1px hinge, 26px 700 figures, a soft 2px shadow; when a figure changes the old top half falls away (140ms) and the new bottom half lands (140ms) as on an old departures board, on the minute; reduced motion swaps without the turn. Phones put the clock on its own row under the name at 23 x 32px. Below the stage, two rows of two equal columns at 1000px and up: Arrivals and Departures, then Parked and Flying over; then About, the airlines here now and the nearby airports in three. The arrivals and departures boards read as an airport's screens: time, flight, from or to, gate, bags (arrivals), status; from or to is the city, as screens name it ("Honolulu", the island after its comma left off), linking to the airport with its name and code in the title, and with the code beside it in muted type where another airport of the city is on the board too ("Chicago ORD", "Chicago MDW"); earliest first, what has landed or left (the API's ten newest), a 2px sky line at now, then what is inbound or scheduled (its ten soonest). The time is the one the board sorts by (wheels-on or gate-in, wheels-off or gate-out, the estimate, the schedule), so the column reads in order and is never blank, in 700 ink; the zone is said once, in the column head ("Time PST"). An inbound flight in the air says when it gets here, not "En Route", in as few characters as a board can: "On time" (up to five minutes early) and "10m early" in green, "4m late" in sky-edge blue, "27m late" (15 minutes or more) in amber; a delay is in its largest unit alone, minutes to 59, then hours to the nearest ("1h late" to 89 minutes, "3h late"). One still to leave keeps "Scheduled" with how far off schedule it is under it. Notes under a status are in the same words, 11.5px muted: a departure held to a slot "12m late", one off its gate "8m early"; a departure that has pushed back before its phase moves on reads "Left gate". A flight fills its row from the left as far along its route as it is: in the air by its position, else by the clock from take-off to the estimate, between 1% and 97%; 98% once it has landed and is taxiing in; all of it once it is at the gate ("Arrived"). A flight in the air keeps filling between the 30-second updates: the server sends its rate with the fill (ground speed over the route's length, or 100% over the planned flight where the fill is by the clock), and site.js advances it every two seconds from when the page drew it, never past 97% (99% on a flight page's route and band track, which fill the same way). The fill is sky at 4% deepening to 22% and stopping hard where the aircraft is now; no line marks the edge, so nothing crosses the text and the only level sky line on a board is the line at now. So the arrivals board is full above the line at now and nearly full below it (the inbound flights are minutes out); on the departures board the rows above it are just started, and a flight that has already reached its destination shows full. Gates are the airline's (the API's `arr_gate`, `dep_gate` and `baggage`, with the terminals on the flight page), each in a column of its own in 700 ink, as an airport's screens have them: the gate an arrival comes to and the carousel its bags come out on, whether it is at the gate, taxiing in or still on its way; the gate a departure leaves from, before and after it pushes back. A board has a Gate column only when one of its rows has a gate and a Bags one only when one has a carousel, so a field the airlines do not report to (Boeing Field) keeps four columns; a row with nothing known leaves its cell blank. With the gate in its column the status stays short ("Arrived", not "Arrived Gate B4"). A gate has a number not led by a zero, so placeholders ("G", "N/A", the "001" a regional waits at) are dropped; a cancelled flight shows no gates and a diverted one no arrival gate or carousel. The rows are laid out on a subgrid of the board's columns rather than as table rows, so the fill paints across the row in every browser; explicit table roles keep the table's meaning. A board narrows by its own width, not the window's (a container query, since the two sit abreast from 1000px): at 520px and under its cells tighten to 6px, the city keeps at least 5.5em and is cut with an ellipsis past that, and a status wraps before anything is cut; at 420px a carousel goes under its gate in one column, headed "Gate" over a muted "Bags", the carousel in 12px 500 muted, and a twin's code goes under its city; at 380px the airline's logo is left off; at 340px the city keeps 3.4em and the edges 10px. So phones keep every column, and nothing scrolls sideways down to 320px. Both boards refresh in place every 30 seconds (live.js), redrawn by the same templates. A flight the data holds twice shows once (the same number within half an hour, or still coming within three hours of landing or leaving). Parked lists aircraft on the ground within 3 miles of the field (departing ones first, then arrived, then parked, aircraft the index can name first; a landed flight stays live for hours, so it reads "Arrived", not "just landed"), Flying over the traffic passing within 25 miles with its route where the lookup finds it (a flight under the same callsign more than 75 miles from the field is another aircraft's, and lends it nothing). A passing flight in the air between two fields fills its row as the boards' rows do, 1% to 97% of the way, the table laid out on the same kind of grid (as cards on phones); one whose record has it on the ground, or back to where it left, stays clear.

**Flights panel** (flight and aircraft pages): one table titled "Flights", latest first, covering the 30-day window. Flights still to come sit on top in `muted` ink with a half-faded airline mark and the status "Scheduled", showing their planned times; the flight under way (else the latest one flown) is next, on the `flown` sky fill in bold, its arrival an estimate marked "est." until it lands; the past runs down from it. Columns: date, aircraft (flight page) or flight (aircraft page), from, to, departure, arrival, duration, status. A footer line reads "143 flights, last 30 days", or "26–50 of 143, last 30 days" when the list pages; pages hold 25 rows, the bare URL is page 1 and later pages are `?page=N` (noindex), with Newer and Older plus page numbers (the first, the last, two either side of the current one) as 32px bordered buttons, the current page filled navy. On phones the rows become the usual labelled cards.

### About Panel
The side column's first panel on flight, airport and aircraft pages. One plain sentence that says what the thing is (a flight's route and how often it flies; an airport's place; an aircraft's year, model and operator), then a key-value list of facts the rest of the page does not show. Live status belongs to the dock, codes and type to the title band, a flying record to its own panel, so none of it is repeated here. A fact's qualifier ("on 14 of 27 flights") sits under its value in 12.5px muted, never squeezed into it. Values are written for people: "PST · UTC−8", not a zone database name; "47.4479° N, 122.3103° W", not signed decimals; "Twin jet", not "2 jet". On airport pages the dock is live traffic only and the reference facts (elevation, coordinates, time zone) live here.

### Map
MapLibre GL JS over WingView's Protomaps v4 basemap (a dated build, `i.wvcdn.com/20261115.pmtiles`), styled by `static/map-style.json`. It started from `@protomaps/basemaps` light and is cut down to what draws at the zooms the pages use (0 to about 9): no streets, buildings, street labels, tunnels, bridges, rail or small landuse, 20 layers in all. It is a consumer map, not a chart: quiet, warm and legible under the routes.
- **Palette:** a warm atlas, the complement of the page's navy and sky. Land `land` (a warm stone), water `ocean` (a quiet grey-teal, a different hue from the sky route ahead), low-zoom vegetation in olive-to-sage tints of the same warm family, highways one soft warm line (#e8d6b6), major roads fainter from zoom 7, country borders solid warm taupe (#b9a796), state borders lighter and dashed. Airports alone are cool (#e6eaf2) with slate runways from zoom 7, since they are the site's subject. The brand blues stay the only saturated colour on the map: routes, pins and aircraft.
- **Relief:** Mapterhorn elevation (`TERRAIN`), shaded with MapLibre's `igor` method in a warm brown (#7a6652), under the water so seas and lakes stay flat. Strongest at world zoom (0.75), where it picks out the ranges, and quiet at airport zoom (0.22), under the traffic. Elevation is read only to zoom 6 and stretched beyond it, which keeps a page to a few tiles.
- **Labels:** Inter, as on the page, with the page's `cv11` and `ss01` forms baked into the glyphs; English names only. Countries in Medium capitals, letter-spaced 0.12em, warm grey (#6b5c4f); states lighter (#9c8b7c), abbreviated below zoom 6; cities from zoom 3 in Regular, sparse and ranked by population, subdued (#776b60 for the large, #94887c the rest, 3px padding) and quieter than the airport codes, but always shown: a main airport's label outranks them, a minor airport's gives way to them, and aircraft never push them off; seas and lakes in letter-spaced Italic grey-teal (#5b8595). Pin and aircraft labels are Inter SemiBold.
- **Route flown:** solid `link`, 3.2px, along the trail the aircraft actually flew. **Route ahead:** sky, 3px, short dashes, along the rest of its filed route from the aircraft onward. Great circles stand in when the trail or plan is missing, when the aircraft is far off its route, or when it has diverted. A route across the antimeridian stays in one copy of the world, with its airports and aircraft beside it.
- **Earlier flights** (aircraft pages): the map draws the flight under way, else the latest one flown, as its flight page does, and under it the three flights before it as 2.4px gray lines along their tracks (great circles when none is on record), slate #4a5363 for the latest fading to #b2b9c3 for the third, the latest drawn on top; their airports join as minor pins and the map frames all four. A parked aircraft keeps its last position on the map.
- **Airport pins:** a navy ring around a white dot, sized by traffic; main airports get a sky dot with a white stroke and larger 13px labels. Labels are navy with a white halo and give way to each other, busiest first.
- **Subject airport:** navy ring, sky dot, white stroke, 15px label that never gives way, drawn above traffic.
- **Planes:** navy top-down airliner glyph with a white edge, rotated to heading; the subject flight larger (30px) than other traffic (22px). On an airport's map each is coloured by what it is doing there, as the boards' headings key it: arrivals `link` blue, departures `ok` green, flights passing over gray (#8792a2); behind each, its last three minutes as a 2px line in its colour fading from 80% at the aircraft to nothing (the API's last five minutes, a point every ten seconds at most, reported positions only; none where its last report is more than five miles from it).
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
