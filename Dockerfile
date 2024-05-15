FROM golang:1.22 AS builder
RUN go version
WORKDIR /wd
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN go build -o /bin/app

FROM debian AS runner
RUN apt-get update
RUN apt-get install -y ca-certificates
COPY --from=builder /bin/app /bin/app
ENTRYPOINT ["/bin/app"]
