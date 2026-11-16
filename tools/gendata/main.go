// Command gendata builds the reference data embedded in the site binary from
// public sources. Run it by hand when the sources should be refreshed; the
// output is committed under internal/geo/data.
//
//	go run ./tools/gendata -raw /path/to/downloads -out internal/geo/data
//
// Expected downloads in -raw (see README "Reference data"):
//
//	ourairports.csv, countries.csv, regions.csv   OurAirports (public domain)
//	mwgg_airports.json                              mwgg/Airports (MIT) — time zones only
//	ne_50m_land.geojson, ne_50m_lakes.geojson,
//	ne_50m_borders.geojson, ne_50m_states.geojson  Natural Earth (public domain)
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	raw := flag.String("raw", "", "directory holding the downloaded sources")
	out := flag.String("out", "internal/geo/data", "output directory")
	flag.Parse()
	if *raw == "" {
		log.Fatal("-raw is required")
	}
	must(writeAirports(*raw, *out))
	must(writeNames(*raw, *out))
	must(writeBasemap(*raw, *out))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// ---------- airports ----------

var icaoRe = regexp.MustCompile(`^[A-Z]{4}$`)

type mwggAirport struct {
	ICAO string  `json:"icao"`
	TZ   string  `json:"tz"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

func writeAirports(raw, out string) error {
	tzs, err := loadTZ(filepath.Join(raw, "mwgg_airports.json"))
	if err != nil {
		return err
	}
	rows, err := readCSV(filepath.Join(raw, "ourairports.csv"))
	if err != nil {
		return err
	}
	head := index(rows[0])
	var lines []string
	seen := map[string]bool{}
	for _, r := range rows[1:] {
		get := func(k string) string { return strings.TrimSpace(r[head[k]]) }
		typ := get("type")
		iata := strings.ToUpper(get("iata_code"))
		sched := get("scheduled_service") == "yes"
		keep := typ == "large_airport" || typ == "medium_airport" ||
			(typ == "small_airport" && (iata != "" || sched))
		if !keep {
			continue
		}
		code := firstMatch(icaoRe, get("icao_code"), get("gps_code"), get("ident"))
		if code == "" {
			code = strings.ToUpper(get("ident"))
		}
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		lat, _ := strconv.ParseFloat(get("latitude_deg"), 64)
		lon, _ := strconv.ParseFloat(get("longitude_deg"), 64)
		elev := get("elevation_ft")
		tz := tzs.lookup(code, get("ident"), lat, lon)
		local := strings.ToUpper(get("local_code"))
		if local == code {
			local = ""
		}
		wiki := strings.TrimPrefix(get("wikipedia_link"), "https://en.wikipedia.org/wiki/")
		if strings.Contains(wiki, "://") {
			wiki = ""
		}
		t := map[string]string{"large_airport": "L", "medium_airport": "M", "small_airport": "S"}[typ]
		s := "0"
		if sched {
			s = "1"
		}
		lines = append(lines, strings.Join([]string{
			code, iata, local, clean(get("name")), clean(get("municipality")), get("iso_region"), get("iso_country"),
			strconv.FormatFloat(lat, 'f', 5, 64), strconv.FormatFloat(lon, 'f', 5, 64), elev, t, s, tz, wiki,
		}, "\t"))
	}
	sort.Strings(lines)
	log.Printf("airports: %d rows", len(lines))
	return writeGz(filepath.Join(out, "airports.tsv.gz"), []byte(strings.Join(lines, "\n")+"\n"))
}

func clean(s string) string { return strings.NewReplacer("\t", " ", "\n", " ").Replace(s) }

func firstMatch(re *regexp.Regexp, vals ...string) string {
	for _, v := range vals {
		if v = strings.ToUpper(v); re.MatchString(v) {
			return v
		}
	}
	return ""
}

type tzIndex struct {
	byCode map[string]string
	pts    []mwggAirport
}

func loadTZ(path string) (tzIndex, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return tzIndex{}, err
	}
	var m map[string]mwggAirport
	if err := json.Unmarshal(b, &m); err != nil {
		return tzIndex{}, err
	}
	idx := tzIndex{byCode: map[string]string{}}
	for k, a := range m {
		if a.TZ == "" {
			continue
		}
		idx.byCode[strings.ToUpper(k)] = a.TZ
		idx.pts = append(idx.pts, a)
	}
	return idx, nil
}

// lookup finds a zone by code, else borrows the nearest known airport's zone
// within 250 km (zone borders rarely matter at that distance for airports).
func (t tzIndex) lookup(code, ident string, lat, lon float64) string {
	if z := t.byCode[code]; z != "" {
		return z
	}
	if z := t.byCode[strings.ToUpper(ident)]; z != "" {
		return z
	}
	best, bestD := "", 250.0
	for _, p := range t.pts {
		if math.Abs(p.Lat-lat) > 3 {
			continue
		}
		if d := km(lat, lon, p.Lat, p.Lon); d < bestD {
			best, bestD = p.TZ, d
		}
	}
	return best
}

func km(lat1, lon1, lat2, lon2 float64) float64 {
	r := math.Pi / 180
	a := math.Sin((lat2-lat1)*r/2)*math.Sin((lat2-lat1)*r/2) +
		math.Cos(lat1*r)*math.Cos(lat2*r)*math.Sin((lon2-lon1)*r/2)*math.Sin((lon2-lon1)*r/2)
	return 6371 * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// ---------- country and region names ----------

func writeNames(raw, out string) error {
	var lines []string
	for _, f := range []string{"countries.csv", "regions.csv"} {
		rows, err := readCSV(filepath.Join(raw, f))
		if err != nil {
			return err
		}
		h := index(rows[0])
		for _, r := range rows[1:] {
			lines = append(lines, r[h["code"]]+"\t"+clean(r[h["name"]]))
		}
	}
	sort.Strings(lines)
	log.Printf("names: %d rows", len(lines))
	return writeGz(filepath.Join(out, "names.tsv.gz"), []byte(strings.Join(lines, "\n")+"\n"))
}

// ---------- basemap ----------

// Binary layout (all integers varint, coordinates in 1e-4 degrees, zigzag
// deltas from the previous point of the same ring):
//
//	"WVBM" 0x01, layer count
//	per layer: kind (0 polygon, 1 line), name length + bytes, feature count
//	per feature: ring count; per ring: point count, then points
type geojson struct {
	Features []struct {
		Properties map[string]any `json:"properties"`
		Geometry   struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

type layer struct {
	name     string
	kind     byte
	features [][][][2]float64 // feature → rings/lines → points
}

func writeBasemap(raw, out string) error {
	specs := []struct {
		file, name string
		kind       byte
	}{
		{"ne_50m_land.geojson", "land", 0},
		{"ne_50m_lakes.geojson", "lakes", 0},
		{"ne_50m_borders.geojson", "borders", 1},
		{"ne_50m_states.geojson", "states", 1},
	}
	var buf []byte
	buf = append(buf, 'W', 'V', 'B', 'M', 1)
	buf = binary.AppendUvarint(buf, uint64(len(specs)))
	for _, s := range specs {
		l, err := readLayer(filepath.Join(raw, s.file), s.name, s.kind)
		if err != nil {
			return fmt.Errorf("%s: %w", s.file, err)
		}
		buf = append(buf, l.kind)
		buf = binary.AppendUvarint(buf, uint64(len(l.name)))
		buf = append(buf, l.name...)
		buf = binary.AppendUvarint(buf, uint64(len(l.features)))
		pts := 0
		for _, f := range l.features {
			buf = binary.AppendUvarint(buf, uint64(len(f)))
			for _, ring := range f {
				buf = binary.AppendUvarint(buf, uint64(len(ring)))
				var px, py int64
				for _, p := range ring {
					x, y := int64(math.Round(p[0]*1e4)), int64(math.Round(p[1]*1e4))
					buf = binary.AppendVarint(buf, x-px)
					buf = binary.AppendVarint(buf, y-py)
					px, py = x, y
					pts++
				}
			}
		}
		log.Printf("basemap %s: %d features, %d points", l.name, len(l.features), pts)
	}
	return writeGz(filepath.Join(out, "basemap.bin.gz"), buf)
}

func readLayer(path, name string, kind byte) (layer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return layer{}, err
	}
	var g geojson
	if err := json.Unmarshal(b, &g); err != nil {
		return layer{}, err
	}
	l := layer{name: name, kind: kind}
	for _, f := range g.Features {
		c := f.Geometry.Coordinates
		switch f.Geometry.Type {
		case "Polygon", "MultiLineString":
			var rings [][][2]float64
			if err := json.Unmarshal(c, &rings); err != nil {
				return l, err
			}
			if kind == 1 { // each line is its own feature
				for _, r := range rings {
					l.features = append(l.features, [][][2]float64{r})
				}
				continue
			}
			l.features = append(l.features, rings)
		case "MultiPolygon":
			var polys [][][][2]float64
			if err := json.Unmarshal(c, &polys); err != nil {
				return l, err
			}
			l.features = append(l.features, polys...)
		case "LineString":
			var line [][2]float64
			if err := json.Unmarshal(c, &line); err != nil {
				return l, err
			}
			l.features = append(l.features, [][][2]float64{line})
		}
	}
	return l, nil
}

// ---------- helpers ----------

func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReader(f))
	r.FieldsPerRecord = -1
	return r.ReadAll()
}

func index(head []string) map[string]int {
	m := map[string]int{}
	for i, h := range head {
		m[h] = i
	}
	return m
}

func writeGz(path string, b []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	if _, err := io.Copy(zw, strings.NewReader(string(b))); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}
