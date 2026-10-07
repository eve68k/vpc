FROM golang:1.22-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
      iproute2 iputils-ping tcpdump ethtool iperf3 curl netcat-openbsd make isc-dhcp-client \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
CMD ["bash"]
