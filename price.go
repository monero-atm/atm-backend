package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/monero-atm/pricefetcher"
	"github.com/rs/zerolog/log"
)

type xmrPrice struct {
	Amount float64 `json:"amount"`
	Short  string  `json:"short"`
}

type priceUpdate struct {
	Currencies []xmrPrice `json:"currencies"`
}

// Only XMR/EUR and XMR/USD pairs are available. When another fiat currency is
// specified, this function will calculate the value based on the daily rate
// provided by European Central Bank. "fiatEurRate" contains this rate.
// It's ignored when "currency" is set to EUR or USD.
func getXmrPrice(currencies []string, fiatRates map[string]float64, fee float64) (priceUpdate, error) {
	var pu priceUpdate

	cl := &http.Client{Timeout: 5 * time.Second}
	fetcher := pricefetcher.New(cl)
	// Get EUR rate
	eurRate, source, err := fetcher.FetchXMRPrice("EUR")
	if err != nil {
		return pu, err
	} else {
		log.Info().Float64("rate", eurRate).Str("source", source).Msg("Got XMR/EUR rate")
	}

	for _, c := range currencies {
		var xp xmrPrice
		if c == "USD" {
			usdRate, source, err := fetcher.FetchXMRPrice("USD")
			if err != nil {
				return pu, err
			} else {
				log.Info().Float64("rate", usdRate).Str("source", source).Msg("Got XMR/USD rate")
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

	// Fetch the price for the first time without the wait.
	prices, err := getXmrPrice(currencies, fiatRates, fee)

	if err != nil {
		log.Error().Err(err).Msg("Failed to get XMR price")
	} else {
		priceEvent <- prices
	}

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
