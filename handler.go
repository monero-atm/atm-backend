package main

import (
	"encoding/json"

	"github.com/rs/zerolog/log"
	"gitlab.com/openkiosk/proto"
)

func handleEvents(payload []byte) {
	var m proto.Event
	if err := json.Unmarshal(payload, &m); err != nil {
		log.Error().Err(err).Str("payload", string(payload)).Msg("Message could not be parsed")
	}
	okUpdate <- m
}
