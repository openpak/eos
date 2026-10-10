# syntax=docker/dockerfile:1
# Epic Online Services for the Switch titles that use it. Plain HTTP inside the pod network:
# Traefik terminates the console's TLS for api.epicgames.dev and forwards the stream, the same
# arrangement as servers/klei and servers/t17.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/eosd ./cmd/eosd

FROM gcr.io/distroless/static
COPY --from=build /out/eosd /eosd
USER nonroot:nonroot
EXPOSE 22620 22621
ENTRYPOINT ["/eosd"]
