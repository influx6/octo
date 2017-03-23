"use strict";

const http = require("http")
const url = require("url")
const websocket = require("websocket-stream")


// HTTPAttr returns a default value object which defines the necessary
// properties that are necessary for using the http object.
function HTTPAttr(){
	return {
		addr: "localhost:3000",
		headers: {},
		clusters: [],
		maxDrops: 10,
		maxReconnects: 10,
		authenticate: false,
	}
}

// Octo defines a function which returns a new instance of an object
// which allows making request to a underlying octo http server.
class Octo {

	constructor(attr) {
	   if(attr === undefined || typeof attr == 'undefined'){
	   	throw new Error("Expected to recieve attribute config")
	   }

	   if(!("addr" in attr)) {
	   	 throw new Error("Expected 'addr' field in attribute/config object");
	   }

	  this.attr = attr;
	  this.servers = [];
	  this.current = null;
	}

	// Octo.getNextServer returns the next available working server.
	prepareServers(){
	  for(var index in this.attr.clusters){
		  this.servers.push({ path: url.parse(this.attr.clusters[index]), reconns: 0, drops: 0, connected: false})
	  }

	  this.servers.push({ path: url.parse(this.attr.addr), reconns: 0, drops: 0, connected: false})
	}

	// Octo.getNextServer returns the next available working server.
	getNextServer(){
		for(var index in this.servers){
			var item = this.servers[index]

			if(item.reconns >= this.attr.maxReconnects){
				this.servers.push(this.servers.shift())
				continue
			}

			if(item.drops >= this.attr.maxDrops){
				this.servers.push(this.servers.shift())
				continue
			}

			this.current = item
			return
		}

		throw new Error("No Available/Usable server found")
	}
}


// HTTP defines a function which returns a new instance of an object
// which allows making request to a underlying octo http server.
class HTTP extends Octo {

	constructor(authCredentials, attr){
		this.auth_header = authCredentials
		this.agent = new http.Agent({keepAlive: true});

		super(attr)
	}

	// OctoHTTP.do calls the request to be made for a request with the data to
	// be delivered with and the callback to be called on the request.
	Do(data, callback, deliveryCallback){
		if(this.current == null || this.current == undefined){
			this.getNextServer();
		};

		var req = http.request({
			method: "POST",
			port: this.current.path.port,
			hostname: this.current.path.hostname,
			header: {"authorization": this.auth_header},
		});

		req.end(data, deliveryCallback);

		var incoming = []
		req.on("data", function(chunk) {incoming.push(chunk); });
		req.on("end", function(){
			callback(incoming, this)
		})
	}
}

// Websocket defines a function which returns a new instance of an object
// which allows making request to a underlying octo http server.
class Websocket extends Octo {

	constructor(authCredentials, attr){
		this.auth_header = authCredentials

		super(attr)
	}

	// OctoHTTP.do calls the request to be made for a request with the data to
	// be delivered with and the callback to be called on the request.
	Do(data, callback){
		if(this.current == null || this.current == undefined){
			this.getNextServer()
		}

		this.socket

	}
}


module.exports = {
	Octo: Octo,
	HTTPClient: HTTP,
	WebsocketClient: Websocket,
}
