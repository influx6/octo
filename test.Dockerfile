FROM golang:latest

COPY . /go/src/github.com/influx6/octo

WORKDIR /go/src/github.com/influx6/octo

# Run go get for vendors
RUN go get -u -v

# Install gometalinter
RUN go get -u -v github.com/alecthomas/gometalinter

# Install missing lint tools
RUN gometalinter --install

# Run go linters
RUN gometalinter --deadline 4m --errors ./consts/...
RUN gometalinter --deadline 4m --errors ./instruments/...
RUN gometalinter --deadline 4m --errors ./messages/...
RUN gometalinter --deadline 4m --errors ./mock/...
RUN gometalinter --deadline 4m --errors ./netutils/...
RUN gometalinter --deadline 4m --errors ./streams/...
RUN gometalinter --deadline 4m --errors ./utils/...

# Run go tests
RUN go test -v ./messages/...
RUN go test -v ./mock/...
RUN go test -v ./netutils/...
RUN go test -v ./streams/...
RUN go test -v ./utils/...
