# Build with the oldest Go the task allows, so the image proves the module
# needs nothing newer.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ghreport ./cmd/ghreport

# distroless/static has CA certificates for HTTPS and no shell; nonroot is
# uid 65532. No token or API address is baked in: both come from `docker run -e`.
FROM gcr.io/distroless/static-debian12:nonroot
# The default --cache-dir is ./.ghreport-cache, so the working directory must
# be writable by the non-root user.
WORKDIR /home/nonroot
COPY --from=build /out/ghreport /usr/local/bin/ghreport
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/ghreport"]
