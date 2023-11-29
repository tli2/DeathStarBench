package main

import (
	"os"
	"time"
	"socialnetworkk8/services/home"
	"socialnetworkk8/tune"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"runtime/debug"
)

func main() {
	debug.SetGCPercent(-1)
	tune.Init()
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).With().Timestamp().Caller().Logger()
	log.Info().Msg("Creating Home server...")
	srv := home.MakeHomeSrv()
	log.Info().Msg("Starting Home server...")
	log.Fatal().Msg(srv.Run().Error())
}
