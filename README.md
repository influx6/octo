Octo
====

[![Go Report Card](https://goreportcard.com/badge/github.com/influx6/octo)](https://goreportcard.com/report/github.com/influx6/octo)

Octo is a library providing a baseline architecture which is specifically for the creation of a service which is able to communicate through several underline transport protocol without change in the underline core messages sent and the logic used.

Octo is intended to provide a system where we can easily build a service capable of talking with others and vice-versa through any supported procotols (http, websocket, udp and tcp). These then allows us easily create higher level services, which can easily be talked to with by clients using any procotol supported by the server.

Install
-------

```bash
go get -u github.com/influx6/octo
```

Protocols
---------

Octo currently provides 4 implemented protocols with plans to expand the list as time goes on.

-	TCP *Server and Client Packages*
-	HTTP *Server and Client Packages*
-	UDP *Server and Client Packages*
-	Websocket *Server and Client Packages*
-	Quic (Pending)

Client Libraries
----------------

All supported protocols have Go based client libraries which allows connecting to given services and handle any internal operation private to these procotols. Octo also supports connections with the Http and Websocket protocols from NodeJS and Webbrowser through the [OctoJS](./octojs) package which allows connecting on javascript runtimes.

Example
-------

Below is demonstrated a codebase which showcases usage of the http and websocket server protocols for use with the OctoJS client library.

*Octo enforces no selective rules has to the contents of a message and it's format, although certain formats such as `json` are used underneath for internal logic between client and servers but users are free to create or use any message format needed and as decided on the server*

The sample uses a combination of JSON and text responses but users will easily be able to change this as suited.

-	Client Code(`index.html`\)

```html
<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
    <title></title>
  </head>
  <body>
    <h1> Websocket test </h1>
    <p>Open console in developer tools to see output</p>
    <script type="text/javascript" src="assets/octojs.js"></script>
    <script type="text/javascript">
    const octo = require("octojs");
    const auth = octo.AuthCredentials("XScheme", "Rack", "4343121-GU", "Teddybear")

    const httpAttr = octo.Attr("http://localhost:5060", [], auth)
    const socketAttr = octo.Attr("ws://localhost:6060", [], auth)

    console.log("HTTP:Attr: ", httpAttr)
    console.log("Websocket:Attr: ", socketAttr)

    const http = new octo.HTTPClient(httpAttr, function(data, tx, res){
      var answers = data.toString().split("\r\n").filter(function(item){
        return item.length !== 0
      })

      console.log("HTTP:Answers: ", answers)
    })

    http.Do(octo.BufferMessage([
      {"name": "REX", "data": null},
      {"name": "PUMP", "data": null},
    ]))

    const socket = new octo.WebsocketClient(socketAttr, {
      error: function(err){
        console.log("Error: ", err)
      },
      data: function(data, tx, res){
        var answers = data.toString().split("\r\n").filter(function(item){
          return item.length !== 0
        })

        console.log("Websocket:Answers: ", answers)
      },
    });

    socket.Do(octo.BufferMessage([
      {"name": "REX", "data": null},
      {"name": "PUMP", "data": null},
    ]));

    </script>
  </body>
</html>

```

-	Server Code

```go
package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	classicHttp "net/http"
	"os"
	"os/signal"

	"github.com/influx6/octo"
	"github.com/influx6/octo/consts"
	"github.com/influx6/octo/instruments"
	"github.com/influx6/octo/messages/jsoni"
	"github.com/influx6/octo/mock"
	"github.com/influx6/octo/streams/server"
	"github.com/influx6/octo/streams/server/http"
	"github.com/influx6/octo/streams/server/websocket"
)

var (
	pocket = octo.AuthCredential{
		Scheme: "XScheme",
		Key:    "Rack",
		Token:  "4343121-GU",
		Data:   "Teddybear",
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
func (mockSystem) Serve(message []byte, tx server.Stream) error {
	fmt.Printf("Message: %+q\n", message)

	cmds, err := jsoni.Parser.Decode(message)
	if err != nil {
		return err
	}

	commands, ok := cmds.([]jsoni.CommandMessage)
	if !ok {
		return consts.ErrParseError
	}
	
	// We personally want this example to bundle the response together, but this is not standard (nor is there a standard)
	// It can equally respond to each individual command seperately.
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

	instruments := instruments.Instruments(mock.NewLogger(), nil)

	httpServer := http.New(instruments, http.BasicAttr{
		Addr:         "127.0.0.1:5060",
		Authenticate: true,
		Auth:         pocket,
	})

	socketServer := websocket.New(instruments, websocket.SocketAttr{
		Authenticate: true,
		Addr:         "127.0.0.1:6060",
		OriginValidator: func(r *classicHttp.Request) bool {
			return true
		},
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
```
