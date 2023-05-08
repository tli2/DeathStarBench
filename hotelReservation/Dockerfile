# syntax=docker/dockerfile:1-experimental

FROM golang:1.17.3

# Install custom version of go with larger minimum stack size.
RUN git clone https://github.com/ArielSzekely/go.git go-custom && \
  cd go-custom && \
  git checkout bigstack && \
  git config pull.rebase false && \
  git pull && \
  cd src && \
  ./all.bash

RUN git config --global http.sslverify false
WORKDIR /go/src/github.com/harlow/go-micro-services
COPY go.* /go/src/github.com/harlow/go-micro-services
RUN /go/go-custom/bin/go mod download
RUN /go/go-custom/bin/go get gopkg.in/mgo.v2
RUN /go/go-custom/bin/go get github.com/bradfitz/gomemcache/memcache
RUN /go/go-custom/bin/go get github.com/google/uuid
RUN mv go.mod x
COPY . /go/src/github.com/harlow/go-micro-services
RUN mv x go.mod 
RUN /go/go-custom/bin/go mod vendor
RUN --mount=type=cache,target=/root/.cache/go-build /go/go-custom/bin/go install -ldflags="-s -w" ./cmd/...
