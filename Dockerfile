FROM golang:alpine as builder
RUN apk update && apk add --no-cache git gcc libc-dev
WORKDIR $GOPATH/src/github.com/drawpile/listserver
COPY . .
RUN go build -o /go/bin/listserver

FROM golang:alpine
COPY --from=builder /go/bin/listserver /go/bin/listserver
ENTRYPOINT ["/go/bin/listserver", "-l", "0.0.0.0:8080"]

