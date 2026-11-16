---
version: 1
slug: "internal-web-templates-flight-html"
primary_target: "internal/web/templates/flight.html"
related_targets: ["internal/web/templates/aircraft.html","internal/web/templates/airport.html","internal/web/templates/airline.html","internal/web/templates/home.html","internal/web/templates/layout.html","internal/web/static/site.css"]
---

# Surface: entity pages (flight, aircraft, airport, airline, tracker)

Mode: Operate. A visitor from search gets one answer (where is it, when does it land) and can go deeper. Travelers first, enthusiasts below. Success: answered on the page; the WingView 3D app is the next step, never a wall.

Constraints: server-rendered, complete without JavaScript; Mapbox GL JS interactive map on every page (owner's choice); LADD aircraft show no map or flights; logo, navy #002f64, sky #229cf8 and Inter are binding.

## Direction contract

THESIS: A flight-tracker page in the FlightAware / Flightradar24 convention: the live map leads, a docked flight panel answers status at a glance, dense tables carry the depth. It refuses the rounded-card SaaS page and the poster-scale display type of the previous build.

OWN-WORLD: WingView navy title band and table headers; sky blue for live state, the route ahead and the app button (navy text, #0065ad edge, as in the app); #0065ad links; paper #f1f4f8 ground; white panels with 1px #d6dde7 rules and 4px corners; solid green, amber, red status bands; Inter at UI sizes with tabular numerals; Mapbox light basemap tinted to the brand.

STORY: The traveler reads status and arrival time in the panel and sees the plane on the map; scans the scheduled-versus-actual times table; enthusiasts continue to history, aircraft and airport links; anyone can open the 3D app.

FIRST VIEWPORT: White header (logo, search, app button). Navy band: flight number, airline, callsign, status. Full-bleed interactive map, 540px tall, with a 360px flight panel docked left inside the content column: status band, route codes with progress, departed and arriving times with delay, aircraft, live data, app button. Mobile: band, 320px map, panel below it.

FORM: The category standard, played straight (canon). Signature move: the status band welded to the top of the flight panel, coloured by state, over the live map. seed: none (pinned): "We do want to look like FlightAware or Flightradar24"; "An example of what we are going for is https://www.flightaware.com/live/flight/UAL1275".

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
