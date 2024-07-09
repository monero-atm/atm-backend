package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"gitlab.com/moneropay/go-monero/walletrpc"
	mpay "gitlab.com/moneropay/moneropay/v2/pkg/model"
	"gitlab.com/openkiosk/proto"
)

type State int

const (
	Idle State = iota
	AddressIn
	MoneyIn
	TxInfo
)

type update struct {
	// keyword description of what happened
	Event     string      `json:"event"`
	Data      interface{} `json:"value"`
	Timestamp time.Time   `json:"timestamp"`
}

type sessionData struct {
	conn        *websocket.Conn
	broker      *autopaho.ConnectionManager
	state       State
	address     string
	fiatBalance map[string]int64
	xmr         uint64
	xmrPrices   map[string]float64
	err         error
	tx          *mpay.TransferPostResponse
}

var (
	session *sessionData

	// to signal end of websocket session on errors
	endSession chan struct{}

	// Updates from frontend
	incoming chan []byte

	// Updates to frontend
	outgoing chan []byte

	// OpenKiosk events
	okUpdate chan proto.Event

	priceEvent chan priceUpdate
	pricePause chan bool

	mpayHealthPause chan bool
)

var upgrader = websocket.Upgrader{} // use default options

type txinfoData struct {
	Tx     string `json:"tx"`
	Amount string `json:"amount"`
}

func main() {
	cfg = loadConfig()
	endSession = make(chan struct{})
	incoming = make(chan []byte)
	outgoing = make(chan []byte)
	priceEvent = make(chan priceUpdate)
	pricePause = make(chan bool)
	mpayHealthPause = make(chan bool)
	okUpdate = make(chan proto.Event)

	session = &sessionData{
		broker:      connectToBroker(),
		xmrPrices:   make(map[string]float64),
		fiatBalance: make(map[string]int64),
	}

	go session.appLogic()
	go pricePoll(cfg.Currencies, cfg.FiatRates, cfg.Fee)
	go mpayHealthPoll()

	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	http.HandleFunc("/ws", atmSessionHandler)
	log.Fatal().Err(http.ListenAndServe(cfg.Bind, nil)).Msg("Failed to bind")
}

func atmSessionHandler(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("Websocket update")
		return
	}
	session.conn = c
	cmd(session.broker, "codescannerd", "start")

	go session.handleIncoming()
	go session.handleOutgoing()
}

// Updates arriving from the frontend: cancel transaction, stop price update
func (s *sessionData) handleIncoming() {
	for {
		select {
		case <-endSession:
			log.Debug().Msg("Exited handleIncoming")
			return
		default:
			mt, message, err := s.conn.ReadMessage()
			if err != nil {
				log.Error().Err(err).Msg("Websocket read")
				endSession <- struct{}{}
				return
			}

			// Skip non-text messages
			if mt != 1 {
				continue
			}
			incoming <- message
		}
	}
}

// Updates to the frontend: money inserted, sent, backend error
func (s *sessionData) handleOutgoing() {
	for {
		select {
		case m := <-outgoing:
			err := s.conn.WriteMessage(1, m)
			if err != nil {
				log.Error().Err(err).Msg("Websocket write")
				endSession <- struct{}{}
				return
			}
		case <-endSession:
			log.Debug().Msg("Exited handleOutgoing")
			return
		}
	}
}

