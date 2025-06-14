FROM golang:1.22.3-alpine AS server

RUN apk add --no-cache curl unzip protobuf-dev && \
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
    
COPY go.mod go.sum ./

RUN go mod download

COPY gen ./gen
COPY internal ./internal
COPY cmd ./cmd

RUN CGO_ENABLED=0 GOOS=linux go build -o /build/server ./cmd/server

EXPOSE 8000

CMD ["/build/server"]
