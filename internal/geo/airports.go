// Package geo holds the embedded reference data (airports, place names, the
// basemap) and the pure functions that turn coordinates into maps.
package geo

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

//go:embed data/*.gz
var dataFS embed.FS

// Airport is one row of the embedded airport table (OurAirports, with time
// zones joined from mwgg/Airports).
type Airport struct {
	ICAO      string // page key; the ICAO code, else the source ident
	IATA      string
	Local     string // national code (FAA LID in the US) when it differs
	Name      string
	City      string
	Region    string // ISO 3166-2, e.g. US-WA
	Country   string // ISO 3166-1 alpha-2
	Lat, Lon  float64
	ElevFt    int
	Size      byte // 'L' large, 'M' medium, 'S' small
	Scheduled bool
	TZ        string
	Wiki      string // en.wikipedia.org article slug
}

// Code is the code a traveller knows: IATA if there is one, else ICAO.
func (a Airport) Code() string {
	if a.IATA != "" {
		return a.IATA
	}
	return a.ICAO
}

// Place is "Seattle, WA" in the US and Canada, "London, United Kingdom" elsewhere.
func (a Airport) Place() string {
	city := a.City
	var region string
	if a.Country == "US" || a.Country == "CA" || a.Country == "AU" {
		if i := strings.IndexByte(a.Region, '-'); i > 0 {
			region = a.Region[i+1:]
		}
	} else {
		region = CountryName(a.Country)
	}
	switch {
	case city == "":
		return region
	case region == "" || region == city:
		return city
	}
	return city + ", " + region
}

type airportDB struct {
	byCode map[string]*Airport // ICAO, IATA and local codes
	all    []*Airport
}

var (
	airports = loadAirports()
	names    = loadNames()
)

func loadAirports() airportDB {
	db := airportDB{byCode: map[string]*Airport{}}
	sc := bufio.NewScanner(gunzip("data/airports.tsv.gz"))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 14 {
			continue
		}
		lat, _ := strconv.ParseFloat(f[7], 64)
		lon, _ := strconv.ParseFloat(f[8], 64)
		elev, _ := strconv.Atoi(f[9])
		a := &Airport{
			ICAO: f[0], IATA: f[1], Local: f[2], Name: f[3], City: f[4], Region: f[5], Country: f[6],
			Lat: lat, Lon: lon, ElevFt: elev, Size: f[10][0], Scheduled: f[11] == "1", TZ: f[12], Wiki: f[13],
		}
		db.all = append(db.all, a)
	}
	// ICAO keys win over IATA/local collisions; bigger airports win among IATA.
	sort.SliceStable(db.all, func(i, j int) bool { return sizeRank(db.all[i]) < sizeRank(db.all[j]) })
	for _, a := range db.all {
		db.byCode[a.ICAO] = a
	}
	for _, k := range []func(*Airport) string{
		func(a *Airport) string { return a.IATA },
		func(a *Airport) string { return a.Local },
	} {
		for _, a := range db.all {
			if c := k(a); c != "" {
				if _, taken := db.byCode[c]; !taken {
					db.byCode[c] = a
				}
			}
		}
	}
	return db
}

func sizeRank(a *Airport) int {
	r := map[byte]int{'L': 0, 'M': 1, 'S': 2}[a.Size]
	if !a.Scheduled {
		r += 3
	}
	return r
}

// LookupAirport finds an airport by ICAO, IATA or national code.
func LookupAirport(code string) (Airport, bool) {
	a, ok := airports.byCode[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return Airport{}, false
	}
	return *a, true
}

// LargeAirports lists the large airports (the crawler's sweep list).
func LargeAirports() []Airport {
	var out []Airport
	for _, a := range airports.all {
		if a.Size == 'L' {
			out = append(out, *a)
		}
	}
	return out
}

// NearbyAirports returns up to n airports with scheduled service within
// maxMi statute miles of (lat, lon), nearest first, excluding skip.
func NearbyAirports(lat, lon, maxMi float64, n int, skip string) []Airport {
	type hit struct {
		a *Airport
		d float64
	}
	var hits []hit
	dLat := maxMi / 69.0
	for _, a := range airports.all {
		if a.ICAO == skip || !a.Scheduled || a.Size == 'S' || math.Abs(a.Lat-lat) > dLat {
			continue
		}
		if d := DistanceMi(lat, lon, a.Lat, a.Lon); d <= maxMi {
			hits = append(hits, hit{a, d})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].d < hits[j].d })
	var out []Airport
	for i := 0; i < len(hits) && i < n; i++ {
		out = append(out, *hits[i].a)
	}
	return out
}

// CountryName maps ISO 3166-1 alpha-2 to an English name.
func CountryName(iso string) string {
	if n, ok := names[iso]; ok {
		return n
	}
	return iso
}

// RegionName maps ISO 3166-2 (US-WA) to its name (Washington).
func RegionName(iso string) string { return names[iso] }

func loadNames() map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(gunzip("data/names.tsv.gz"))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "\t"); ok {
			m[k] = v
		}
	}
	return m
}

func gunzip(name string) io.Reader {
	b, err := dataFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		panic(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		panic(err)
	}
	return bytes.NewReader(out)
}

const earthMi = 3958.8

// DistanceMi is the great-circle distance in statute miles.
func DistanceMi(lat1, lon1, lat2, lon2 float64) float64 {
	return earthMi * centralAngle(lat1, lon1, lat2, lon2)
}

func centralAngle(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2 := lat1*rad, lat2*rad
	dp, dl := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

const rad = math.Pi / 180
