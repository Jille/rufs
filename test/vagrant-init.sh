#!/usr/bin/env bash

set -xe -o pipefail

apt-get update
apt-get install -y fuse jq
sed -i 's/#user_allow_other/user_allow_other/' /etc/fuse.conf

echo "198.51.100.42 e2e.circle" >>/etc/hosts

mkdir -p /public/certs
ln -sf /public/certs /tmp/rufs

case $1 in
discovery)
  ~vagrant/bin/create_ca_pair -circle e2e.circle
  cat <<EOF >/etc/systemd/system/rufs-discovery.service
[Unit]
Description=RUFS Discovery service
After=network.target

[Service]
Type=simple
User=vagrant
Group=vagrant
ExecStart=/home/vagrant/bin/discovery

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now rufs-discovery
;;

client)
  mkdir -p /data
  mkdir -p /fuse
  rm -rf ~vagrant/.rufs2/pki/e2e.circle
  mkdir -p ~vagrant/.rufs2/pki/e2e.circle
  cat <<EOF >~vagrant/.rufs2/config.yaml
circles:
- name: "e2e.circle"
  shares:
  - local: "/data"
    remote: "data"
EOF
  chown -R vagrant:vagrant /data /fuse ~vagrant/.rufs2

  AUTH_TOKEN=$(~vagrant/bin/create_auth_token user1 2>/dev/null)
  sudo -Hiu vagrant ~vagrant/bin/register -circle e2e.circle -token $AUTH_TOKEN -user user1
;;
esac
