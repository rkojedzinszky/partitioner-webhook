FROM golang:1.14-alpine AS build

ADD . /pw

RUN cd /pw && CGO_ENABLED=0 go build . && apk --no-cache add binutils && strip -s partitioner-webhook

FROM scratch

COPY --from=build /pw/partitioner-webhook /

USER 65534

EXPOSE 8443

CMD ["/partitioner-webhook"]
