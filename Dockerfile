# syntax=docker/dockerfile:1
FROM golang:1.27.1-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -o /out/remind . \
    && mkdir -p /out/data \
    && chown 65532:65532 /out/data \
    && chmod 0700 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/remind /remind
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV DATABASE_PATH=/data/remind.db \
    HTTP_ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=30s --retries=3 \
    CMD ["/remind", "healthcheck"]
ENTRYPOINT ["/remind"]
