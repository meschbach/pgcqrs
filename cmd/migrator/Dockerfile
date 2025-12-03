FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS data
RUN apk update && apk add ca-certificates && apk add tzdata
ENV USER=appuser
ENV UID=10001
RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "${UID}" \
    "${USER}"
WORKDIR /app

FROM scratch AS final
ARG TARGETARCH
ARG TARGETOS
COPY --from=data /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=data /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=data /etc/passwd /etc/passwd
COPY --from=data /etc/group /etc/group
USER appuser:appuser
COPY --chown=appuser:appuser --chmod=ugo=rx "cmd/migrator/${TARGETARCH}_${TARGETOS}" /migrator

ENTRYPOINT ["/migrator"]
