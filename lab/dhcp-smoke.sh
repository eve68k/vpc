#!/usr/bin/env bash
# vpc-agent の DHCP 応答で vm1/vm2 が IP とデフォルトルートを取得できることの確認
# （各 PVE 上で agent を起動し、対応する VM で dhclient を実行）
set -euo pipefail
cd "$(dirname "$0")/.."
make build >/dev/null

work=$(mktemp -d)
pids=()
trap 'kill "${pids[@]}" 2>/dev/null || true; rm -rf "$work"' EXIT

for i in 1 2; do
  ip netns exec "pve${i}" ./bin/vpc-agent -port-if "tap-vm${i}" -vni-mac "02:00:00:00:00:0${i}" >"$work/agent${i}.log" 2>&1 &
  pids+=($!)
done
sleep 1

fail=0
for i in 1 2; do
  ip netns exec "vm${i}" dhclient -1 -v -lf "$work/lease${i}" -pf "$work/pid${i}" eth0 || true

  ip=$(ip -n "vm${i}" -4 -o addr show dev eth0 | awk '{print $4}')
  gw=$(ip -n "vm${i}" route show default | awk '{print $3}')
  ip netns exec "vm${i}" dhclient -r -lf "$work/lease${i}" -pf "$work/pid${i}" eth0 >/dev/null 2>&1 || true

  if [[ "$ip" == 10.10.0.*/24 && "$gw" == 10.10.0.1 ]]; then
    echo "OK: vm${i} got $ip gw $gw via DHCP"
  else
    echo "NG: vm${i} (addr='${ip}' gw='${gw}')"; cat "$work/agent${i}.log"; fail=1
  fi
done
exit $fail
