FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-worker ./cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-beat ./cmd/beat \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-manage ./cmd/manage

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -H pace
COPY --from=build /pace-api /usr/local/bin/pace-api
COPY --from=build /pace-worker /usr/local/bin/pace-worker
COPY --from=build /pace-beat /usr/local/bin/pace-beat
COPY --from=build /pace-manage /usr/local/bin/pace-manage
USER pace
EXPOSE 8000
ENTRYPOINT ["pace-api"]
