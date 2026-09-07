FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod go.work ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/bia-api ./services/api/cmd
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/bia-api /bia-api
ENTRYPOINT ["/bia-api"]
