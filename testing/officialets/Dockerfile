FROM golang:1.26.8-bookworm@sha256:a688600ca24f8a4d3ca77f95b0dd40704a9fc787c826660eb7ba0b641b8b175d AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY testing/officialets/controller ./testing/officialets/controller
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/officialets-controller ./testing/officialets/controller

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/officialets-controller /officialets-controller
ENTRYPOINT ["/officialets-controller"]
