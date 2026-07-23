# Web panel image: the platform binary in `serve` mode, plus the docker CLI +
# compose plugin so it can drive deploys through the mounted socket.
# Build context is the repo root (needs cli/ for the embedded web assets).
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY cli/ ./cli/
RUN cd cli && CGO_ENABLED=0 go build -ldflags="-s -w" -o /platform .

FROM alpine:3.20
RUN apk add --no-cache docker-cli docker-cli-compose
COPY --from=build /platform /usr/local/bin/platform
WORKDIR /repo
EXPOSE 8080
ENTRYPOINT ["platform", "serve", ":8080"]
