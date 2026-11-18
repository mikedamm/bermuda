# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Primary: travelers and the people meeting them, landing from a search result ("UA1275", "N527AS", "SEA arrivals"), very often on a phone, wanting one answer: where the flight is, when it lands, whether it is late.

Secondary: aviation enthusiasts (spotters, av-geeks) who want the depth: registrations, airframe and ownership records, route history, what is flying near an airport. They read further down the same pages.

## Product Purpose

WingView's public flight pages answer a flight, aircraft, airport or airline question completely on the page, from WingView's own live flight data, and offer the WingView app (app.wingview.com, aircraft and airport activity in 3D) as the next step for anyone who wants to watch it live. Success is a visitor whose question is answered without leaving the page, with the app as the natural follow-on, not a gate. The pages also build WingView's search presence.

## Positioning

The pages sit on WingView's own live tracking data and lead into a 3D globe and airport view of the same aircraft; neighboring trackers' pages end at a 2D map.

## Operating Context

- Entry is almost always a search engine result page; the first screen must answer the query that brought the visitor.
- Served from www.wingview.com under `/flight/`, `/aircraft/`, `/airport/`, `/airline/` and `/tracker/`, routed by the main site to a Go container; the rest of www.wingview.com is a separate site.
- Pages are server-rendered HTML, rendered fresh on every request (never cached; only the API answers behind them are); they must be complete without JavaScript, for crawlers and fast first paint.
- Times are shown in each airport's local time.

## Capabilities and Constraints

- Data per flight: live position, altitude, speed, heading, squawk; gate and runway times (scheduled, out, off, on, in, estimates); aircraft registration and type; 30 days of history per flight number. Many fields are often missing and must degrade gracefully.
- Data per aircraft: type, specs, register entry and owner history, recent flights, busiest airports, last position.
- Data per airport: live traffic within 50 miles (inbound, outbound, on the ground, overhead, parked), with routes where resolved; reference data and nearby airports.
- No flown-track positions yet: a route is drawn as a great circle split at the live position.
- Aircraft in the FAA LADD program show identity and owner only; no positions or flights.
- Maps are WingView's own Protomaps basemap (`https://i.wvcdn.com/20261115.pmtiles`, a dated planet-wide Protomaps build the `adsb` Bunny pull zone proxies), drawn in the browser with MapLibre GL JS; no third-party map service or key.
- Terminology: flight number (IATA, "UA1275") shown first, ICAO callsign ("UAL1275") second; registration ("tail number"); gate out/off/on/in.

## Brand Commitments

Binding (confirmed by the owner): the WingView name and window-and-wing logo, the app's navy `#002f64` and sky blue `#229cf8` palette, and the Inter typeface. The pages must read as part of the same product as app.wingview.com.

Binding (owner's words): "We do want to look like FlightAware or Flightradar24." The pages follow the established flight-tracker convention those two products set (the original brief's reference is flightaware.com/live/flight/UAL1275), executed at their craft level in WingView's brand, with interactive maps on every page.

## Evidence on Hand

Real live and archived flight data from the internal API, real airport reference data, the logo and brand SVGs. No testimonials, user counts, press or accuracy claims exist; none may be invented.

## Product Principles

1. Answer first: the traveler's question is answered in the first screen, in plain words, before any depth.
2. Depth below, never in the way: enthusiast detail follows, complete and scannable.
3. The app is the next step, offered where watching live adds something, never a wall.
4. Say what is known: show estimates as estimates, missing data as missing, and never imply precision the data lacks.
5. Fast and crawlable: every page is complete HTML, light enough for a phone on a bad connection.
