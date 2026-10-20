# Build a static binary, run it as a non-root user with nothing else in the image.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pseudonymbench ./cmd/pseudonymbench \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mockupstream ./cmd/mockupstream

FROM gcr.io/distroless/static-debian12:nonroot AS pseudonymbench
COPY --from=build /out/pseudonymbench /pseudonymbench
EXPOSE 8080
ENTRYPOINT ["/pseudonymbench"]

FROM gcr.io/distroless/static-debian12:nonroot AS mockupstream
COPY --from=build /out/mockupstream /mockupstream
EXPOSE 9000
ENTRYPOINT ["/mockupstream"]
