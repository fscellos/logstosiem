FROM hub.valentine.private.sfr.com/dockerhub/library/alpine:3.14
COPY bin/labgo labgo
ENTRYPOINT ./labgo