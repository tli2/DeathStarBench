# syntax=docker/dockerfile:1-experimental

FROM golang 

RUN git config --global http.sslverify false
WORKDIR /go/src/github.com/harlow/go-micro-services
COPY go.* .
RUN go mod download
COPY . /go/src/github.com/harlow/go-micro-services
RUN go mod vendor

RUN --mount=type=cache,target=/root/.cache/go-build go install -ldflags="-s -w" ./cmd/...
