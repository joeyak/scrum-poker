FROM golang:1.22 AS build

WORKDIR /build

COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

RUN go install github.com/a-h/templ/cmd/templ@latest
RUN $GOPATH/bin/templ generate
RUN go build -v -o app 

FROM photon

COPY --from=build /build/app /usr/local/bin

ENTRYPOINT ["app"]

CMD ["-no-color"]
