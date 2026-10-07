#!/usr/bin/env bash
# vpc-agent の DHCP 応答で vm1 が IP を取得できることの確認（pve1 上で agent を起動し vm1 で dhclient を実行）
set -euo pipefail
cd "$(dirname "$0")/.."
make build >/dev/null

work=$(mktemp -d)
out=$work/agent.log
ip netns exec pve1 ./bin/vpc-agent -port-if tap-vm1 >"$out" 2>&1 &
pid=$!
trap 'kill $pid 2>/dev/null || true; rm -rf "$work"' EXIT
sleep 1

ip netns exec vm1 dhclient -1 -v -lf "$work/lease" -pf "$work/pid" eth0 || true

ip=$(ip -n vm1 -4 -o addr show dev eth0 | awk '{print $4}')
ip netns exec vm1 dhclient -r -lf "$work/lease" -pf "$work/pid" eth0 >/dev/null 2>&1 || true

if [[ "$ip" == 10.10.0.*/24 ]]; then
  echo "OK: vm1 got $ip via DHCP"
else
  echo "NG: vm1 did not get an address (eth0: '${ip}')"; cat "$out"; exit 1
fi
