package main

import (
	"context"
	"fmt"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/rs/zerolog/log"
)

func onConnectionUp(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
	log.Info().Msg("MQTT connection up.")
	if _, err := cm.Subscribe(context.Background(), &paho.Subscribe{
		Subscriptions: cfg.Mqtt.Subscriptions,
	}); err != nil {
		log.Error().Err(err).Msg("Failed to subscribe. This is likely to mean no messages will be received.")
		return
	}
	log.Info().Msg("MQTT subscription made.")
}

func onConnectionError(err error) {
	log.Error().Err(err).Msg("Error whilst attempting connection.")
}

func onClientError(err error) {
	log.Error().Err(err).Msg("Server requested disconnect.")
}

func onServerDisconnect(d *paho.Disconnect) {
	if d.Properties != nil {
		log.Warn().Str("reason", d.Properties.ReasonString).Msg("Server requested disconnect.")
	} else {
		log.Warn().Str("reason_code", string(d.ReasonCode)).Msg("Server requested disconnect.")
	}
}

func connectToBroker() *autopaho.ConnectionManager {
	cliCfg := autopaho.ClientConfig{
		BrokerUrls:     cfg.Mqtt.BrokerUrls,
		OnConnectionUp: onConnectionUp,
		OnConnectError: onConnectionError,
		ClientConfig: paho.ClientConfig{
			ClientID: cfg.Mqtt.ClientId,
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					log.Debug().Str("topic", pr.Packet.Topic).
						Str("payload", string(pr.Packet.Payload)).Msg("")
					handleEvents(pr.Packet.Payload)
					return true, nil
				},
			},
			OnClientError:      onClientError,
			OnServerDisconnect: onServerDisconnect,
		},
	}

	// Connect to the broker.
	cm, err := autopaho.NewConnection(context.Background(), cliCfg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to the broker")
	}
	return cm
}

func cmd(broker *autopaho.ConnectionManager, topic, cmd string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := broker.AwaitConnection(ctx); err != nil { // Should only happen when context is cancelled
		log.Error().Err(err).Msg("AwaitConnection")
		return
	}

	pr, err := broker.Publish(context.Background(), &paho.Publish{
		QoS:     2,
		Topic:   topic,
		Payload: []byte(fmt.Sprintf(`{"cmd": "%s"}`, cmd)),
	})
	if err != nil {
		log.Error().Err(err).Msg("Error publishing.")
		return
	} else if pr.ReasonCode != 0 && pr.ReasonCode != 16 { // 16 = Server received message but there are no subscribers
		log.Warn().Int("reason_code", int(pr.ReasonCode)).Msg("")
		return
	}
	log.Info().Str("state", cmd).Msg("Sent message: state change.")
}
