# -- Build stage
FROM golang:1.26.0-bookworm AS build

WORKDIR /app

COPY go.mod go.sum .

COPY cmd ./cmd
COPY internal ./internal
COPY lib ./lib

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o obgo ./cmd/obgo

# image

## if alpine is needed:
FROM alpine:latest
RUN apk add --no-cache ca-certificates

## scratch (addgroup / adduser wont work)
# FROM scratch AS runtime

RUN addgroup -S obgo && adduser -S obgo -G obgo

WORKDIR /app

COPY --from=build /app/obgo .

USER obgo:obgo

ENTRYPOINT ["./obgo", "pull", "--watch", "-v"]