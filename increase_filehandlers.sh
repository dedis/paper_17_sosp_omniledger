#!/usr/bin/env bash
if uname -a | grep -i darwin; then
  echo "Detected a Mac system - updating file handler limits"
  sudo sysctl -w kern.maxfilesperproc=1024000
  ulimit -n 1024000
  sudo sysctl -w kern.ipc.somaxconn=204800
  sudo sysctl -w kern.maxfiles=1228800
else
  echo "Don't know how to update the file handler limits in this system."
fi