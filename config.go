package main

import (
	"log"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/eclipse/paho.golang/paho"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type brokerConfig struct {
	Brokers       []string `yaml:"brokers"`
	Topics        []string `yaml:"topics"`
	ClientId      string   `yaml:"client_id"`
	BrokerUrls    []*url.URL
	Subscriptions []paho.SubscribeOptions
}

type backendConfig struct {
	Mqtt               brokerConfig  `yaml:"mqtt"`
	Mode               string        `yaml:"mode"`
	LogFormat          string        `yaml:"log_format"`
	LogFile            string        `yaml:"log_file"`
	Fee                float64       `yaml:"fee"`
	Moneropay          string        `yaml:"moneropay"`
	MpayTimeout        time.Duration `yaml:"moneropay_timeout"`
	MpayHealthPollFreq time.Duration `yaml:"moneropay_health_poll_frequency"`
	PricePollFreq      time.Duration `yaml:"price_poll_frequency"`
	Currencies         []string      `yaml:"currencies"`
	Motd               string        `yaml:"motd"`
	StateTimeout       time.Duration `yaml:"state_timeout"`
	FinishTimeout      time.Duration `yaml:"finish_timeout"`
	FallbackPrice      float64       `yaml:"fallback_price"`
	FiatRates          map[string]float64
	Bind               string `yaml:"bind"`
}

func loadConfig() backendConfig {
	var cfg backendConfig
	if len(os.Args) < 2 {
		log.Fatal("Usage: ./atm-backend config.yaml")
	}
	file, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal("Failed to read config: ", err)
	}
	if err := yaml.Unmarshal(file, &cfg); err != nil {
		log.Fatal("Failed to unmarshal yaml: ", err)
	}

	f, err := os.OpenFile(cfg.LogFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		panic(err)
	}

	if cfg.LogFormat == "pretty" {
		zlog.Logger = zlog.Output(zerolog.ConsoleWriter{Out: f,
			TimeFormat: time.RFC3339})
	}

	for _, urlStr := range cfg.Mqtt.Brokers {
		u, err := url.Parse(urlStr)
		if err != nil {
			log.Fatal("Failed to parse broker URL.")
		}
		cfg.Mqtt.BrokerUrls = append(cfg.Mqtt.BrokerUrls, u)
	}

	for _, topic := range cfg.Mqtt.Topics {
		cfg.Mqtt.Subscriptions = append(cfg.Mqtt.Subscriptions, paho.SubscribeOptions{
			Topic: topic, QoS: 2, NoLocal: true})
	}

	rates, err := fetchEcbDaily()
	if err != nil {
		log.Fatalf("Failed to get fiat rates from ECB: %s", err.Error())
	}
	cfg.FiatRates = make(map[string]float64)
	for _, c := range cfg.Currencies {
		// XMR pairs are available for these currencies
		if c == "EUR" || c == "USD" {
			continue
		}
		if val, ok := rates[c]; ok {
			cRate, err := strconv.ParseFloat(val, 64)
			cfg.FiatRates[c] = cRate
			if err != nil {
				log.Fatal("Failed to convert rate string into float64: ", err)
			}
		}
	}

	return cfg
}
