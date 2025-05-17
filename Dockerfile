FROM --platform=$BUILDPLATFORM golang:1.24-alpine as build

WORKDIR /work

# Install git so that go build populates the VCS details in build info, which
# is then reported to Tailscale in the node version string.
RUN apk add git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS TARGETARCH TARGETVARIANT
RUN \
  if [ "${TARGETARCH}" = "arm" ] && [ -n "${TARGETVARIANT}" ]; then \
  export GOARM="${TARGETVARIANT#v}"; \
  fi; \
  GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 go build -v ./cmd/golink

FROM gcr.io/distroless/static-debian12:nonroot


ENV HOME $VOLUME_HOME_DIR
ENV DATABASE_URL $DATABASE_URL
# The DATABASE_URL environment variable should be set when running the container.
# Example: docker run -e DATABASE_URL="postgres://user:pass@host:port/dbname?sslmode=disable" your-golink-image

COPY --from=build /work/golink /golink
ENTRYPOINT ["/golink"]
# Default CMD, expects DATABASE_URL to be set in the environment.
# If DATABASE_URL is not set, --pgdsn will be empty and the application will exit with an error,
# unless a default is set in the application for dev mode (which is currently not the case for production).
CMD ["--verbose"] # --pgdsn will be picked from DATABASE_URL by the app
