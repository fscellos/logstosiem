FROM hub.valentine.private.sfr.com/dockerhub/library/centos:8
COPY bin/logtosiem logtosiem
ENTRYPOINT ./logtosiem