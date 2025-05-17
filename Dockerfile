FROM --platform=$BUILDPLATFORM golang:1.24-alpine as build

WORKDIR /work

# Install git so that go build populates the VCS details in build info, which
# is then reported to Tailscale in the node version string.
RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS TARGETARCH TARGETVARIANT
RUN \
  if [ "${TARGETARCH}" = "arm" ] && [ -n "${TARGETVARIANT}" ]; then \
  export GOARM="${TARGETVARIANT#v}"; \
  fi; \
  GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 go build -v -ldflags='-buildid=' ./cmd/golink

FROM gcr.io/distroless/static-debian12:nonroot

ENV HOME /home/nonroot
# DATABASE_URL and PORT will be injected by Railway.

COPY --from=build /work/golink /golink
COPY --from=build /work/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
CMD ["--verbose"] # Default flags passed to entrypoint.sh 
