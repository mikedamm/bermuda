package geo

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
)

// ring is one polygon ring or polyline in degrees, with its bounding box.
type ring struct {
	lon, lat               []float64
	minLon, maxLon, minLat float64
	maxLat                 float64
}

type feature []ring

type mapLayer struct {
	name     string
	polygon  bool
	features []feature
}

var basemap = loadBasemap()

func loadBasemap() map[string]mapLayer {
	b, err := io.ReadAll(gunzip("data/basemap.bin.gz"))
	if err != nil || !bytes.HasPrefix(b, []byte("WVBM\x01")) {
		panic("geo: bad basemap data")
	}
	r := bytes.NewReader(b[5:])
	uv := func() int {
		v, err := binary.ReadUvarint(r)
		if err != nil {
			panic(err)
		}
		return int(v)
	}
	sv := func() int64 {
		v, err := binary.ReadVarint(r)
		if err != nil {
			panic(err)
		}
		return v
	}
	out := map[string]mapLayer{}
	for range uv() {
		kind, _ := r.ReadByte()
		name := make([]byte, uv())
		if _, err := io.ReadFull(r, name); err != nil {
			panic(err)
		}
		l := mapLayer{name: string(name), polygon: kind == 0}
		nf := uv()
		l.features = make([]feature, 0, nf)
		for range nf {
			nr := uv()
			f := make(feature, 0, nr)
			for range nr {
				np := uv()
				rg := ring{lon: make([]float64, np), lat: make([]float64, np),
					minLon: math.Inf(1), maxLon: math.Inf(-1), minLat: math.Inf(1), maxLat: math.Inf(-1)}
				var x, y int64
				for i := range np {
					x += sv()
					y += sv()
					lo, la := float64(x)/1e4, float64(y)/1e4
					rg.lon[i], rg.lat[i] = lo, la
					rg.minLon, rg.maxLon = math.Min(rg.minLon, lo), math.Max(rg.maxLon, lo)
					rg.minLat, rg.maxLat = math.Min(rg.minLat, la), math.Max(rg.maxLat, la)
				}
				f = append(f, rg)
			}
			l.features = append(l.features, f)
		}
		out[l.name] = l
	}
	return out
}
