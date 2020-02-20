FROM golang:alpine as builder
RUN apk update && apk add --no-cache git gcc libc-dev
RUN go get github.com/BurntSushi/toml crawshaw.io/sqlite github.com/gorilla/mux github.com/gorilla/handlers github.com/kelseyhightower/envconfig
WORKDIR $GOPATH/src/github.com/drawpile/listserver
COPY . .
RUN go build -o /go/bin/listserver

FROM golang:alpine
COPY --from=builder /go/bin/listserver /go/bin/listserver
ENTRYPOINT ["/go/bin/listserver", "-l", "0.0.0.0:8080"]

