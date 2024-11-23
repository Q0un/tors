#!/bin/bash

trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

sudo ip netns exec netns1 ./server 0.0.0.0 8080 > /dev/null 2> /dev/null &
sudo ip netns exec netns2 ./server 0.0.0.0 8080 > /dev/null 2> /dev/null &

sleep 2

sudo ip netns exec netns2 iptables -A INPUT -m statistic --mode random --probability 1 -j DROP

./master 0 2 2> /dev/null

sudo ip netns exec netns2 iptables -F
