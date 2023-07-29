package main

import (
	"os"
	"time"
	"socialnetworkk8/services/post"
	"socialnetworkk8/tune"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"runtime/debug"
)

func main() {
	debug.SetGCPercent(-1)
	tune.Init()
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).With().Timestamp().Caller().Logger()
	log.Info().Msg("Creating Post server...")
	srv := post.MakePostSrv()
	log.Info().Msg("Starting Post server...")
	log.Fatal().Msg(srv.Run().Error())
}
