#!/usr/bin/env bash
# PVE 2台 + VM 2台を network namespace で模擬する。
#
#   [vm1] eth0 ─veth─ tap-vm1 [pve1] pve1-ul ─veth─ br-ul ─veth─ pve2-ul [pve2] tap-vm2 ─veth─ eth0 [vm2]
#
# - 1 netns = 1 PVE ホスト / 1 VM
# - tap-vmN は本番の tap<vmid>i0 の代用（Go アプリがこれを掴む）
# - br-ul は物理スイッチ (AT-x510) の代用
set -euo pipefail

NS_PVE=(pve1 pve2)
UNDERLAY_MTU=1500
VM_MTU=1450   # カプセル化 (約50B) 分を引く

for ns in pve1 pve2 vm1 vm2; do
  ip netns add "$ns"
  ip -n "$ns" link set lo up
done

# underlay: 物理スイッチ相当のブリッジ
ip link add br-ul type bridge
ip link set br-ul mtu "$UNDERLAY_MTU" up

for i in 1 2; do
  # PVE <-> underlay
  ip link add "pve${i}-ul" type veth peer name "br-pve${i}"
  ip link set "pve${i}-ul" netns "pve${i}"
  ip link set "br-pve${i}" master br-ul up
  ip -n "pve${i}" link set "pve${i}-ul" up
  ip -n "pve${i}" addr add "192.168.100.${i}/24" dev "pve${i}-ul"

  # VM <-> PVE（tap の代用）。tap 側にはIPを付けない（Go アプリが L2 で握る）
  ip link add "tap-vm${i}" netns "pve${i}" type veth peer name eth0 netns "vm${i}"
  ip -n "pve${i}" link set "tap-vm${i}" up
  ip -n "vm${i}" link set eth0 address "02:00:00:00:00:0${i}"
  ip -n "vm${i}" link set eth0 mtu "$VM_MTU" up
  # VM のIPは静的に振らず DHCP で取得させる

  # virtio 相当のオフロードを無効化（チェックサム未計算フレーム対策）
  ip netns exec "vm${i}"  ethtool -K eth0 tx off rx off tso off gso off gro off >/dev/null 2>&1 || true
  ip netns exec "pve${i}" ethtool -K "tap-vm${i}" tx off rx off tso off gso off gro off >/dev/null 2>&1 || true
done

echo "lab is up:"
echo "  vm1 (pve1) / vm2 (pve2): IP は DHCP で取得"
echo "  underlay: pve1=192.168.100.1 pve2=192.168.100.2"
echo "例: ip netns exec pve1 ./bin/vpc-agent -port-if tap-vm1 -vni-mac 02:00:00:00:00:01"
