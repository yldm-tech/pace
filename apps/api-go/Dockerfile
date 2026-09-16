# The tag has to keep up with the go line in go.mod: the image pins GOTOOLCHAIN=local, so a module asking for a newer toolchain than the image carries fails the download outright rather than fetching one.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-worker ./cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-beat ./cmd/beat \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-manage ./cmd/manage \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pace-live ./cmd/live

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -H pace
COPY --from=build /pace-api /usr/local/bin/pace-api
COPY --from=build /pace-worker /usr/local/bin/pace-worker
COPY --from=build /pace-beat /usr/local/bin/pace-beat
COPY --from=build /pace-manage /usr/local/bin/pace-manage
COPY --from=build /pace-live /usr/local/bin/pace-live
# The entrypoint scripts, one per service, mirroring apps/api/bin. They are what carry the work each service used to do before its server started — waiting for the database, waiting for migrations, and for the api registering the instance and making the bucket — so that the compose file, the CLI deployment and the all-in-one image all get it without each repeating it.
COPY bin/docker-entrypoint-api.sh /usr/local/bin/pace-entrypoint-api
COPY bin/docker-entrypoint-worker.sh /usr/local/bin/pace-entrypoint-worker
COPY bin/docker-entrypoint-beat.sh /usr/local/bin/pace-entrypoint-beat
COPY bin/docker-entrypoint-migrator.sh /usr/local/bin/pace-entrypoint-migrator
COPY bin/docker-entrypoint-live.sh /usr/local/bin/pace-entrypoint-live
USER pace
# 8000 is the api's; the live service listens on 3000.
EXPOSE 8000 3000
ENTRYPOINT ["pace-entrypoint-api"]
