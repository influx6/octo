'use strict';

var expect  = require('chai').expect;
const octo = require("../octojs.js")

describe("Given the need to connect to a Octo.MultiProtocolServer", function(){

  const auth = octo.AuthCredentials("XScheme", "Rack", "4343121-GU", "Teddybear")
  const httpAttr = octo.Attr("http://localhost:5060", [], true)
  const socketAttr = octo.Attr("http://localhost:6060", [], true)

  // it("When giving the Addr of a HTTP Protocol", function(){
  //     const http = new octo.HTTPClient(auth, httpAttr, function(data, tx, res){
  //       var answers = data.toString().split("\r\n").filter(function(item){
  //         return item.length !== 0
  //       })
  //
  //       expect(answers.length).to.equal(2)
  //       expect(answers[0]).to.equal("DEX")
  //       expect(answers[1]).to.equal("DUMP")
  //     })
  //
  //     http.Do(octo.BufferMessage([
  //       {"name": "REX", "data": null},
  //       {"name": "PUMP", "data": null},
  //     ]))
  // })

  it("When giving the Addr of a Websocket Protocol", function(){
    const socket = new octo.WebsocketClient(auth, socketAttr, {
      error: function(err){
        console.log("Error: ", err)
      },
      data: function(data, tx, res){
        console.log("Data: ", data.toString())
        var answers = data.toString().split("\r\n").filter(function(item){
          return item.length !== 0
        })

        expect(answers.length).to.equal(2)
        expect(answers[0]).to.equal("DEX")
        expect(answers[1]).to.equal("DUMP")
      },
    });

    socket.on("end", function(data){
      console.log("Ending data: ", data)
    });

    socket.on("data", function(data){
      console.log("Logging data: ", data)
    });

    socket.Do(octo.BufferMessage([
      {"name": "REX", "data": null},
      {"name": "PUMP", "data": null},
    ]));
  });

})
