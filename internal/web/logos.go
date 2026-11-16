package web

// Operator logos live on the CDN as {base}/{ICAO}/icon.png: 50×50 marks on
// a transparent ground, one for every ICAO-assigned operator. The server
// never asks the CDN whether one exists (its bot protection answers scripted
// requests with 404s); a well-formed designator is enough.

import "regexp"

// logoCode is an ICAO airline designator, the form opCode hands over: the
// API's operator code, or the prefix of a callsign like UAL1275.
var logoCode = regexp.MustCompile(`^[A-Z]{3}$`)

// logoURL is an operator's logo, or "" without a logo base or a designator.
func (s *Server) logoURL(code string) string {
	if s.logoBase == "" || !logoCode.MatchString(code) {
		return ""
	}
	return s.logoBase + "/" + code + "/icon.png"
}
