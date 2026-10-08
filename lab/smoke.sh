#!/usr/bin/env bash
# vpc-agent が VM のフレームを読めることの確認（pve1 上で agent を起動し vm1 から ARP/ICMP を出す）
set -euo pipefail
cd "$(dirname "$0")/.."
make build >/dev/null

out=$(mktemp)
ip netns exec pve1 ./bin/vpc-agent -port-if tap-vm1 -vni-mac 02:00:00:00:00:01 >"$out" 2>&1 &
pid=$!
trap 'kill $pid 2>/dev/null || true' EXIT
sleep 1

# DHCP 未使用のため vm1 に一時的にIPを振る。ゲートウェイは未実装のため ping 自体は失敗する。ARP request が agent に届けば OK。
ip -n vm1 addr add 10.10.0.1/24 dev eth0
ip netns exec vm1 ping -c 1 -W 1 10.10.0.254 >/dev/null 2>&1 || true
sleep 1

if grep -q "ethertype=0x0806" "$out"; then
  echo "OK: agent captured ARP from vm1"
else
  echo "NG: no frame captured"; cat "$out"; exit 1
fi
