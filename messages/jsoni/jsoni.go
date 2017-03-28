package jsoni

import (
	"github.com/influx6/octo"
)

// CommandMessage defines a type which recieves a command to be parsed for a giving
// json message from a endpoint.
type CommandMessage struct {
	Name string      `json:"name"`
	Data interface{} `json:"data"`
}

// AuthMessage defines a struct type which is defines the response expected for
// an authentication message.
type AuthMessage struct {
	Name string              `json:"name"`
	Data octo.AuthCredential `json:"data"`
}
