package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

type xmrPrice struct {
	Amount float64 `json:"amount"`
	Short  string  `json:"short"`
}

type priceUpdate struct {
	Currencies []xmrPrice `json:"currencies"`
}

var krakenUrl = "https://api.kraken.com/0/public/Ticker?pair=XMR"

type krakenPair struct {
	Result struct {
		XmrEur struct {
			C []string `json:"c"`
		} `json:"XXMRZEUR"`
		XmrUsd struct {
			C []string `json:"c"`
		} `json:"XXMRZUSD"`
	} `json:"result"`
}

func getKrakenRate(currencyShort string) (float64, error) {
	req, err := http.NewRequest("GET", krakenUrl+currencyShort, nil)
	if err != nil {
		return 0, err
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, err
	}
	var kp krakenPair
	if err := json.NewDecoder(resp.Body).Decode(&kp); err != nil {
		return 0, err
	}
	var rate string
	if currencyShort == "EUR" {
		rate = kp.Result.XmrEur.C[0]
	} else if currencyShort == "USD" {
		rate = kp.Result.XmrUsd.C[0]
	} else {
		return 0, fmt.Errorf("non-existent XMR pair on Kraken")
	}
	return strconv.ParseFloat(rate, 64)
}

// Kraken only has XMR/EUR and XMR/USD pairs. When another fiat currency is
// specified, this function will calculate the value based on the daily rate
// provided by European Central Bank. "fiatEurRate" contains this rate.
// It's ignored when "currency" is set to EUR or USD.
func getXmrPrice(currencies []string, fiatRates map[string]float64, fee float64) (priceUpdate, error) {
	var pu priceUpdate

	// Get EUR rate
	eurRate, err := getKrakenRate("EUR")
	if err != nil {
		return pu, err
	}

	for _, c := range currencies {
		var xp xmrPrice
		if c == "USD" {
			usdRate, err := getKrakenRate("USD")
			if err != nil {
				return pu, err
			}
			xp.Amount = usdRate
			xp.Short = c
		} else if c == "EUR" {
			xp.Amount = eurRate
			xp.Short = "EUR"
		} else {
			if val, ok := fiatRates[c]; ok {
				xp.Amount = eurRate * val
				xp.Short = c
			} else {
				return pu, fmt.Errorf("ECB doesn't have a rate for this currency")
			}
		}
		xp.Amount *= (1 + fee)
		pu.Currencies = append(pu.Currencies, xp)
	}
	return pu, err
}

func pricePoll(currencies []string, fiatRates map[string]float64, fee float64) {
	pause := false
	for {
		select {
		case p := <-pricePause:
			pause = p
		case <-time.After(cfg.PricePollFreq):
			if !pause {
				prices, err := getXmrPrice(currencies, fiatRates, fee)

				if err != nil {
					log.Error().Err(err).Msg("Failed to get XMR price")
				} else {
					priceEvent <- prices
				}
			}
		}
	}
}
