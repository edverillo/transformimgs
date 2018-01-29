FROM golang:1.8-alpine

#Installing imagemagick
RUN apk add --no-cache imagemagick git

#Installing godeps
RUN go get github.com/golang/dep/cmd/dep

RUN mkdir -p /go/src/github.com/Pixboost/
WORKDIR /go/src/github.com/Pixboost/
RUN git clone https://github.com/Pixboost/transformimgs.git

WORKDIR /go/src/github.com/Pixboost/transformimgs/
RUN dep ensure

WORKDIR /go/src/github.com/Pixboost/transformimgs/cmd
ENTRYPOINT ["go", "run", "main.go", "-imConvert=/usr/bin/convert", "-imIdentify=/usr/bin/identify"]
