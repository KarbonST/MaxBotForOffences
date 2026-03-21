FROM golang:1.25-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

FROM builder AS bot-build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/max_bot .

FROM builder AS reference-api-build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/reference_api ./cmd/reference_api

FROM alpine:3.21 AS bot
RUN apk add --no-cache ca-certificates tzdata
COPY --from=bot-build /out/max_bot /usr/local/bin/max_bot
ENTRYPOINT ["/usr/local/bin/max_bot"]

FROM alpine:3.21 AS reference-api
RUN apk add --no-cache ca-certificates tzdata
COPY --from=reference-api-build /out/reference_api /usr/local/bin/reference_api
ENTRYPOINT ["/usr/local/bin/reference_api"]
