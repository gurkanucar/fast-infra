FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app .

FROM alpine:3.20
RUN apk add --no-cache wget ca-certificates
COPY --from=build /app /app
EXPOSE 8080
USER nobody
ENTRYPOINT ["/app"]
