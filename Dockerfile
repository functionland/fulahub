FROM golang:1.21 as build
WORKDIR /go/src/fulahub

COPY go.mod go.sum ./
RUN go get -d -v ./...

ADD . /go/src/fulahub
RUN CGO_ENABLED=0 go build -o /go/bin/fulahub ./cmd/fulahub

FROM gcr.io/distroless/static
COPY --from=build /go/bin/fulahub /usr/local/
ENTRYPOINT ["/usr/local/fulahub"]
