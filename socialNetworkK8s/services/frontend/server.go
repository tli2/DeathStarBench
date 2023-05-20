package frontend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"strconv"

	geo "socialnetworkk8/services/geo/proto"
	profile "socialnetworkk8/services/profile/proto"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"socialnetworkk8/dialer"
	"socialnetworkk8/registry"
	"socialnetworkk8/tls"
	"socialnetworkk8/tracing"
	"github.com/opentracing/opentracing-go"
)

// Server implements frontend service
type Server struct {
	geoClient geo.GeoClient
	IpAddr    string
	Port      int
	record    bool
	Tracer    opentracing.Tracer
	Registry  *registry.Client
	p         *Perf
}

// Run the server
func (s *Server) Run() error {
	if s.Port == 0 {
		return fmt.Errorf("Server port must be set")
	}

	zerolog.SetGlobalLevel(zerolog.Disabled)

	log.Info().Msg("Initializing gRPC clients...")
	if err := s.initGeo("srv-geo"); err != nil {
		return err
	}

	s.p = MakePerf("hotelperf/k8s", "hotel")

	log.Info().Msg("Successfull")

	log.Trace().Msg("frontend before mux")
	mux := tracing.NewServeMux(s.Tracer)
	mux.Handle("/", http.FileServer(http.Dir("services/frontend/static")))
	mux.Handle("/echo", http.HandlerFunc(s.echoHandler))
	mux.Handle("/geo", http.HandlerFunc(s.geoHandler))
	mux.Handle("/saveresults", http.HandlerFunc(s.saveResultsHandler))
	mux.Handle("/pprof/cpu", http.HandlerFunc(pprof.Profile))
	mux.Handle("/startrecording", http.HandlerFunc(s.startRecordingHandler))

	log.Trace().Msg("frontend starts serving")

	tlsconfig := tls.GetHttpsOpt()
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: mux,
	}
	if tlsconfig != nil {
		log.Info().Msg("Serving https")
		srv.TLSConfig = tlsconfig
		return srv.ListenAndServeTLS("x509/server_cert.pem", "x509/server_key.pem")
	} else {
		log.Info().Msg("Serving https")
		return srv.ListenAndServe()
	}
}

func (s *Server) initGeo(name string) error {
	conn, err := dialer.Dial(
		name,
		s.Registry.Client,
		dialer.WithTracer(s.Tracer),
	)
	if err != nil {
		return fmt.Errorf("dialer error: %v", err)
	}
	s.geoClient = geo.NewGeoClient(conn)
	return nil
}

func (s *Server) echoHandler(w http.ResponseWriter, r *http.Request) {
	if s.record {
		defer s.p.TptTick(1.0)
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")

	log.Trace().Msg("starts echoHandler")

	// grab locale from query params or default to en
	msg := r.URL.Query().Get("msg")

	log.Info().Msg(fmt.Sprintf("echoHandler msg %v", msg))

	res := map[string]interface{}{
		"message": "Echo: " + msg,
	}

	json.NewEncoder(w).Encode(res)
}

func (s *Server) geoHandler(w http.ResponseWriter, r *http.Request) {
	if s.record {
		defer s.p.TptTick(1.0)
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	ctx := r.Context()

	sLat, sLon := r.URL.Query().Get("lat"), r.URL.Query().Get("lon")
	if sLat == "" || sLon == "" {
		http.Error(w, "Please specify location params", http.StatusBadRequest)
		return
	}
	Lat, _ := strconv.ParseFloat(sLat, 32)
	lat := float32(Lat)
	Lon, _ := strconv.ParseFloat(sLon, 32)
	lon := float32(Lon)

	geores, err := s.geoClient.Nearby(ctx, &geo.Request{
		Lat: lat,
		Lon: lon,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	str := "Geo!"

	res := map[string]interface{}{
		"message": str,
		"length": strconv.Itoa(len(geores.HotelIds)),
	}

	json.NewEncoder(w).Encode(res)
}

func (s *Server) startRecordingHandler(w http.ResponseWriter, r *http.Request) {

	s.record = true

	w.Header().Set("Access-Control-Allow-Origin", "*")

	log.Info().Msg("Start recording!")

	str := "Started recording!"

	reply := map[string]interface{}{
		"message": str,
	}

	json.NewEncoder(w).Encode(reply)
}

func (s *Server) saveResultsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	s.p.Done()

	str := "Done!"

	res := map[string]interface{}{
		"message": str,
	}

	json.NewEncoder(w).Encode(res)
}

// return a geoJSON response that allows google map to plot points directly on map
// https://developers.google.com/maps/documentation/javascript/datalayer#sample_geojson
func geoJSONResponse(hs []*profile.Hotel) map[string]interface{} {
	fs := []interface{}{}

	for _, h := range hs {
		fs = append(fs, map[string]interface{}{
			"type": "Feature",
			"id":   h.Id,
			"properties": map[string]string{
				"name":         h.Name,
				"phone_number": h.PhoneNumber,
			},
			"geometry": map[string]interface{}{
				"type": "Point",
				"coordinates": []float32{
					h.Address.Lon,
					h.Address.Lat,
				},
			},
		})
	}

	return map[string]interface{}{
		"type":     "FeatureCollection",
		"features": fs,
	}
}

func checkDataFormat(date string) bool {
	if len(date) != 10 {
		return false
	}
	for i := 0; i < 10; i++ {
		if i == 4 || i == 7 {
			if date[i] != '-' {
				return false
			}
		} else {
			if date[i] < '0' || date[i] > '9' {
				return false
			}
		}
	}
	return true
}
