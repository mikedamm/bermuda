// Package api is a typed client for the WingView internal API
// (INTERNAL-API.md). JSON null decodes to the zero value: "" for strings,
// 0 for numbers, the zero time for timestamps.
package api

import "time"

// AirportRef is the shared airport reference shape.
type AirportRef struct {
	ICAO        string  `json:"icao"`
	IATA        string  `json:"iata"`
	FAA         string  `json:"faa"`
	Name        string  `json:"name"`
	City        string  `json:"city"`
	Region      string  `json:"region"`
	Country     string  `json:"country"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	ElevationFt float64 `json:"elevation_ft"`
	Type        string  `json:"type"`
	TZ          string  `json:"tz"`
}

type Operator struct {
	ICAO     string `json:"icao"`
	IATA     string `json:"iata"`
	Name     string `json:"name"`
	Callsign string `json:"callsign"`
}

// FlightSummary is a live flight (status.flight, status.upcoming[], search
// results' live). Dep/Arr are airport codes.
type FlightSummary struct {
	FlightID     string    `json:"flight_id"`
	AirframeID   string    `json:"airframe_id"`
	Callsign     string    `json:"callsign"`
	IATAFlight   string    `json:"iata_flight"`
	Operator     Operator  `json:"operator"`
	Registration string    `json:"registration"`
	Hex          string    `json:"hex"`
	Type         string    `json:"type"`
	Model        string    `json:"model"`
	Dep          string    `json:"dep"`
	Arr          string    `json:"arr"`
	ActualArr    string    `json:"actual_arr"`
	Status       string    `json:"status"`
	Phase        string    `json:"phase"`
	PhaseWord    string    `json:"phase_word"`
	Lat          float64   `json:"lat"`
	Lon          float64   `json:"lon"`
	Alt          float64   `json:"alt"`
	GS           float64   `json:"gs"`
	Hdg          float64   `json:"hdg"`
	OnGround     bool      `json:"on_ground"`
	PositionAt   time.Time `json:"position_at"`
	LastSeen     time.Time `json:"last_seen"`
	ETD          time.Time `json:"etd"`
	EDCT         time.Time `json:"edct"`
	ETA          time.Time `json:"eta"`
	STA          time.Time `json:"sta"`
	Out          time.Time `json:"out"`
	Off          time.Time `json:"off"`
	On           time.Time `json:"on"`
	In           time.Time `json:"in"`
	SchedDep     time.Time `json:"sched_dep"`
	SchedArr     time.Time `json:"sched_arr"`
	Runway       string    `json:"runway"`
	Squawk       string    `json:"squawk"`
	LADDRedacted bool      `json:"ladd_redacted"`
}

// HistoryEntry is an archived flight. Dep/Arr/ActualArr are references.
type HistoryEntry struct {
	FlightID      string      `json:"flight_id"`
	AirframeID    string      `json:"airframe_id"`
	Callsign      string      `json:"callsign"`
	IATAFlight    string      `json:"iata_flight"`
	Operator      Operator    `json:"operator"`
	Registration  string      `json:"registration"`
	Hex           string      `json:"hex"`
	Type          string      `json:"type"`
	Dep           *AirportRef `json:"dep"`
	Arr           *AirportRef `json:"arr"`
	ActualArr     *AirportRef `json:"actual_arr"`
	Status        string      `json:"status"`
	Outcome       string      `json:"outcome"`
	Phase         string      `json:"phase"`
	PhaseWord     string      `json:"phase_word"`
	FirstSeen     time.Time   `json:"first_seen"`
	LastSeen      time.Time   `json:"last_seen"`
	ETD           time.Time   `json:"etd"`
	ETA           time.Time   `json:"eta"`
	Off           time.Time   `json:"off"`
	On            time.Time   `json:"on"`
	Out           time.Time   `json:"out"`
	In            time.Time   `json:"in"`
	DurationS     int64       `json:"duration_s"`
	DurationBasis string      `json:"duration_basis"`
	LADDRedacted  bool        `json:"ladd_redacted"`
}

type Country struct {
	ISO  string `json:"iso"`
	Name string `json:"name"`
}

type Owner struct {
	Name           string `json:"name"`
	City           string `json:"city"`
	State          string `json:"state"`
	Country        string `json:"country"`
	RegistrantType string `json:"registrant_type"`
	Registered     string `json:"registered"`
}

type Registry struct {
	Source         string `json:"source"`
	Status         string `json:"status"`
	Registration   string `json:"registration"`
	Manufacturer   string `json:"manufacturer"`
	Model          string `json:"model"`
	SerialNumber   string `json:"serial_number"`
	Category       string `json:"category"`
	YearBuilt      int    `json:"year_built"`
	AgeYears       int    `json:"age_years"`
	EngineType     string `json:"engine_type"`
	EngineCount    int    `json:"engine_count"`
	Seats          int    `json:"seats"`
	RegistrantType string `json:"registrant_type"`
	Registered     string `json:"registered"`
	Owner          *Owner `json:"owner"`
}

type Aircraft struct {
	ICAOType        string    `json:"icao_type"`
	TypeKnown       bool      `json:"type_known"`
	Manufacturer    string    `json:"manufacturer"`
	Model           string    `json:"model"`
	Name            string    `json:"name"`
	Engines         int       `json:"engines"`
	EngineType      string    `json:"engine_type"`
	Species         string    `json:"species"`
	Wake            string    `json:"wake"`
	Registration    string    `json:"registration"`
	Hex             string    `json:"hex"`
	Country         Country   `json:"country"`
	Military        bool      `json:"military"`
	Interesting     bool      `json:"interesting"`
	TypeDescription string    `json:"type_description"`
	YearBuilt       int       `json:"year_built"`
	AgeYears        int       `json:"age_years"`
	SerialNumber    string    `json:"serial_number"`
	Seats           int       `json:"seats"`
	HexSince        time.Time `json:"hex_since"`
	Registry        *Registry `json:"registry"`
	LADDRedacted    bool      `json:"ladd_redacted"`
	AirframeID      string    `json:"airframe_id"`
}

type LastPosition struct {
	Live     bool        `json:"live"`
	FlightID string      `json:"flight_id"`
	At       time.Time   `json:"at"`
	AgeS     int64       `json:"age_s"`
	Lat      float64     `json:"lat"`
	Lon      float64     `json:"lon"`
	Alt      float64     `json:"alt"`
	Hdg      float64     `json:"hdg"`
	OnGround bool        `json:"on_ground"`
	Airport  *AirportRef `json:"airport"`
}

type AircraftStatus struct {
	Phase    string          `json:"phase"` // flying, on_ground, unknown
	Flight   *FlightSummary  `json:"flight"`
	Last     *LastPosition   `json:"last"`
	Upcoming []FlightSummary `json:"upcoming"`
}

type AirportCount struct {
	Code    string      `json:"code"`
	Count   int         `json:"count"`
	Airport *AirportRef `json:"airport"`
}

type Stats struct {
	Days         int            `json:"days"`
	Flights      int            `json:"flights"`
	Hours        float64        `json:"hours"`
	Diverted     int            `json:"diverted"`
	LastFlightAt time.Time      `json:"last_flight_at"`
	Airports     []AirportCount `json:"airports"`
}

type RegistryVersion struct {
	Registration   string `json:"registration"`
	ValidFrom      string `json:"valid_from"`
	ValidTo        string `json:"valid_to"`
	Manufacturer   string `json:"manufacturer"`
	Model          string `json:"model"`
	AircraftType   string `json:"aircraft_type"`
	OwnerName      string `json:"owner_name"`
	OwnerCity      string `json:"owner_city"`
	OwnerState     string `json:"owner_state"`
	OwnerCountry   string `json:"owner_country"`
	RegistrantType string `json:"registrant_type"`
	Status         string `json:"status"`
	YearBuilt      int    `json:"year_built"`
	CertIssueDate  string `json:"cert_issue_date"`
}

type LADD struct {
	Listed   bool `json:"listed"`
	Redacted bool `json:"redacted"`
}

// AircraftPage is GET /aircraft/{key}.
type AircraftPage struct {
	Key             string            `json:"key"`
	AirframeID      string            `json:"airframe_id"`
	Days            int               `json:"days"`
	Aircraft        Aircraft          `json:"aircraft"`
	Operator        Operator          `json:"operator"`
	Owner           *Owner            `json:"owner"`
	Status          AircraftStatus    `json:"status"`
	Stats           Stats             `json:"stats"`
	RecentFlights   []HistoryEntry    `json:"recent_flights"`
	RegistryHistory []RegistryVersion `json:"registry_history"`
	LADD            LADD              `json:"ladd"`
}

// FlightsPage is GET /aircraft/{key}/flights.
type FlightsPage struct {
	Key        string         `json:"key"`
	AirframeID string         `json:"airframe_id"`
	Days       int            `json:"days"`
	Count      int            `json:"count"`
	Flights    []HistoryEntry `json:"flights"`
	NextBefore time.Time      `json:"next_before"`
	LADD       LADD           `json:"ladd"`
}

type Match struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Exact bool   `json:"exact"`
}

type LastFlight struct {
	FlightID string    `json:"flight_id"`
	Callsign string    `json:"callsign"`
	Dep      string    `json:"dep"`
	Arr      string    `json:"arr"`
	Outcome  string    `json:"outcome"`
	LastSeen time.Time `json:"last_seen"`
}

type SearchResult struct {
	AirframeID   string         `json:"airframe_id"`
	Registration string         `json:"registration"`
	Hex          string         `json:"hex"`
	ICAOType     string         `json:"icao_type"`
	Manufacturer string         `json:"manufacturer"`
	Model        string         `json:"model"`
	Name         string         `json:"name"`
	Country      Country        `json:"country"`
	Military     bool           `json:"military"`
	Interesting  bool           `json:"interesting"`
	OwnerName    string         `json:"owner_name"`
	YearBuilt    int            `json:"year_built"`
	Matched      Match          `json:"matched"`
	Live         *FlightSummary `json:"live"`
	LastFlight   *LastFlight    `json:"last_flight"`
	LADDRedacted bool           `json:"ladd_redacted"`
}

// Search is GET /search/aircraft.
type Search struct {
	Q               string         `json:"q"`
	Interpretations []string       `json:"interpretations"`
	Count           int            `json:"count"`
	Total           int            `json:"total"`
	Results         []SearchResult `json:"results"`
}

type NearbyAircraft struct {
	Callsign       string  `json:"callsign"`
	Squawk         string  `json:"squawk"`
	SquawkAssigned string  `json:"squawk_assigned"`
	Direction      string  `json:"direction"` // arriving, departing, local, transit
	FlightID       string  `json:"flight_id"`
	Hex            string  `json:"hex"`
	Registration   string  `json:"registration"`
	AltFt          float64 `json:"alt_ft"`
	OnGround       bool    `json:"on_ground"`
	Status         string  `json:"status"`
	DistanceMi     float64 `json:"distance_mi"`
}

// AirportTraffic is GET /airports/{code}/aircraft. Airport is a bare code.
type AirportTraffic struct {
	Airport  string           `json:"airport"`
	RadiusMi float64          `json:"radius_mi"`
	Count    int              `json:"count"`
	Aircraft []NearbyAircraft `json:"aircraft"`
}
