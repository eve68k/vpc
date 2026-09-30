#!/usr/bin/env bash
set -uo pipefail
for ns in vm1 vm2 pve1 pve2; do ip netns del "$ns" 2>/dev/null; done
ip link del br-ul 2>/dev/null
# netns 削除で veth の片端は消えるが、root ns 側の br-pveN が残る場合に備える
for i in 1 2; do ip link del "br-pve${i}" 2>/dev/null; done
echo "lab is down"
