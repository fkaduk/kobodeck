# -*- mode: ruby -*-
# vi: set ft=ruby :


Vagrant.configure("2") do |config|
  config.vm.box = "debian/stretch64"

  # change from default virtualbox, not available in debian
  config.vm.provider :libvirt do |libvirt|
    libvirt.driver = "kvm"
    libvirt.host = ""
    libvirt.connect_via_ssh = false
    libvirt.storage_pool_name = "default"
  end

  # publish source directory under GOPATH
  config.vm.synced_folder './', '/vagrant/go/src/gitlab.com/anarcat/wallabako', type: 'sshfs'

  # preseed the box with wallabako dependencies
  config.vm.provision "shell", inline: <<-SHELL
    export DEBIAN_FRONTEND=noninteractive APT_LISTCHANGES_FRONTEND=mail
    apt update
    apt upgrade -yy
    apt install -y golang golint git gcc-arm-linux-gnueabihf make pv curl
    export GOPATH=/vagrant/go
    mkdir -p $GOPATH/src/gitlab.com/anarcat/wallabako $GOPATH/bin
    chown -R vagrant $GOPATH
    if [ -x /vagrant/go/bin/dep ]; then
        echo "W: curl | sh is horribly, but it seems the only way"
        curl -sSL https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
    fi
  SHELL
end
