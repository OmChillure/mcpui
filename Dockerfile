FROM golang:1.23-alpine AS build_base

RUN apk add --no-cache git

# Set the Current Working Directory inside the container
WORKDIR /tmp/app

# We want to populate the module cache based on the go.{mod,sum} files.
COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

# Build the Go app
RUN go build ./cmd/server/

# Start fresh from a smaller image
FROM alpine:latest
COPY --from=build_base /tmp/app/server /server

# Prepare the config directory
ENV HOME /root
RUN mkdir $HOME/.config
RUN mkdir $HOME/.config/mcpwebui

# Run the binary program produced by `go install`
CMD ["./server"]

