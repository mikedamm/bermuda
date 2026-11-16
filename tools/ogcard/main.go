// Command ogcard writes the HTML for the default social share card (1200x630).
// Render it to JPEG with a headless browser; see README "Share image".
//
//	go run ./tools/ogcard > /tmp/og.html
package main

import (
	"fmt"
	"os"

	"github.com/mikedamm/bermuda/internal/geo"
)

func main() {
	ap := func(c string) geo.LL { a, _ := geo.LookupAirport(c); return geo.LL{Lat: a.Lat, Lon: a.Lon} }
	routes := [][2]string{{"KSEA", "KJFK"}, {"KLAX", "EGLL"}, {"KSFO", "RJTT"}, {"KDFW", "MMUN"}, {"KORD", "KDEN"}, {"KATL", "KMIA"}, {"KSEA", "PANC"}, {"KBOS", "LFPG"}}
	spec := geo.MapSpec{W: 700, H: 630}
	for _, r := range routes {
		spec.Routes = append(spec.Routes, geo.MapRoute{From: ap(r[0]), To: ap(r[1]), Flown: true, Weight: 0.8})
	}
	c := geo.LL{Lat: 42, Lon: -80}
	spec.Center, spec.RadiusMi = &c, 3300
	m := geo.RenderMap(spec)
	css, _ := os.ReadFile("internal/web/static/site.css")
	fmt.Printf(`<!doctype html><html><head><meta charset="utf-8"><style>%s
html,body{margin:0;width:1200px;height:630px;overflow:hidden;background:#f1f4f8}
.card{position:relative;width:1200px;height:630px;display:grid;grid-template-columns:520px 680px}
.copy{padding:72px 0 0 72px}
.copy img{width:300px;height:auto}
.copy h1{margin-top:56px;font-size:58px;line-height:1.02;letter-spacing:-.04em;font-weight:800;max-width:9ch}
.copy p{margin-top:22px;font-size:24px;color:#24456f;max-width:19ch;line-height:1.35}
.globe{position:relative;margin:0}
.globe .map-svg{position:absolute;inset:0;width:100%%;height:100%%}
.globe .m-space{fill:#f1f4f8}
</style></head><body><div class="card"><div class="copy"><img src="wingview-header.svg" alt=""><h1>Where is that flight right now?</h1><p>Live flight status, aircraft history and airport activity.</p></div><div class="globe">%s</div></div></body></html>`, css, m.SVG)
}
