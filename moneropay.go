package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"gitlab.com/moneropay/go-monero/walletrpc"
	mpay "gitlab.com/moneropay/moneropay/v2/pkg/model"
)

func mpayTransfer(amount uint64, address string) (*mpay.TransferPostResponse, error) {
	endpoint, err := url.JoinPath(cfg.Moneropay, "/transfer")
	if err != nil {
		return nil, err
	}
	reqData := mpay.TransferPostRequest{
		Destinations: []walletrpc.Destination{
			{Amount: amount, Address: strings.TrimSpace(address)},
		}}
	b := new(bytes.Buffer)
	if err := json.NewEncoder(b).Encode(reqData); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", endpoint, b)
	if err != nil {
		return nil, err
	}
	cl := &http.Client{Timeout: cfg.MpayTimeout}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		var errResp mpay.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			return nil, err
		}
		return nil, errors.New(errResp.Message)
	}
	var respData mpay.TransferPostResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, err
	}
	return &respData, nil
}

func mpayHealth() (*mpay.HealthResponse, error) {
	endpoint, err := url.JoinPath(cfg.Moneropay, "/health")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	cl := &http.Client{Timeout: cfg.MpayTimeout}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	var respData mpay.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, err
	}
	return &respData, nil
}

func mpayHealthPoll() {
	pause := false
	for {
		select {
		case p := <-mpayHealthPause:
			pause = p
		case <-time.After(cfg.MpayHealthPollFreq):
			if !pause {
				isHealthy := false
				healthResp, err := mpayHealth()
				if err != nil {
					log.Error().Err(err).Msg("Failed to get MoneroPay health status")
				} else {
					if healthResp.Status == 200 {
						isHealthy = true
					} else {
						log.Info().Int("status", healthResp.Status).Msg("MoneroPay health is degraded")
					}
				}
				if err := sendToFrontend(update{Event: "mpay_health", Data: isHealthy}); err != nil {
					log.Error().Err(err).Msg("Failed to send MoneroPay health status update")
				}
				log.Info().Bool("healthy", isHealthy).Msg("Moneropay Update")
			}
		}
	}
}