// Main application logic happens here.
// Make sense of all updates.
func (s *sessionData) appLogic() {
	for {
		select {
		case frontendUpdate := <-incoming:
			var front update
			if err := json.Unmarshal(frontendUpdate, &front); err != nil {
				log.Error().Err(err).Msg("Malformed frontend update")
				continue
			}
			log.Info().Str("type", front.Event).Msg("Received frontend event")
			switch front.Event {
			case "start":
				s.reset()
				s.state = AddressIn
				// Pause price updates and health checks
				pricePause <- true
				mpayHealthPause <- true
				log.Info().Msg("Began new transaction")
			case "moneyin":
				s.state = MoneyIn
				cmd(s.broker, "moneyacceptord", "start")
				cmd(s.broker, "codescannerd", "stop")
			case "txinfo":
				s.state = TxInfo
				cmd(s.broker, "moneyacceptord", "stop")

				fmt.Printf("%v\n", s.fiatBalance)
				// Calculate xmr given the rate and fiat
				var xmrFloat float64 = 0
				for currShort, balance := range s.fiatBalance {
					xf := float64(balance) / s.xmrPrices[currShort]
					log.Info().Float64("summing up", xf).Msg("")
					xmrFloat += xf
				}
				s.xmr = uint64(xmrFloat * 1000000000000)
				log.Info().Uint64("xmr", s.xmr).Float64("xmrFloat", xmrFloat).Msg("calc")
				s.tx, s.err = mpayTransfer(s.xmr, s.address)
				if s.err != nil {
					log.Error().Err(s.err).Msg("Failed to transfer")
					if err := sendToFrontend(update{Event: "error", Data: s.err.Error()}); err != nil {
						log.Error().Err(s.err).Msg("Failed to send to frontend")
					}
					continue
				}
				xmrString := walletrpc.XMRToDecimal(s.xmr)
				log.Info().Str("amount", xmrString).Str("address", s.address).Msg("Sent XMR")
				if err := sendToFrontend(update{
					Event: "txinfo", Data: txinfoData{
						Tx:     s.tx.TxHashList[0],
						Amount: xmrString,
					},
				}); err != nil {
					log.Error().Err(s.err).Msg("Failed to send to frontend")
				}
				s.reset()
				log.Info().Msg("Finalized transaction")
			case "cancel":
				s.reset()
				log.Info().Msg("Cancelled transaction")
			case "final":
				s.reset()
				log.Info().Msg("Finalized transaction")
			}
		case hardwareUpdate := <-okUpdate:
			log.Info().Str("type", hardwareUpdate.Event).Msg("")
			if hardwareUpdate.Event == "codescan" {
				log.Info().Str("data", fmt.Sprintf("%v", hardwareUpdate)).Msg("")
				data, err := proto.GetScanData(hardwareUpdate.Data)
				if err != nil {
					log.Error().Err(err).Msg("Failed to unmarshall scan data")
					continue
				}
				decoded, err := base64.StdEncoding.DecodeString(data.Scan)
				if err != nil {
					log.Error().Err(err).Msg("Failed to base64 decode scan data")
					continue
				}
				addr := parseAddress(string(decoded))
				if err := addressValidator(addr); err != nil {
					log.Error().Err(err).Msg("Invalid address received")
					if err := sendToFrontend(update{Event: "error", Data: err.Error()}); err != nil {
						log.Error().Err(err).Msg("Failed to send to frontend")
					}
				}
				s.address = addr
				if err := sendToFrontend(update{Event: "addressin", Data: addr}); err != nil {
					log.Error().Err(err).Msg("Failed to send to frontend")
				}

				// If this transaction began not by tapping the screen but by scanning QR
				if s.state == Idle {
					pricePause <- true
					mpayHealthPause <- true
					log.Info().Msg("Began new transaction")
				}
			}
			if hardwareUpdate.Event == "moneyin" {
				log.Info().Str("data", fmt.Sprintf("%v", hardwareUpdate)).Msg("")
				data, err := proto.GetMoneyinData(hardwareUpdate.Data)
				if err != nil {
					log.Error().Err(err).Msg("Failed to unmarshall scan data")
				}
				s.fiatBalance[data.Currency] += data.Amount
				if err := sendToFrontend(update{Event: "moneyin", Data: data}); err != nil {
					log.Error().Err(err).Msg("Failed to send to frontend")
				}
				fmt.Printf("fiat balance: %v\n", s.fiatBalance)
			}

		case price := <-priceEvent:
			for _, pc := range price.Currencies {
				s.xmrPrices[pc.Short] = pc.Amount
			}
			if err := sendToFrontend(update{Event: "price", Data: price}); err != nil {
				log.Error().Err(err).Msg("Failed to send to frontend")
			}
		}
	}
}

func (s *sessionData) reset() {
	// Reset all data from previous transaction
	s.state = Idle
	s.address = ""
	s.fiatBalance = make(map[string]int64)
	s.xmr = 0
	s.err = nil
	s.tx = nil

	// Enable price updates
	pricePause <- false
	mpayHealthPause <- false

	// Stop bill acceptor, enable QR code scanning
	cmd(s.broker, "moneyacceptord", "stop")
	cmd(s.broker, "codescannerd", "start")
}
