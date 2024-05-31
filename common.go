package main

import "encoding/json"

func sendToFrontend(u update) error {
	updateBytes, err := json.Marshal(u)
	if err != nil {
		return err
	}
	outgoing <- updateBytes
	return nil
}
