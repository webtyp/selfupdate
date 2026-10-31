FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/selfupdate ./cmd/selfupdate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates docker-cli docker-cli-compose
COPY --from=build /out/selfupdate /usr/local/bin/selfupdate
ENTRYPOINT ["/usr/local/bin/selfupdate"]
