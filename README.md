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

-	TCP *Server an Client Packages*
-	HTTP *Server an Client Packages*
-	UDP *Server an Client Packages*
-	Websocket *Server an Client Packages*
-	Quic (Pending)

Client Libraries
----------------

All supported procotols have client libraries which allows connecting to given services and handle any internal operation private to these procotols. Octo also supports connections with the Http and Websocket protocols from NodeJS and Webbrowser through the [OctoJS](https://github.com/influx6/octo/octojs) package which allows connecting on javascript runtimes.
