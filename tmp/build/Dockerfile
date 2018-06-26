FROM alpine:3.6

RUN adduser -D nginx-operator
USER nginx-operator

ADD tmp/_output/bin/nginx-operator /usr/local/bin/nginx-operator
