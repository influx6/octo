"use strict";

const http = require("http")
const url = require("url")
const websocket = require("websocket-stream")

// Message returns a Buffer version of the jsonified object.
function Message(obj){
	return JSON.stringify(obj)
}

// BufferMessage returns a Buffer version of the jsonified object.
function BufferMessage(obj){
	return new Buffer(JSON.stringify(obj))
}

// AuthCredentials returns a default value object which defines the necessary
// properties that are necessary for using the http object.
function AuthCredentials(scheme, key, token, data){
	return {
		scheme: scheme,
		key: key,
		token: token,
		data: data,
	}
}

// ParseAuthCredentialsAsHeader returns the provided credentials in a string which can be
// placed in a 'Authorization' Header.
function ParseAuthCredentialsAsHeader(auth_credentail) {
	return auth_credentail.scheme+" "+auth_credentail.key+":"+auth_credentail.token+":"+JSON.stringify(auth_credentail.data)
}

// ParseAuthCredentialsAsCommand returns the provided credentials in a structure which
// meets the octo.Command struct.
function ParseAuthCredentialsAsCommand(auth_credentail) {
	return JSON.stringify({
		"name":"AUTHCRED",
		"data": [auth_credentail],
	})
}

// Attr returns a default value object which defines the necessary
// properties that are necessary for using the http object.
function Attr(addr, clusters, authenticate, headers, drops, recons){
	return {
		addr: addr || "localhost:3000",
		headers: headers || {},
		clusters: clusters || [],
		maxDrops: drops || 10,
		maxReconnects: recons || 10,
		authenticate: authenticate || false,
	}
}

// Octo defines a function which returns a new instance of an object
// which allows making request to a underlying octo http server.
class Octo {

	constructor(attr, callback) {
	   if(attr === undefined || typeof attr == 'undefined'){
	   	throw new Error("Expected to recieve attribute config")
	   }

	   if(!("addr" in attr)) {
	   	 throw new Error("Expected 'addr' field in attribute/config object");
	   }

	  this.attr = attr;
	  this.servers = [];
	  this.current = null;
		this.callback = callback
		this.prepareServers()
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

	constructor(auth_credentail, attr, callback){
		super(attr, callback)
		this.credentials = auth_credentail
	}

	// OctoHTTP.do calls the request to be made for a request with the data to
	// be delivered with and the callback to be called on the request.
	Do(data, deliveryCallback){
		if(this.current == null || this.current == undefined){
			this.getNextServer();
		};

		var self = this;

		var req = http.request({
			method: "POST",
			path: "/",
			port: this.current.path.port,
			hostname: this.current.path.hostname,
			headers: {
				"Authorization": ParseAuthCredentialsAsHeader(this.credentials),
				"Content-Type": "application/json",
			},
		}, function(res){
			var incoming = []
			res.on("data", function(chunk) {incoming.push(chunk); });
			res.on("end", function(){
				self.callback.call(self, incoming, res, self)
			})
		});

		req.end(data, deliveryCallback);

	}
}

// Websocket defines a function which returns a new instance of an object
// which allows making request to a underlying octo http server.
class Websocket extends Octo {

	constructor(auth_credentail, attr, callback, connectionCallback){
		super(attr, callback)
		this.socket = null
		this.onConnect = connectionCallback
		this.credentials = auth_credentail
	}

	Do(data){
		if(this.current == null || this.current == undefined){
			this.getNextServer();
		};

		if(this.current.path.protocol !== "ws:" && this.current.path.protocol !== "wss:"){
			throw new Error("Invalid protocol for socket address");
		};


		if(this.socket === null){
			this.socket = websocket(this.current.path.toString())

			this.socket.on("error", function(err){
				console.log("SocketError: ", err)
			});

			this.socket.on("close", function(){
				this.socket = null;
			});

			this.socket.on("data", function(o){
				this.callback(o);
			});
		}

		this.socket.write(data)
	}
}


module.exports = {
	Octo: Octo,
	Attr: Attr,
	HTTPClient: HTTP,
	Message: Message,
	BufferMessage: BufferMessage,
	WebsocketClient: Websocket,
	AuthCredentials: AuthCredentials,
	ParseAuthCredentialsAsHeader: ParseAuthCredentialsAsHeader,
	ParseAuthCredentialsCommand: ParseAuthCredentialsAsCommand,
}
