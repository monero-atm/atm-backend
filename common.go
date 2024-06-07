package main

import (
	"encoding/json"
	"time"
)

func sendToFrontend(u update) error {
	u.Timestamp = time.Now()
	updateBytes, err := json.Marshal(u)
	if err != nil {
		return err
	}
	outgoing <- updateBytes
	return nil
}
