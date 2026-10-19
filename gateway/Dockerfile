# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/fling-gateway ./cmd/fling-gateway \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/ulvs/fling"
COPY --from=build /out/fling-gateway /usr/local/bin/fling-gateway
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV FLING_LISTEN=:8080 \
    FLING_DATABASE_PATH=/data/fling-gateway.db
EXPOSE 8080
VOLUME /data
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/fling-gateway"]
