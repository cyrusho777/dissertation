FROM alpine:3.16

WORKDIR /

COPY bin/kube-scheduler /bin/kube-scheduler

USER 1000

ENTRYPOINT ["/bin/kube-scheduler"] 