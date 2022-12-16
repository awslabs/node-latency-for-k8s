FROM public.ecr.aws/aws-ec2/amazon-ec2-metadata-mock:v1.11.2 as aemm
FROM public.ecr.aws/amazonlinux/amazonlinux:2

ENV IMDS_ENDPOINT="http://127.0.0.1:1338"
ENV PROMETHEUS_METRICS="false"
COPY --from=aemm /ec2-metadata-mock /sbin/ec2-metadata-mock
COPY test/entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]