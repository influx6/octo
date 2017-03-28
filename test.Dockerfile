FROM golang:latest

ADD . /go/src/github.com/influx6/octo

WORKDIR /go/src/github.com/influx6/octo

# Install gometalinter
RUN go get -u -v github.com/alecthomas/gometalinter

# Install missing lint tools
RUN gometalinter --install

# Run go tests
RUN go test -v ./...

# Run go linters
RUN gometalinter --deadline 2m --errors --vendor ./...
