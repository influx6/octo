"use strict";

// const http = require("http")
const url = require("url")
const websocket = require("websocket-stream")
const request = require('request');

// These are sets of possible message headers.
const OK = "OK"
const AuthRequest = "AUTH"
const AuthResponse = "AUTHCRED"
const AuthGranted = "AuthGranted"
const AuthDenied = "AuthDenied"

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
		"name": AuthResponse,
		"data": auth_credentail,
	})
}

// Attr returns a default value object which defines the necessary
// properties that are necessary for using the http object.
function Attr(addr, clusters, auth, headers, drops, recons){
	return {
		auth: auth,
		addr: addr || "localhost:3000",
		headers: headers || {},
		clusters: clusters || [],
		maxDrops: drops || 10,
		maxReconnects: recons || 10,
		authenticate: auth ? true : false,
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

		 var callbacks = { data: null, error: null };

		 switch(GetType(callback)){
			 case "Function":
			 	callbacks.data = callback
				break

			 case "Object":
			  if(callback['data'] && GetType(callback.data) !== "Function"){
					throw new Error("data field must be a function")
				}

			  if(callback['error'] && GetType(callback.error) !== "Function"){
					throw new Error("error field must be a function")
				}

			 	callbacks = callback
				break
		 }

	  this.attr = attr;
	  this.servers = [];
	  this.current = null;
		this.callbacks = callbacks
		this.prepareServers()
	}

	prepareServers(){
	  for(var index in this.attr.clusters){
		  this.servers.push({
				path: url.parse(this.attr.clusters[index]),
				reconns: 0,
				drops: 0,
				connected: false,
			});
	  };

	  this.servers.push({
			path: url.parse(this.attr.addr),
			reconns: 0,
			drops: 0,
			connected: false,
		});
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
	constructor(attr, callback){
		super(attr, callback)
	}

	// OctoHTTP.do calls the request to be made for a request with the data to
	// be delivered with and the callback to be called on the request.
	Do(data, deliveryCallback){
		if(this.current == null || this.current == undefined){
			this.getNextServer();
		};

		var self = this;
		var headers = {
				"Content-Type": "application/json",
		};

		// console.log("Will Auth: ", GetType(self.attr.auth), IsNull(self.attr.auth), self.attr.authenticate)

		if(self.attr.authenticate && !IsNull(self.attr.auth)){
			// console.log("Adding Auth: ", GetType(self.attr.auth))
			switch(GetType(self.attr.auth)){
				case "String":
					headers["Authorization"] = self.attr.auth;
					break
				case "Object":
					headers["Authorization"] = ParseAuthCredentialsAsHeader(self.attr.auth);
					break
				default:
				  throw new Error("Invalid Auth Object Type");
			};
		};

		if(!IsNull(self.attr.headers)){
			// console.log("Headers: ", self.attr.headers)
			for(var key in self.attr.headers){
				var val = self.attr.headers[key];

				if(key === "Authorization" || key === "authorization"){
					return;
				};

				headers[key] = val;
			};
		};

		try {
			var req = request({
				method: "POST",
				headers: headers,
				url: this.current.path.href,
			}, function(err, res, body){
				if(err != null || err != undefined){
					self.current.connected = false

				  console.log("HTTP Request Error: ", err)
					self.current.drops++

					if(self.callbacks['error']){
						self.callbacks.error.call(self, err, res,req, self)
					}

					return
				}

				if(self.callbacks['data']){
					self.callbacks.data.call(self, body, res, self)
				}
			});

			req.on("connect", function(){ self.current.connected = true; });
			req.end(data, deliveryCallback);
		}catch(e){
			  console.log("HTTP Request Error: ", e)
				self.current.drops++
		}
	}
}

// Websocket defines a function which returns a new instance of an object
// which allows making request to a underlying octo http server.
class Websocket extends Octo {
	constructor(attr, callback){
		super(attr, callback)
		this.buffer = [];
		this.servers = [];
		this.socket = null;
		this.authenticated = false;
		this.prepareServers();
	}

	prepareServers(){
	  for(var index in this.attr.clusters){
			var pu = url.parse(this.attr.clusters[index])

			switch(pu.protocol){
				case "http:":
					pu.protocol = "ws:"
					break
				case "https:", "tls":
					pu.protocol = "wss:"
					break
			}

		  this.servers.push({
				path: url.parse(url.format(pu, {fragment: true, unicode:true, auth: true})),
				reconns: 0,
				drops: 0,
				connected: false
			})
	  }

		var pu = url.parse(this.attr.addr)

		switch(pu.protocol){
			case "http:":
				pu.protocol = "ws:"
				break
			case "https:", "tls":
				pu.protocol = "wss:"
				break
		}

	  this.servers.push({
			path: url.parse(url.format(pu, {fragment: true, unicode:true, auth: true})),
			reconns: 0,
			drops: 0,
			connected: false,
		})
	}

	on(){
		if(this.socket == null){
			return
		}

		this.socket.on.apply(this.socket, Array.prototype.slice.call(arguments));
	}

	once(){
		if(this.socket == null){
			return
		}

		this.socket.once.apply(this.socket, Array.prototype.slice.call(arguments));
	}

  _handleMessageParsing(message){
		switch(GetType(message)){
			case "Buffer":
				return JSON.parse(message.toString())
			case "String":
				return JSON.parse(message)
			case "Object":
				return message
			default:
			  throw new Error("Unknown type")
		}
	}

  _handleMessage(messages, socket, next){
		var self = this;

		if(GetType(messages) !== "Array"){
			throw new Error("Expected Array response");
		}

		messages.forEach(function(message){
			// console.log("Delivered: ", message);

			if(!message.hasOwnProperty("name") && next){
				return next.call(self,message, socket, self)
			}

		  // console.log("Handling Message Data: ", message);
			switch(message.name){
				case "OK":
				 return

				case AuthRequest:
					var authData = null;

				 switch(GetType(self.attr.auth)){
					case "Object":
						 authData = ParseAuthCredentialsAsCommand(self.attr.auth);
						 break
				  default:
						 throw new Error("Invalid Auth Object Type");
				 }

				 try{
					 socket.write(authData);
				 }catch(e){
					 console.log("Authentication request write failed: ", e)
				 }

				 return

				case AuthDenied:
				  self.authenticated = false;
					return

				case AuthGranted:
				 self.authenticated = true;

				 self.buffer.forEach(function(data){
					 socket.write(data);
				 });

				 self.buffer = []
				 return
			}

		})
	}

	_handleInternals(message, next, socket){
		var self = this;

		// Attempt to handle message internally if there is error
		// pass it to the next handler.
		try{
			self._handleMessage(self._handleMessageParsing(message), socket, next)
		}catch(e){
			console.log("Websocket:MessageHandler error: ", e)
			if(next !== null && next !== undefined){
				console.log("Websocket:MessageHandler: Passing Data to Next: ", message)
				next(message, socket, self)
			}
		}
	}

	Do(data){
		if(this.current == null || this.current == undefined){
			this.getNextServer();
		};

		if(this.current.path.protocol !== "ws:" && this.current.path.protocol !== "wss:"){
			throw new Error("Invalid protocol for socket address");
		};

		var self = this;

		if(this.socket === null){
			try {
				this.socket = websocket(this.current.path.href)

				this.socket.on("connect", function(){
					console.log("Connected to: ", self.current.path.href)

					self.current.connected = true
					if(self.callbacks['connects']){
						self.callbacks.connects.call(self, self.socket, self)
					}
				});


				this.socket.on("error", function(err){
					if(self.callbacks['error']){
						self.callbacks.error.call(self, err, self.socket, self)
					}
				});

				this.socket.on("close", function(){
					console.log("Closing connection to: ", self.current.path.href)

					if(self.callbacks['close']){
						self.callbacks.close.call(self, self.socket, self)
					}

					self.socket = null;
				});

				this.socket.on("data", function(data){
					self._handleInternals(data, self.callbacks['data'], self.socket)
				});

				// this.socket.write(ParseAuthCredentialsAsCommand(self.credentials))
			}catch(e){
				this.current.drops++
			}
		}

		if(this.attr.authenticate && !this.authenticated){
			this.buffer.push(data)
			return
		}

		this.socket.write(data)
	}
}

// GetType returns the internal type of the provided item.
function GetType(item){
	if(item !== undefined && item != null) {
		return (item.constructor.toString().match(/function (.*)\(/)[1])
	}
}

// IsNull returns true/false if the giving value is null/undefined.
function IsNull(item){
 return (item === null || item === undefined)
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
