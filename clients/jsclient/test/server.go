package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/influx6/octo"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/transmission"
	"github.com/influx6/octo/transmission/http"
	"github.com/influx6/octo/transmission/websocket"
	"github.com/influx6/octo/utils"
)

var (
	pocket = octo.AuthCredential{
		Scheme: "XScheme",
		Key:    "Rack",
		Token:  "4343121-GU",
		Data:   []byte("Teddybear"),
	}
)

type mockSystem struct{}

// Authenticate authenticates the provided credentials and implements
// the octo.Authenticator interface.
func (mockSystem) Authenticate(cred octo.AuthCredential) error {
	if cred.Scheme != pocket.Scheme {
		return errors.New("Scheme does not match")
	}

	if cred.Key != pocket.Key {
		return errors.New("Key does not match")
	}

	if cred.Token != pocket.Token {
		return errors.New("Token does not match")
	}

	return nil
}

// Serve handles the processing of different requests coming from the outside.
func (mockSystem) Serve(message []byte, tx transmission.Stream) error {
	fmt.Printf("Message: %+q\n", message)

	commands, err := utils.ToCommands(message)
	if err != nil {
		return err
	}

	var res bytes.Buffer

	for _, command := range commands {
		switch command.Name {
		case "PUMP":
			res.WriteString("DUMP\r\n")
			continue
		case "REX":
			res.WriteString("DEX\r\n")
		default:
			return errors.New("Invalid Command")
		}
	}

	return tx.Send(res.Bytes(), true)
}

func main() {
	var system mockSystem

	instruments := instruments.Instrument(instruments.InstrumentAttr{
		Log: mock.NewLogger(),
	})

	httpServer := http.New(instruments, http.BasicAttr{
		Addr:         "127.0.0.1:5060",
		Authenticate: true,
		Credential:   pocket,
	})

	socketServer := websocket.New(instruments, websocket.SocketAttr{
		Authenticate: true,
		Addr:         "127.0.0.1:6060",
	})

	if err := httpServer.Listen(system); err != nil {
		log.Fatalf("Failed to start http server: %+q", err)
	}

	if err := socketServer.Listen(system); err != nil {
		log.Fatalf("Failed to start http server: %+q", err)
	}

	defer socketServer.Close()
	defer httpServer.Close()

	log.Printf("HTTP Server started @ %+q", httpServer.Attr.Addr)
	log.Printf("Websocket Server started @ %+q", socketServer.Attr.Addr)

	// Listen for an interrupt signal from the OS.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan
}
