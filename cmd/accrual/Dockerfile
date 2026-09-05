FROM debian:bookworm-slim
COPY accrual_linux_amd64 /usr/local/bin/accrual
RUN chmod +x /usr/local/bin/accrual
EXPOSE 8080
ENTRYPOINT ["accrual"]
