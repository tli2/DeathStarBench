package main

import (
	"os"
	"time"
	"socialnetworkk8/services/user"
	"socialnetworkk8/tune"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	tune.Init()
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}).With().Timestamp().Caller().Logger()
	log.Info().Msg("Creating serverg...")
	srv := user.MakeUserSrv()
	log.Info().Msg("Starting server...")
	log.Fatal().Msg(srv.Run().Error())
}
