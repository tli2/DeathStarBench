# syntax=docker/dockerfile:1-experimental

FROM golang 

# Download an initial version of Go
#RUN wget "https://go.dev/dl/go1.20.2.linux-amd64.tar.gz" && \
#  tar -C /usr/local -xzf go1.20.2.linux-amd64.tar.gz

# Set the PATH to include the new Go install.
#ENV PATH="${PATH}:/usr/local/go/bin"

# Install custom version of go with larger minimum stack size.
RUN git clone https://github.com/ArielSzekely/go.git go-custom && \
  cd go-custom && \
  git checkout bigstack && \
  git config pull.rebase false && \
  git pull && \
  cd src && \
  ./make.bash

RUN git config --global http.sslverify false
WORKDIR /go/src/socialnetworkk8
COPY go.* .
COPY . /go/src/socialnetworkk8
RUN go mod vendor

RUN --mount=type=cache,target=/root/.cache/go-build /go/go-custom/bin/go install -ldflags="-s -w" ./cmd/...
