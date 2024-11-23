#!/bin/bash

# Clear

sudo ip netns delete netns1
sudo ip netns delete netns2
sudo ip link delete veth1-br
sudo ip link delete veth2-br
sudo ifconfig br0 down
sudo brctl delbr br0

# Setup

sudo ip link add name br0 type bridge
sudo ip link set br0 up

# Для netns1
sudo ip link add veth1 type veth peer name veth1-br
sudo ip link set veth1-br master br0
sudo ip link set veth1-br up

# Для netns2
sudo ip link add veth2 type veth peer name veth2-br
sudo ip link set veth2-br master br0
sudo ip link set veth2-br up

# Для netns1
sudo ip netns add netns1
sudo ip link set veth1 netns netns1
sudo ip netns exec netns1 ip link set veth1 up
sudo ip netns exec netns1 ip addr add 192.168.100.2/24 dev veth1
sudo ip netns exec netns1 ip link set lo up

# Для netns2
sudo ip netns add netns2
sudo ip link set veth2 netns netns2
sudo ip netns exec netns2 ip link set veth2 up
sudo ip netns exec netns2 ip addr add 192.168.100.3/24 dev veth2
sudo ip netns exec netns2 ip link set lo up

sudo ip addr add 192.168.100.1/24 dev br0
