FROM --platform=$BUILDPLATFORM golang:1.19.3 as builder
ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH
RUN uname -a
RUN echo $BUILDPLATFORM $TARGETPLATFORM $TARGETARCH $TARGETOS
ENV USER=appuser
ENV UID=9000
RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "${UID}" \
    "${USER}"
WORKDIR /app
COPY go.mod go.sum /app
RUN go mod download -x
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags='-w -s -extldflags "-static"' -o migrator ./cmd/migrator

FROM --platform=$TARGETPLATFORM alpine as final
WORKDIR /
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /app/migrator /migrator
RUN mkdir -p /app/migrations
COPY migrations /app/migrations
RUN ls /app/migrations
USER appuser:appuser
WORKDIR /app
ENTRYPOINT ["/migrator"]
