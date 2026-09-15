FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-api ./cmd/api

FROM alpine:3.22
RUN adduser -D -H pace
COPY --from=build /pace-api /usr/local/bin/pace-api
USER pace
EXPOSE 8000
ENTRYPOINT ["pace-api"]
